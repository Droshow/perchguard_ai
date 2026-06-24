#!/usr/bin/env python3
"""
PerchGuard Red Team Observer — Phase 5 + Phase 5B Governance Depth

Consolidates five observation surfaces into one terminal session and
persists everything to disk for post-run forensics.

  Surface 1: GET /api/fleet/summary (retrying poll → fleet-history.jsonl)
             Phase 5B: shows delegated_sessions, max_delegation_depth
  Surface 2: tail ./snapshots/audit.jsonl  (per-call forensic record)
             Phase 5B: shows policy_version, data_refs_in, data_ref_out
  Surface 3: watch ./snapshots/governance.json (session close events)
             Phase 5B: shows parent_session_id, child_sessions
  Surface 4: structured stdout combining the above with ANSI colour coding
  Surface 5: GET /api/pipeline (policy version watcher — alerts on change)
             Phase 5B Cap 1: detects hot-reload events during the run

Stored artefacts (written to ./snapshots/):
  fleet-history.jsonl   — one fleet snapshot per poll, timestamped
  obs-<ts>.jsonl        — unified event stream for this observation run

Usage:
  python3 scripts/observe.py [--url http://localhost:8080] [--interval 5]

Env (override args):
  PERCHGUARD_URL      base URL   (default http://localhost:8080)
  PERCHGUARD_API_KEY  Bearer key (required for /api/* endpoints)
"""

import argparse
import json
import os
import queue
import sys
import threading
import time
from datetime import datetime, timezone
from pathlib import Path

try:
    import requests
except ImportError:
    sys.exit("requests is required:  pip install requests")


# ---------------------------------------------------------------------------
# ANSI colour helpers
# ---------------------------------------------------------------------------

RESET  = "\033[0m"
BOLD   = "\033[1m"
DIM    = "\033[2m"
RED    = "\033[91m"
GREEN  = "\033[92m"
YELLOW = "\033[93m"
BLUE   = "\033[94m"
MAGENTA = "\033[95m"
CYAN   = "\033[96m"

DECISION_COLOR = {
    "ALLOW":        GREEN,
    "DENY":         RED,
    "MUTATE":       YELLOW,
    "HUMAN_REVIEW": CYAN,
    "TERMINATE":    MAGENTA,
    "TERMINATE_SESSION": MAGENTA,
}

RISK_COLOR = {
    "low":    GREEN,
    "medium": YELLOW,
    "high":   RED,
}


def _c(color: str, text: str) -> str:
    return f"{color}{text}{RESET}"


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


# ---------------------------------------------------------------------------
# Unified event log (written to obs-<ts>.jsonl)
# ---------------------------------------------------------------------------

class ObsLog:
    """Thread-safe JSONL event log for this observation run."""

    def __init__(self, path: Path):
        self._path = path
        self._lock = threading.Lock()
        path.parent.mkdir(parents=True, exist_ok=True)

    def write(self, surface: str, data: dict):
        record = {"ts": now_iso(), "surface": surface, **data}
        line = json.dumps(record, default=str)
        with self._lock:
            with self._path.open("a") as f:
                f.write(line + "\n")


# ---------------------------------------------------------------------------
# Surface 1: fleet summary poller
# ---------------------------------------------------------------------------

def _risk_color(score: float) -> str:
    if score > 0.7:
        return RED
    if score > 0.4:
        return YELLOW
    return GREEN


def fleet_poller(base_url: str, api_key: str, interval: int,
                 fleet_history_path: Path, obs_log: ObsLog,
                 print_q: queue.Queue, stop: threading.Event):
    """Poll /api/fleet/summary every `interval` seconds with exponential back-off on failure."""
    url = f"{base_url.rstrip('/')}/api/fleet/summary"
    headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
    backoff = 2
    fleet_history_path.parent.mkdir(parents=True, exist_ok=True)

    while not stop.is_set():
        try:
            resp = requests.get(url, headers=headers, timeout=5)
            if resp.status_code == 401:
                print_q.put(("fleet", _c(RED, "[fleet] 401 Unauthorized — set PERCHGUARD_API_KEY")))
                stop.wait(interval)
                continue
            resp.raise_for_status()
            data = resp.json()
            backoff = 2  # reset after success

            # Persist snapshot
            snapshot = {"ts": now_iso(), **data}
            with fleet_history_path.open("a") as f:
                f.write(json.dumps(snapshot) + "\n")
            obs_log.write("fleet", data)

            # Build display line
            active    = data.get("active_sessions", 0)
            high      = data.get("high_risk_sessions", 0)
            dist      = data.get("risk_distribution", {})
            low_n     = dist.get("low", 0)
            med_n     = dist.get("medium", 0)
            high_n    = dist.get("high", 0)
            recent_t  = data.get("recent_terminates", [])
            delegated = data.get("delegated_sessions", 0)
            max_depth = data.get("max_delegation_depth", 0)

            risk_bar = (
                _c(GREEN,   f"low={low_n}") + " " +
                _c(YELLOW,  f"med={med_n}") + " " +
                _c(RED,     f"high={high_n}")
            )
            line = (
                f"{_c(BOLD+CYAN, '[fleet]')}  "
                f"sessions={_c(BOLD, str(active))}  "
                f"high_risk={_c(RED+BOLD if high else GREEN, str(high))}  "
                f"dist=[{risk_bar}]"
            )
            if delegated:
                depth_color = RED if max_depth >= 3 else YELLOW if max_depth >= 2 else DIM
                line += f"  {_c(CYAN, f'delegated={delegated}')}  {_c(depth_color, f'max_depth={max_depth}')}"
            if recent_t:
                line += f"  {_c(MAGENTA, f'terminates={len(recent_t)}')}"

            print_q.put(("fleet", line))

        except requests.ConnectionError:
            msg = _c(DIM, f"[fleet] PerchGuard not reachable at {url} — retrying in {backoff}s")
            print_q.put(("fleet", msg))
            stop.wait(backoff)
            backoff = min(backoff * 2, 60)
            continue
        except Exception as exc:
            print_q.put(("fleet", _c(DIM, f"[fleet] poll error: {exc}")))

        stop.wait(interval)


# ---------------------------------------------------------------------------
# Surface 2: audit.jsonl tail
# ---------------------------------------------------------------------------

def audit_tailer(audit_path: Path, obs_log: ObsLog,
                 print_q: queue.Queue, stop: threading.Event):
    """Follow audit.jsonl and emit colour-coded lines for each new AuditRecord."""
    # Wait for the file to appear (PerchGuard may still be starting)
    announced = False
    while not audit_path.exists():
        if stop.is_set():
            return
        if not announced:
            print_q.put(("audit", _c(DIM, f"[audit] waiting for {audit_path} ...")))
            announced = True
        time.sleep(2)

    with audit_path.open("r") as f:
        # Seek to end so we only see new entries during this run
        f.seek(0, 2)
        print_q.put(("audit", _c(DIM, f"[audit] tailing {audit_path}")))

        while not stop.is_set():
            line = f.readline()
            if not line:
                time.sleep(0.2)
                continue
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except json.JSONDecodeError:
                continue

            obs_log.write("audit", rec)

            decision    = rec.get("decision", "?")
            tool        = rec.get("tool_name", rec.get("tool", "?"))
            session     = rec.get("session_id", "")[-8:]   # last 8 chars
            risk        = rec.get("risk_score", 0.0)
            reason      = rec.get("reason", "")
            policy      = rec.get("policy_hit", "")
            dur         = rec.get("duration_ms", "")
            pol_ver     = rec.get("policy_version", "")      # 5B Cap 1
            refs_in     = rec.get("data_refs_in", [])        # 5B Cap 2
            ref_out     = rec.get("data_ref_out", "")        # 5B Cap 2

            dec_color = DECISION_COLOR.get(decision, RESET)
            risk_str  = _c(_risk_color(risk), f"{risk:.2f}")

            detail = reason
            if policy:
                detail += f"  policy={_c(YELLOW, policy)}"

            msg = (
                f"{_c(BOLD+BLUE, '[audit]')}  "
                f"{_c(dec_color+BOLD, decision):<22}  "
                f"tool={_c(BOLD, tool):<28}  "
                f"risk={risk_str}  "
                f"sess=..{session}"
            )
            if pol_ver:
                msg += f"  {_c(DIM, f'policy={pol_ver}')}"
            if refs_in:
                refs_str = ",".join(refs_in[:3])
                if len(refs_in) > 3:
                    refs_str += f"+{len(refs_in)-3}"
                msg += f"\n          {_c(CYAN, f'lineage_in=[{refs_str}]')}"
            if ref_out:
                msg += f"  {_c(CYAN, f'lineage_out={ref_out}')}"
            if detail:
                msg += f"\n          {_c(DIM, detail)}"
            if dur:
                msg += f"  {_c(DIM, f'{dur}ms')}"

            print_q.put(("audit", msg))


# ---------------------------------------------------------------------------
# Surface 3: governance.json watcher
# ---------------------------------------------------------------------------

def governance_watcher(gov_path: Path, obs_log: ObsLog,
                       print_q: queue.Queue, stop: threading.Event):
    """Watch governance.json for new session records appended on session close."""
    seen_ids: set[str] = set()
    last_size = -1

    while not stop.is_set():
        if not gov_path.exists():
            time.sleep(2)
            continue

        size = gov_path.stat().st_size
        if size == last_size:
            time.sleep(1)
            continue
        last_size = size

        try:
            with gov_path.open() as f:
                records = json.load(f)
        except (json.JSONDecodeError, OSError):
            time.sleep(1)
            continue

        if not isinstance(records, list):
            records = [records]

        for rec in records:
            rid = rec.get("id", "")
            if not rid or rid in seen_ids:
                continue
            seen_ids.add(rid)
            obs_log.write("governance", rec)

            ctx       = rec.get("session_context", {})
            agent_id  = ctx.get("agent_id", "?")
            session   = ctx.get("session_id", "")[-8:]
            calls     = ctx.get("tool_calls", 0)
            peak      = ctx.get("peak_risk_score", 0.0)
            early     = ctx.get("terminated_early", False)
            decisions = ctx.get("decisions", {})
            summary   = rec.get("summary", "")

            parent_id     = ctx.get("parent_session_id", "")  # 5B Cap 3
            child_sessions = ctx.get("child_sessions", [])      # 5B Cap 3

            early_flag = _c(MAGENTA+BOLD, " [TERMINATED EARLY]") if early else ""
            dec_str = "  ".join(
                f"{_c(DECISION_COLOR.get(k, RESET), k)}={v}"
                for k, v in decisions.items()
            )
            msg = (
                f"\n{_c(BOLD+MAGENTA, '─── [governance] session closed ───')}{early_flag}\n"
                f"  agent={_c(BOLD, agent_id)}  sess=..{session}  "
                f"calls={calls}  peak_risk={_c(_risk_color(peak), f'{peak:.2f}')}\n"
                f"  decisions: {dec_str}\n"
            )
            if parent_id:
                msg += f"  {_c(CYAN, f'sub-agent of: ..{parent_id[-8:]}  (delegation depth)')}\n"
            if child_sessions:
                child_short = [f"..{s[-8:]}" for s in child_sessions]
                msg += f"  {_c(CYAN, f'spawned {len(child_sessions)} sub-agent(s): {chr(32).join(child_short)}')}\n"
            msg += (
                f"  {_c(DIM, summary)}\n"
                f"{_c(MAGENTA, '─' * 52)}"
            )
            print_q.put(("governance", msg))


# ---------------------------------------------------------------------------
# Surface 5: policy version watcher (Phase 5B Cap 1)
# ---------------------------------------------------------------------------

def policy_watcher(base_url: str, api_key: str, interval: int,
                   obs_log: ObsLog, print_q: queue.Queue, stop: threading.Event):
    """Poll /api/pipeline and alert whenever the policy hash changes.
    Captures hot-reload events during the observation run."""
    url = f"{base_url.rstrip('/')}/api/pipeline"
    headers = {"Authorization": f"Bearer {api_key}"} if api_key else {}
    last_hash = None

    while not stop.is_set():
        try:
            resp = requests.get(url, headers=headers, timeout=5)
            if resp.status_code in (401, 404):
                stop.wait(interval * 2)
                continue
            resp.raise_for_status()
            data = resp.json()
            current_hash = data.get("policy_hash", "")

            if current_hash and current_hash != last_hash:
                if last_hash is None:
                    msg = (
                        f"{_c(BOLD+YELLOW, '[policy]')}  "
                        f"active hash={_c(BOLD, current_hash)}  "
                        f"{_c(DIM, '(baseline captured)')}"
                    )
                else:
                    msg = (
                        f"{_c(BOLD+RED, '[policy]')}  "
                        f"{_c(RED+BOLD, 'HOT-RELOAD DETECTED')}  "
                        f"{_c(DIM, last_hash)} → {_c(BOLD+YELLOW, current_hash)}"
                    )
                    obs_log.write("policy", {"event": "hot_reload",
                                             "old_hash": last_hash,
                                             "new_hash": current_hash})
                last_hash = current_hash
                print_q.put(("policy", msg))

        except requests.ConnectionError:
            pass
        except Exception as exc:
            print_q.put(("policy", _c(DIM, f"[policy] watcher error: {exc}")))

        stop.wait(interval)


# ---------------------------------------------------------------------------
# Print worker — serialises all output to stdout
# ---------------------------------------------------------------------------

def print_worker(q: queue.Queue, stop: threading.Event):
    while not stop.is_set() or not q.empty():
        try:
            _surface, msg = q.get(timeout=0.3)
            print(msg, flush=True)
        except queue.Empty:
            continue


# ---------------------------------------------------------------------------
# Banner
# ---------------------------------------------------------------------------

def print_banner(base_url: str, interval: int, audit_path: Path,
                 fleet_path: Path, obs_path: Path):
    print(_c(BOLD + CYAN, """
┌─────────────────────────────────────────────────────────────┐
│    PerchGuard Red Team Observer — Phase 5 + Phase 5B        │
│    Governance Depth (Caps 1-5)                              │
└─────────────────────────────────────────────────────────────┘"""))
    print(f"  PerchGuard URL : {_c(BOLD, base_url)}")
    print(f"  Poll interval  : {_c(BOLD, f'{interval}s')}")
    print(f"  Audit source   : {_c(BOLD, str(audit_path))}")
    print(f"  Fleet history  : {_c(BOLD, str(fleet_path))}")
    print(f"  Obs log        : {_c(BOLD, str(obs_path))}")
    print(_c(DIM, "\n  Ctrl-C to stop and print summary\n"))
    print(_c(DIM, "  " + "─" * 56))
    print(f"  {_c(DIM, 'Surfaces:')}  "
          f"{_c(CYAN, '[fleet]')} risk+delegation  "
          f"{_c(BLUE, '[audit]')} per-call+lineage  "
          f"{_c(MAGENTA, '[governance]')} session-close  "
          f"{_c(YELLOW, '[policy]')} hot-reload")

    legend = " | ".join(
        _c(color + BOLD, label)
        for label, color in [
            ("ALLOW", GREEN), ("DENY", RED), ("MUTATE", YELLOW),
            ("HUMAN_REVIEW", CYAN), ("TERMINATE", MAGENTA),
        ]
    )
    print(f"  {legend}\n")


# ---------------------------------------------------------------------------
# Summary on exit
# ---------------------------------------------------------------------------

def print_summary(fleet_history_path: Path, obs_path: Path):
    print(_c(BOLD + CYAN, "\n\n── Observation run complete ──────────────────────────────"))

    counts: dict[str, int] = {}
    peak_risk = 0.0
    fleet_snapshots = 0
    peak_delegated = 0
    peak_depth = 0
    policy_reloads = 0
    lineage_events = 0
    governance_sessions = 0
    delegated_closures = 0

    if fleet_history_path.exists():
        with fleet_history_path.open() as f:
            for line in f:
                try:
                    snap = json.loads(line)
                    fleet_snapshots += 1
                    hr = snap.get("high_risk_sessions", 0)
                    if hr > peak_risk:
                        peak_risk = hr
                    d = snap.get("delegated_sessions", 0)
                    if d > peak_delegated:
                        peak_delegated = d
                    depth = snap.get("max_delegation_depth", 0)
                    if depth > peak_depth:
                        peak_depth = depth
                except json.JSONDecodeError:
                    pass

    if obs_path.exists():
        with obs_path.open() as f:
            for line in f:
                try:
                    ev = json.loads(line)
                    surface = ev.get("surface", "")
                    if surface == "audit":
                        d = ev.get("decision", "?")
                        counts[d] = counts.get(d, 0) + 1
                        if ev.get("data_refs_in") or ev.get("data_ref_out"):
                            lineage_events += 1
                    elif surface == "governance":
                        governance_sessions += 1
                        ctx = ev.get("session_context", {})
                        if ctx.get("parent_session_id"):
                            delegated_closures += 1
                    elif surface == "policy" and ev.get("event") == "hot_reload":
                        policy_reloads += 1
                except json.JSONDecodeError:
                    pass

    print(f"  Fleet polls collected      : {fleet_snapshots}")
    print(f"  Peak high-risk sessions    : {_c(_risk_color(peak_risk / max(fleet_snapshots, 1) * 10), str(int(peak_risk)))}")
    print(f"  Peak delegated sessions    : {_c(CYAN if peak_delegated else DIM, str(peak_delegated))}  (max depth: {peak_depth})")
    print(f"  Policy hot-reloads seen    : {_c(RED+BOLD if policy_reloads else DIM, str(policy_reloads))}")
    print(f"  Lineage-tagged audit events: {_c(CYAN if lineage_events else DIM, str(lineage_events))}")
    print(f"  Sessions closed (gov snap) : {governance_sessions}  ({delegated_closures} were sub-agents)")
    print("\n  Admission decisions observed:")
    for decision, count in sorted(counts.items()):
        color = DECISION_COLOR.get(decision, RESET)
        print(f"    {_c(color + BOLD, f'{decision:<20}')} {count}")

    print(f"\n  Full event log → {_c(BOLD, str(obs_path))}")
    print(_c(CYAN, "─" * 52 + "\n"))


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(
        description="PerchGuard Phase 5 Red Team Observer"
    )
    parser.add_argument("--url", default=None,
                        help="PerchGuard base URL (default: $PERCHGUARD_URL or http://localhost:8080)")
    parser.add_argument("--interval", type=int, default=5,
                        help="Fleet poll interval in seconds (default: 5)")
    parser.add_argument("--snapshots", default="./snapshots",
                        help="Snapshots directory (default: ./snapshots)")
    args = parser.parse_args()

    base_url = args.url or os.getenv("PERCHGUARD_URL", "http://localhost:8080")
    api_key  = os.getenv("PERCHGUARD_API_KEY", "")
    snap_dir = Path(args.snapshots)

    if not api_key:
        print(_c(YELLOW, "Warning: PERCHGUARD_API_KEY not set — fleet endpoint will return 401 unless auth is disabled"))

    ts = datetime.now().strftime("%Y%m%d-%H%M%S")
    fleet_history_path = snap_dir / "fleet-history.jsonl"
    obs_path           = snap_dir / f"obs-{ts}.jsonl"
    audit_path         = snap_dir / "audit.jsonl"
    gov_path           = snap_dir / "governance.json"

    snap_dir.mkdir(parents=True, exist_ok=True)
    print_banner(base_url, args.interval, audit_path, fleet_history_path, obs_path)

    obs_log  = ObsLog(obs_path)
    print_q  = queue.Queue()
    stop_evt = threading.Event()

    threads = [
        threading.Thread(target=fleet_poller,
                         args=(base_url, api_key, args.interval,
                               fleet_history_path, obs_log, print_q, stop_evt),
                         daemon=True, name="fleet"),
        threading.Thread(target=audit_tailer,
                         args=(audit_path, obs_log, print_q, stop_evt),
                         daemon=True, name="audit"),
        threading.Thread(target=governance_watcher,
                         args=(gov_path, obs_log, print_q, stop_evt),
                         daemon=True, name="governance"),
        threading.Thread(target=policy_watcher,
                         args=(base_url, api_key, args.interval,
                               obs_log, print_q, stop_evt),
                         daemon=True, name="policy"),
        threading.Thread(target=print_worker,
                         args=(print_q, stop_evt),
                         name="printer"),
    ]

    for t in threads:
        t.start()

    try:
        while True:
            time.sleep(0.5)
    except KeyboardInterrupt:
        pass
    finally:
        stop_evt.set()
        for t in threads:
            t.join(timeout=3)
        print_summary(fleet_history_path, obs_path)


if __name__ == "__main__":
    main()
