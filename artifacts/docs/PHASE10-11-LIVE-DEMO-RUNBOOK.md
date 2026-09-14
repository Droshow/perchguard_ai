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
- Phase 10a–10c (Part B1–B2): live-validated once before, 2026-09-02, then torn down. This
  run re-validates it, not a first.
- Phase 10e (Part B4, Kata): code-complete and deployed, but the `.metal` node group has
  **never scaled above 0** — kata-deploy's DaemonSet has never actually run on a real node.
  This is the actual first execution.
- Phase 10f (Part B3/B5): both verification scripts exist and have **never been run**. This
  is their first execution too.

---

## Prerequisites

| Tool | Needed for |
|---|---|
| `docker`, `docker compose` | Part A |
| Python 3.11+, `pip install -r sdk/python/requirements.txt` (or the SDK's own) | Part A (if running outside the container) |
| `aws-cli`, `terraform >= 1.6`, `kubectl`, `helm` | Part B |
| `hubble` CLI ([release](https://github.com/cilium/hubble/releases)) | Part B3/B5 |
| `jq` | Part B3/B5 |

**Keys:**
- `ANTHROPIC_API_KEY` — lives in `/home/devsbridge/Work/PerchGuard/perchguard/.env` (the
  sibling repo, not this one). Source it into this shell before Part A:
  ```bash
  export $(grep -E '^ANTHROPIC_API_KEY=' /home/devsbridge/Work/PerchGuard/perchguard/.env | xargs)
  ```
- `PERCHGUARD_API_KEY` — auto-generated on server startup if unset; read it from the
  `perchguard` container's startup logs (Part A) or `bootstrap.sh` output (Part B).

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
`deployments/insurance-agent-python/agent.py:26-89`) has the claims-intake agent call
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
