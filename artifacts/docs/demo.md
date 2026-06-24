# Demo: declared-intent lab + compliance export

`scripts/demo.sh` runs the full governance loop on a single agent session, then
generates an EU AI Act compliance report from the audit trail that loop produced.
No mocking — it builds the real binary, starts a real server, and fires real
HTTP calls against it.

```bash
./scripts/demo.sh
```

It writes into `./snapshots/` and `./demo-output/` for the duration of the run.
`snapshots/governance.json` is tracked in git, so the script backs it up first
and restores it on exit — running the demo never leaves the repo dirty.

## What it does

1. **Builds** PerchGuard (`go build ./cmd`) and starts it in `enforce` mode
   against a fresh, empty audit database.
2. **Registers** an agent, `claims-lab`, with role `read_only_agent` and a
   declared mission scoped to `read_file` and `read_policy`.
3. **Fires three `/intercept` calls** that exercise three different decision
   paths:

   | Call | Tool | Why | Decision |
   |------|------|-----|----------|
   | 1 | `read_file` | in scope, no PII | `ALLOW` |
   | 2 | `write_file` | denied tool for this role | `DENY` |
   | 3 | `read_policy` | carries an SSN-shaped parameter | `HUMAN_REVIEW` |

4. **Submits the human-review call** to `POST /api/review`, then **approves**
   it as an operator via the API-key-guarded `POST /api/review/{id}/approve`.
5. **Closes the session** (`DELETE /api/sessions/{id}`), which emits a
   governance record.
6. **Reads `snapshots/governance.json`** and shows the record for this
   session — three steps, three different decisions, one risk-driven
   governance event.
7. **Stops the server** and runs the same binary in `--mode=compliance` twice,
   against the audit database the run just produced: once for the Art. 26
   deployer-obligations coverage report, once for the Annex IV technical
   documentation report. No server required for this step — it reads the
   SQLite audit store and policy file directly.

## Sample output

A real run (anonymized session IDs, timestamps from `2026-06-19`):

```
=== Call 2/3 — write_file (denied tool for read_only_agent) -> expect DENY ===
{
  "uid": "call-2",
  "decision": "DENY",
  "reason": "Tool 'write_file' is explicitly denied for role 'read_only_agent'"
}

=== Call 3/3 — read_policy carrying an SSN-shaped parameter -> expect HUMAN_REVIEW ===
{
  "uid": "call-3",
  "decision": "HUMAN_REVIEW",
  "reason": "PII-shaped content in parameter \"customer_ssn\""
}
```

The governance record for the session shows all three decisions and the
declared intent baseline drift score recorded for each step:

```json
"decisions": { "ALLOW": 1, "DENY": 1, "HUMAN_REVIEW": 1 },
"peak_risk_score": 0.20814413464563078,
"intent_baseline": "Look up insurance claims and policy details for customers read_file read_policy",
"drift_threshold": 0.6
```

The Art. 26 coverage report turns that same audit trail into a per-obligation
table — for example, obligation (4) ("Monitor operation; suspend use & notify
on risk") reads as:

```
| (4) Monitor operation; suspend use & notify on risk (Art. 79(1)) | `evidenced` | 1 HUMAN_REVIEW decisions, 0 TERMINATE decisions in window. | audit store decisions table |
```

And the Annex IV report's §2(e) (human-oversight assessment) reads as:

```
## §2(e) Assessment of Art. 14 human-oversight measures

**Status:** `generated`

humanReview.enabled=true, timeoutSeconds=60. 1 HUMAN_REVIEW decisions in window.
Each decision's Reason/PolicyHit fields are the technical measures facilitating
deployer interpretation of outputs.
```

See [compliance-export.md](compliance-export.md) for what every section of
both reports does and doesn't cover.

## Two gotchas the script works around

- **Stale policy file.** `findPolicyPath()` checks `~/.config/perchguard/`
  before the repo's `./configs/`. If you've previously run `--mode=init` on
  this machine, that directory may hold an old `policies.yaml` (e.g. one set
  to `enforcement: observe`) that would silently override what you expect
  this demo to do. The script sets `PERCHGUARD_POLICY` explicitly to avoid
  this.
- **`go run` doesn't forward signals to its compiled child.** Killing a
  `go run ./cmd &` process leaves the actual server running and bound to the
  port. The script builds a real binary with `go build -o ... ./cmd` and runs
  that directly so its PID is the one the script can actually stop.

---

← [Docs index](README.md)
