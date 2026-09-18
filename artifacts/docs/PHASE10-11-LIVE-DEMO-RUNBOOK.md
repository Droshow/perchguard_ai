# Phase 10 + 11 — Live Demo Runbook

**How to use this doc:** this is not a script for an agent to run unattended. It's a
sequence of scenarios for a human to drive, one command block at a time, narrating what's
on screen — the source material for a recorded walkthrough. Each scenario states what it
proves, the exact commands, what to point the camera at, and the expected result. Stop and
look at the output before moving to the next block.

**Do Part A first.** It's free, local, and takes ~15 minutes. Part B needs a live EKS
cluster and, for the Kata scenario, a `.metal` instance at real money — don't start it
without deliberately deciding to spend.

**What's actually being proven, honestly, for the narration:**
- Phase 11 (Part A): real code path, unit-tested, but this will be its **first live run
  against the real Claude API** — nobody has watched this delegation happen end-to-end yet.
- Phase 11 LangGraph swarm (Part A3b): a real 5-role concurrent fan-out/fan-in graph, run
  live for the first time on 2026-09-18. See "A3b gotchas" below before narrating this one —
  it does not complete as a clean approval on every run, and that's worth explaining rather
  than hiding.
- Phase 10a–10c (Part B1–B2): live-validated once before, 2026-09-02, then torn down. This
  run re-validates it, not a first.
- Phase 10e (Part B4, Kata): code-complete and deployed, but the `.metal` node group has
  **never scaled above 0** — kata-deploy's DaemonSet has never actually run on a real node.
  This is the actual first execution.
- Phase 10f (Part B3/B5): both verification scripts exist and have **never been run**. This
  is their first execution too.

**Status as of 2026-09-18 (last live run):** Part A confirmed working end-to-end including
observability. Part B bootstrapped clean on the first `bootstrap.sh` pass (no
`LESSONS-LEARNED.md` deviations this time) — EKS live, ALB healthy. B1–B6 have not been
re-driven yet this session; do that next.

---

## Prerequisites

| Tool | Needed for |
|---|---|
| `docker`, `docker compose` | Part A |
| Python 3.11+, `pip install -r sdk/python/requirements.txt` (or the SDK's own) | Part A (if running outside the container) |
| `aws-cli`, `terraform >= 1.6`, `kubectl`, `helm` | Part B |
| `hubble` CLI ([release](https://github.com/cilium/hubble/releases)) | Part B3/B5 |
| `jq` | Part B3/B5 |
| `grip` (`pip install grip`) | optional — renders this doc as GitHub-style HTML at `localhost:6419` via `grip artifacts/docs/PHASE10-11-LIVE-DEMO-RUNBOOK.md 6419` |

**Keys:**
- `ANTHROPIC_API_KEY` — lives in `/home/devsbridge/Work/PerchGuard/perchguard/.env` (the
  sibling repo, not this one). Source it into this shell before Part A:
  ```bash
  export $(grep -E '^ANTHROPIC_API_KEY=' /home/devsbridge/Work/PerchGuard/perchguard/.env | xargs)
  ```
- `PERCHGUARD_API_KEY` — auto-generated on server startup if unset; read it from the
  `perchguard` container's startup logs (Part A) or `bootstrap.sh` output (Part B).
- **docker-compose.yaml expects a `.env` at this repo's root** (it doesn't ship one). The
  sibling repo's `.env` already has the right var names
  (`ANTHROPIC_API_KEY_USED_BY_PERCHGUARD`, `ANTHROPIC_API_KEY_USED_BY_RED_TEAM`,
  `PERCHGUARD_API_KEY`) — don't duplicate the secret, symlink it once:
  ```bash
  ln -s /home/devsbridge/Work/PerchGuard/perchguard/.env /home/devsbridge/Work/PerchGuard/perchguard_ai/.env
  ```
  (`.env` and `.env.*` are gitignored here, so the symlink is safe to leave in place.)
- **AWS SSO profile for Part B**: `AWS_PROFILE=devsbridge` (account `961477247679`,
  `eu-central-1`) — the `default` profile in `~/.aws/config` is a different account and will
  fail `bootstrap.sh` with `InvalidClientTokenId`. Verify with
  `AWS_PROFILE=devsbridge aws sts get-caller-identity` before running `bootstrap.sh`.

**`deployments/` layout (reorganized 2026-09-18 — every path below changed once; if a
command in this doc 404s on a Dockerfile or build context, this is why):**
```
deployments/
├── golang-agents/            # Go source — main.go (+ Dockerfile where containerized)
│   ├── insurance-agent/          # go-run scenario script, no Dockerfile
│   ├── insurance-mcp-server/     # real MCP tool server, JSON-RPC :8090
│   └── healthcare-mcp-server/    # real MCP tool server, JSON-RPC :8091
├── python-agents/            # Python source — agent.py/tools.py/tasks.py + Dockerfile
│   ├── insurance-agent-python/       # A2/A3 hand-wired 2-hop demo
│   ├── insurance-agent-langgraph/    # A3b real LangGraph 5-role swarm
│   ├── healthcare-agent-python/      # Phase 3 healthcare lab
│   └── redteam-mcp-agent/            # Phase 5 red-team harness (also used by EKS Part B)
├── docker-compose/           # unchanged — compose files + mock-agent/ (Go, stays local to compose)
├── eks/                      # unchanged — Terraform + eks/redteam-mcp-agent/ (k8s manifests
│                              # only, not source; still points at python-agents/redteam-mcp-agent
│                              # for the image build)
├── grafana/, helm/, k3s/, prometheus/   # unchanged — infra config, not agent source
```
Docker Compose service *names* (`insurance-agent-python`, `insurance-mcp-server`, etc.) are
unchanged throughout this doc — every `docker compose exec <service> ...` command below still
works verbatim. Only filesystem paths moved (build contexts, Dockerfile `COPY` lines,
`bootstrap.sh`'s local `docker build` for the redteam image) — already updated everywhere in
this repo as of this reorg; this note exists so a stale local checkout or a half-read old
version of this doc doesn't send you chasing a path that no longer exists.

---

## Part A — Phase 11: Multi-agent delegation (local, free)

### A1. Bring up the stack

```bash
docker compose -f deployments/docker-compose/docker-compose.yaml up --build -d perchguard insurance-mcp-server insurance-agent-python jaeger
docker compose -f deployments/docker-compose/docker-compose.yaml logs perchguard | grep -i "api key\|listening"
```
Grab the auto-generated `PERCHGUARD_API_KEY` (`pgmk-...`) from that log line — you'll want
it for A5's live watcher.

**On screen:** the four containers healthy (`docker compose ps`).

### A1b. Optional — bring up Grafana/Prometheus/Loki

A separate compose file (`docker-compose.observability.yml`), additive to A1's stack — it
scrapes the already-running `perchguard` container by service name, so bring it up with
`prometheus`/`loki`/`grafana` named explicitly rather than `up -d` with no service list, or
compose will also try to recreate `perchguard`/`insurance-agent-python` from the merged
definition:
```bash
docker compose -f deployments/docker-compose/docker-compose.yaml \
               -f deployments/docker-compose/docker-compose.observability.yml \
  up -d prometheus loki grafana
```
**On screen:** Grafana at `localhost:3000` (`admin` / `perchguard`), Prometheus at
`localhost:9090`, dashboards provisioned from `deployments/grafana/dashboards/`. Note this
recreates the `perchguard` container (config merge from the second compose file) — harmless
before any session is registered, but do this **before** A2/A3, not mid-scenario, or you'll
lose in-flight session state.

### A2. Baseline sanity check — one governed call, no delegation

```bash
docker compose -f deployments/docker-compose/docker-compose.yaml exec \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  insurance-agent-python python agent.py summarise_policy
```
**On screen:** one `SESSION:` line, a normal Claude response, a real `read_policy` tool
call round-tripping through PerchGuard → `insurance-mcp-server`. Confirms the baseline
governed loop works before we add delegation to the picture.

### A3. The headline scenario — fraud-escalation delegation

This is the real "agent-as-tool" hand-off: `agent.py`'s `run_fraud_escalation` (see
`deployments/python-agents/insurance-agent-python/agent.py:26-89`) has the claims-intake agent call
`escalate_to_investigator` as a genuine tool. The SDK routes that call to a local Python
handler instead of the MCP server (`sdk/python/perchguard/loop.py:144-145`), which
registers a **child session** with `parent_session_id` set and runs the investigator's own
fully-governed loop.

```bash
docker compose -f deployments/docker-compose/docker-compose.yaml exec \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  insurance-agent-python python agent.py fraud_escalation
```
**Watch for, on screen:**
- Two distinct `SESSION:` lines — the second printed as `(sub-agent of <parent-id>)`.
- The investigator's own `search_claims` / `run_sql` / `write_report` tool calls, each
  ALLOWed under the **child's own token**, not the parent's.
- A final `CLAUDE:` response that's the investigator's findings, handed back through the
  parent's `pg.validate_output()`.

This is the moment worth cutting into the video — real delegation, not a scripted second
task.

### A3b. LangGraph swarm — concurrent fan-out/fan-in (first live run, 2026-09-18)

A separate, richer scenario from A3: a real LangGraph `StateGraph`
(`deployments/python-agents/insurance-agent-langgraph/graph.py`) with 5 governed roles in a diamond+tail
shape, not a straight chain —
`intake -> {investigator, compliance} -> approver -> notifier`. `investigator` and
`compliance` run **concurrently**, both real PerchGuard sessions with `intake`'s session as
their verified parent (`X-PerchGuard-Agent-Token`, constant-time compare, not a trusted
claim — see `pkg/api/agents.go`). This is the scenario that populates the dashboard's
**Swarm Graph** panel with more than a 2-node edge — multiple children fanning from one
parent, live.

```bash
docker compose -f deployments/docker-compose/docker-compose.yaml up --build -d insurance-agent-langgraph
docker compose -f deployments/docker-compose/docker-compose.yaml exec \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -e PERCHGUARD_URL="http://perchguard:8080" \
  -e MCP_URL="http://insurance-mcp-server:8090" \
  -e PERCHGUARD_API_KEY="$PERCHGUARD_API_KEY" \
  insurance-agent-langgraph python agent.py
```
**On screen:** `localhost:8080`'s Swarm Graph panel during the run — the delegation edges
from `intake` fanning out to two concurrent children, joining at `approver`. Each node's
tool calls print with their own ALLOW/HUMAN_REVIEW/TERMINATE decisions, same as A3.

**Gotchas from the first live run — read before narrating this live:**
- **Swarm graph's "data lineage" dashed edges will not appear.** The graph edges you *will*
  see are `delegation` only. Cross-agent data-lineage edges
  (`pkg/api/swarm.go`'s `"data"` edge type) require a tool call to set `DataRefOut` and a
  child to declare `InheritedDataRefs` at registration — neither `agent.py` nor
  `sdk/python/perchguard/loop.py` wires this yet. Don't promise the dashed line on camera.
- **`read_policy` needs a real `policy_id`, and none of the mock claim data supplies one for
  CLM-9985.** Fixed 2026-09-18 by adding a `CLM-9985` case to `handleReadPolicy` in
  `deployments/golang-agents/insurance-mcp-server/main.go` (mirroring the existing `CLM-9983` pattern) and
  tightening `INTAKE_TASK` in `tasks.py` to force the exact tool call
  (`"do not substitute or search first"` — the same phrasing that already works reliably in
  the hand-wired demo's `poisoned_claim` task). Without this, Claude sometimes skips
  `read_policy` and asks *you* for a policy ID instead — and that ambiguous text, once
  interpolated into the investigator/compliance prompts via `{intake_summary}`, reliably
  trips the `SemanticFirewallValidator` as injection-shaped, cascading into every downstream
  node refusing. (Genuinely interesting to narrate if it happens again — the agents
  correctly distrusted each other — but it's not the "headline" clean-approval story.)
- **Session risk is monotonic and never decays** (`pkg/agent/risk.go:18-20`, by design —
  "a session that drifted once remains suspect even if it temporarily behaves"). The
  investigator/compliance task templates give Claude open-ended license to explore
  (`"Use search_claims and run_sql to investigate for fraud indicators"`), and broad
  exploratory SQL (`SELECT * FROM claims WHERE claim_amount > 2000000`, guessing at
  policy IDs the intake node never actually handed them) accumulates risk past the 0.9
  TERMINATE threshold within 2-3 calls — permanently locking that session out of
  `write_report` too, even though the underlying findings were legitimate. This is not a
  bug. If you want a clean approve-path run for camera, this is the thing to tighten next
  (narrower task templates, or hand the real policy_id/claim data down through graph state
  instead of letting each node re-discover it) — not yet done as of this write-up.
- **Re-validated 2026-09-18 (second session)**: after two more fixes below, a clean
  end-to-end run completed all 5 nodes with zero TERMINATE decisions. `send_notification`
  still fails every run — `NOTIFY_TASK_TEMPLATE` hardcodes `destination='claims-team@internal'`,
  which the mock MCP server's allowlist doesn't recognize, so Claude asks you for the
  correct channel instead of sending. This is expected with the current task template, not
  a governance bug — narrate it as "the agent correctly refuses to guess a channel," or fix
  the template's destination string before recording if a clean finish is wanted on camera.
- **Crash: empty `tool_results` sent to Anthropic as a 400 (fixed 2026-09-18).** After
  several consecutive `TERMINATE` decisions, a node's turn sometimes has no `tool_use`
  blocks left in `response.content` (text-only response, `stop_reason != "end_turn"`).
  `_run_loop` (`sdk/python/perchguard/loop.py:184`) then appended
  `{"role": "user", "content": []}` — Anthropic's API rejects empty user content with
  `400 messages.N: user messages must have non-empty content`, killing the whole graph
  mid-run (hit live in the `investigator` node). Fixed by returning the turn's text instead
  of sending an empty follow-up when `tool_results` is empty. Rebuild both agent images
  after pulling this fix — the SDK is installed from `sdk/python` at image build time, not
  mounted live.
- **Dashboard Swarm Graph labels were truncated mid-word (fixed 2026-09-18).**
  `shortLabel()` in `pkg/api/dashboard.go` sliced the last 18 raw characters of `agent_id`
  (`base.slice(-18)`), which cut `insurance-agent-compliance` to `e-agent-compliance` —
  every node's shared `insurance-agent-` prefix garbled differently instead of dropping
  cleanly. Fixed to drop whole `-`-separated segments from the front until the label fits
  (`agent-compliance`, `investigator`, etc.). Rebuild the `perchguard` image to pick this up.
- **Token/cost dashboard fixed 2026-09-18** (was always $0.00 regardless of real Anthropic
  spend — see Phase 9 in the top-level `CLAUDE.md` for the full mechanism). `loop.py` now
  forwards real `response.usage` per turn; confirmed live via
  `curl localhost:8080/api/sessions/<id>/budget` and `perchguard_tokens_used_total` in
  `/metrics`. Narrate the dashboard's Cost/Tokens cards with confidence now — they're real.
- **Sessions weren't evicting** (`"session left un-evicted: no api_key configured"` warning)
  because `graph.py:65`'s `PerchGuardClient(PERCHGUARD_URL)` never got an api_key. Pass
  `-e PERCHGUARD_API_KEY=...` on the `exec` (same as A5's pattern) — without it, every run's
  sessions pile up as stale "active" rows on the dashboard.

### A4. Negative test — scope boundary actually holds

Prove `fraud_investigator` cannot itself escalate (no recursive delegation —
`configs/policies.yaml`'s `fraud_investigator` role denies `escalate_to_investigator`).
Easiest way to show this live: register a session as `fraud_investigator` directly and
attempt the tool call.

```bash
curl -s -X POST http://localhost:8080/agents/register \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"demo-investigator","agent_role":"fraud_investigator","intent":"probe scope boundary"}'
```
Take the returned `session_id`/`token`, then POST an `escalate_to_investigator` call to
`/intercept` under that token — expect a `DENY` with a reason citing the role's denied-tool
list.

**On screen:** a clean DENY, proving the ceiling `ValidateScope` enforces
(`pkg/agent/delegation.go:73-101`) is real, not just documentation.

### A5. Watch it live + inspect the audit trail

Run this in a second terminal *before* re-running A3, so the delegation shows up live:
```bash
PERCHGUARD_API_KEY=pgmk-... python3 scripts/observe.py
```
**On screen:** the `fleet` view showing `delegated_sessions`/`max_delegation_depth` tick
up, then the governance close event rendering "sub-agent of ..." for the investigator
session.

Then, after the run:
```bash
cat snapshots/governance.json | jq '.[] | select(.agent_id | contains("investigator")) | {id, parent_session_id}'
cat snapshots/governance.json | jq '.[] | select(.child_sessions != null) | {id, child_sessions}'
```
**On screen:** the parent record's `child_sessions` list containing the investigator's
session id, and the investigator record's `parent_session_id` pointing back — the
delegation edge, persisted.

### A6. Cleanup

```bash
docker compose -f deployments/docker-compose/docker-compose.yaml down
```

---

## Part B — Phase 10: K8s isolation operator, Cilium, Kata (live EKS — costs money)

> **STOP before running B0.** This provisions a real EKS cluster (~$0.10/hr control plane
> + Fargate + NAT gateway + node groups). Confirm you want to spend before continuing.
> `deployments/eks/LESSONS-LEARNED.md` has the full gotcha history if anything here
> deviates from what actually happens.

### B0. Bootstrap

```bash
export AWS_ACCOUNT_ID=<your account id>       # or let bootstrap.sh detect it via STS
export AWS_REGION=eu-central-1                 # optional, this is the default
cd deployments/eks && ./bootstrap.sh
```
Loads `PERCHGUARD_LLM_API_KEY`/`PERCHGUARD_API_KEY` from repo-root `.env` if present, then
runs the three-phase apply (ECR → images → AWS infra Phase A → kubeconfig → CoreDNS/Gateway
CRDs Phase B → full apply Phase C). Expect ~12–20 minutes; narrate the phases as they print
— they exist specifically because a naive single-pass `terraform apply` fails (see
`LESSONS-LEARNED.md` #2).

**On screen at the end:** the printed ALB address, `Intercept:`/`Management:`/`Health:`
URLs. Confirm:
```bash
export KUBECONFIG=deployments/eks/.kubeconfig
kubectl get nodes
curl -sf http://<alb>/healthz
```

**2026-09-18 run:** clean first pass, no `LESSONS-LEARNED.md` deviations. ALB came up as
`k8s-perchgua-perchgua-<hash>.eu-central-1.elb.amazonaws.com`, `/healthz` confirmed. Don't
hardcode this hostname anywhere — the ALB is re-provisioned (new hash) on every fresh
`bootstrap.sh` run; always re-read it from `bootstrap.sh`'s own output or
`kubectl get gateway perchguard -n perchguard`.

### B1. Phase 10a/10b — agent pods + operator reconcile

```bash
kubectl get pods -n insurance-agent -n healthcare-agent -n redteam-mcp-agent
kubectl get agentisolationpolicy default -o yaml
```
**On screen:** all agent pods `Running` with the hardened pod-security context (`runAsNonRoot`,
`capabilities.drop: ["ALL"]`), and the CRD's `status.conditions` showing `Reconciled: True`
with `spec.egress.allowedDestinations` matching `configs/policies.yaml`'s
`dataExfiltration` block verbatim — the "no drift" claim, shown live rather than just
tested.

To make the hot-reload point land on camera: edit `configs/policies.yaml` (add/remove a
destination), `kubectl apply` the updated ConfigMap, and watch `lastAppliedHash` change
within the 30s window:
```bash
kubectl get agentisolationpolicy default -o jsonpath='{.status.lastAppliedHash}' -w
```

### B2. Phase 10c — Cilium enforcement is real, not just declared

```bash
kubectl get pods -n cilium-system
kubectl exec -n redteam-mcp-agent deploy/redteam-mcp-agent -- curl -s -o /dev/null -w '%{http_code}\n' https://<an-allowed-destination>
kubectl exec -n redteam-mcp-agent deploy/redteam-mcp-agent -- curl -s -o /dev/null -w '%{http_code}\n' https://some-not-allowlisted-host.example --max-time 5
```
**On screen:** the allowed call succeeds, the disallowed call **times out at the network
layer** — this is CNI-level enforcement, independent of whether the app-layer call ever
happens. This is the concrete answer to the Cagebreak-GPT incident doc's verdict.

### B3. Phase 10d/10f — cross-layer conformance (first real execution)

```bash
kubectl port-forward -n cilium-system svc/hubble-relay 4245:80 &
export PERCHGUARD_URL=http://<alb>
export PERCHGUARD_API_KEY=pgmk-...
./scripts/cross-layer-conformance.sh 1h
```
**On screen:** the app-layer DENY list from `/api/audit`, the Hubble DROPPED list, and the
diff. `OK` lines mean both layers agree; any `DRIFT` line is real signal — an app-layer
denial the network layer doesn't actually block yet — worth calling out on camera exactly
as what it is: a live gap, not a hidden one. (Per the phase table, the full
burn-in-then-flip rollout procedure for 10d isn't built — this script is the diagnostic
piece that exists today, not the automated rollout.)

### B4. Phase 10e — Kata sandbox (first time this has ever run)

> **STOP before this step.** `c5.metal` is ~$4/hr. Scale it up, run the check, scale back
> to 0 immediately after — don't leave it running.

```bash
cd deployments/eks
terraform apply -var="aws_account_id=$AWS_ACCOUNT_ID" -var="kata_node_desired_size=1"
kubectl get nodes -l perchguard/kata-capable=true
kubectl get pods -n kube-system -l app=kata-deploy   # DaemonSet actually running on the node
cd ../..
./scripts/kata-containment-check.sh
```
**On screen:** the `runc` and `kata-qemu` containment-test pods' logs, and the script's
kernel-version comparison. A **PASS** (different `KERNEL:` lines) is the actual proof that
Kata is giving the redteam-mcp-agent workload real guest-kernel isolation, not just a
`RuntimeClass` label with no teeth. This is worth narrating as the moment kata's promise
gets checked for the first time.

Then immediately:
```bash
cd deployments/eks
terraform apply -var="aws_account_id=$AWS_ACCOUNT_ID" -var="kata_node_desired_size=0"
```

### B5. Phase 10f, closing the loop

Re-run B3's `cross-layer-conformance.sh` now that Kata's DaemonSet has actually run once —
narrate that this script and the containment check together are what "cross-layer
conformance" means in this project: network-layer and sandbox-layer claims checked against
what a real cluster does, not just what the manifests say.

### B6. Teardown — do not skip

```bash
cd deployments/eks
terraform destroy -var="aws_account_id=$AWS_ACCOUNT_ID"
```
**Confirm afterward:**
```bash
aws eks list-clusters --region "$AWS_REGION"
aws ecr describe-repositories --region "$AWS_REGION" | grep perchguard
```
Both should come back empty. If `terraform destroy` fails partway (see
`LESSONS-LEARNED.md` for known ordering issues), don't leave it — resolve or delete the
stuck resources manually before ending the session.
