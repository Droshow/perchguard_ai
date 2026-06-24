#!/usr/bin/env bash
# PerchGuard public demo — declared-intent lab + EU AI Act compliance export.
#
# Walks through the full governance loop on a single agent session:
#   register -> ALLOW call -> DENY call -> PII call -> HUMAN_REVIEW
#   -> operator approves -> session closes -> governance.json
#   -> compliance report generated from the same audit trail
#
# Usage (from the perchguard/ repo root):
#   ./scripts/demo.sh
#
# Writes into ./snapshots/ and ./demo-output/ for the duration of the run.
# snapshots/governance.json is tracked in git, so this script backs it up
# before running and restores it on exit — the demo never leaves the repo
# dirty.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

BASE_URL="http://localhost:8080"
API_KEY="demo-key-$(date +%s)"
DB_PATH="./snapshots/demo-audit.db"
OUT_DIR="./demo-output"
BACKUP_DIR="$(mktemp -d)"
BIN=""

log()     { printf '\n=== %s ===\n' "$1"; }
narrate() { printf '\n  \033[1;36m%s\033[0m\n' "$*"; }
pause()   { printf '\n'; read -rp $'  \033[1;33m[Press Enter to continue]\033[0m  '; printf '\n'; }

cleanup() {
  if [[ -n "${PERCHGUARD_PID:-}" ]] && kill -0 "$PERCHGUARD_PID" 2>/dev/null; then
    kill "$PERCHGUARD_PID" 2>/dev/null || true
    wait "$PERCHGUARD_PID" 2>/dev/null || true
  fi
  [[ -n "$BIN" ]] && rm -f "$BIN"
  rm -f "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm"
  # Restore tracked/pre-existing snapshot files so the demo leaves no trace.
  if [[ -f "$BACKUP_DIR/governance.json" ]]; then
    mv "$BACKUP_DIR/governance.json" ./snapshots/governance.json
  else
    rm -f ./snapshots/governance.json
  fi
  if [[ -f "$BACKUP_DIR/audit.jsonl" ]]; then
    mv "$BACKUP_DIR/audit.jsonl" ./snapshots/audit.jsonl
  else
    rm -f ./snapshots/audit.jsonl
  fi
  rm -rf "$BACKUP_DIR"
}
trap cleanup EXIT

[[ -f ./snapshots/governance.json ]] && cp ./snapshots/governance.json "$BACKUP_DIR/governance.json"
[[ -f ./snapshots/audit.jsonl ]] && cp ./snapshots/audit.jsonl "$BACKUP_DIR/audit.jsonl"
mkdir -p "$OUT_DIR"

narrate "PerchGuard — agentic admission controller"
narrate "Every tool call an AI agent makes is intercepted and evaluated here before it executes."
narrate "This demo: register agent  →  ALLOW  →  DENY  →  PII flagged (HUMAN_REVIEW)  →  operator approves  →  governance snapshot  →  EU AI Act export."
pause

log "Building PerchGuard"
# Build a real binary rather than `go run`: go run's child process doesn't
# receive signals sent to the `go run` wrapper, so cleanup can't stop it.
BIN="$OUT_DIR/perchguard-demo"
go build -o "$BIN" ./cmd

narrate "Binary built. Starting the server in enforce mode with a clean audit database."
narrate "Policy file: configs/policies.yaml — every tool call is evaluated against these rules."
pause

log "Starting PerchGuard (enforce mode, fresh audit db)"
# Pin the policy file explicitly: findPolicyPath() prefers ~/.config/perchguard/
# over the repo's ./configs/ when both exist, which would silently run this demo
# in whatever mode happens to be installed on the machine.
PERCHGUARD_API_KEY="$API_KEY" PERCHGUARD_DB_PATH="$DB_PATH" PERCHGUARD_POLICY="./configs/policies.yaml" \
  "$BIN" --mode=server > "$OUT_DIR/server.log" 2>&1 &
PERCHGUARD_PID=$!

for _ in $(seq 1 30); do
  curl -s -o /dev/null "$BASE_URL/" && break
  sleep 0.5
done

narrate "Server is up. An agent must register before it can call any tools."
narrate "The manifest declares identity, mission, allowed scope, and role — this becomes the policy baseline for the whole session."
narrate "PerchGuard responds with a session token. Every subsequent call must carry it."
pause

log "Registering agent 'claims-lab' (read_only_agent, scope=read_file/read_policy)"
REGISTER_RESP=$(curl -s -X POST "$BASE_URL/agents/register" \
  -H "Content-Type: application/json" \
  -d '{
    "apiVersion": "perchguard/v1", "kind": "AgentManifest",
    "metadata": {"id": "claims-lab", "version": "0.1.0"},
    "mission": {"summary": "Look up insurance claims and policy details for customers", "scope": ["read_file", "read_policy"]},
    "authorization": {"role": "read_only_agent"}
  }')
echo "$REGISTER_RESP" | jq .
SESSION=$(echo "$REGISTER_RESP" | jq -r .session_id)

narrate "Registered. Now the agent starts making tool calls — each one hits the admission pipeline."
narrate "Call 1: read_file  —  this tool is in the agent's declared scope. Should pass all validators."
pause

log "Call 1/3 — read_file (in scope) -> expect ALLOW"
curl -s -X POST "$BASE_URL/intercept" \
  -H "Content-Type: application/json" \
  -d "{\"uid\":\"call-1\",\"session_id\":\"$SESSION\",\"agent_id\":\"claims-lab\",\"agent_role\":\"read_only_agent\",\"user_intent\":\"look up policy file\",\"tool_call\":{\"name\":\"read_file\",\"parameters\":{\"path\":\"/workspace/policy.yaml\"}}}" \
  | jq '{uid, decision, reason}'

narrate "ALLOW — call cleared all validators. Now let's try something the agent is not permitted to do."
narrate "Call 2: write_file  —  this tool is explicitly denied for the read_only_agent role."
narrate "The ToolAuthorizationValidator will block it before it ever reaches the upstream."
pause

log "Call 2/3 — write_file (denied tool for read_only_agent) -> expect DENY"
curl -s -X POST "$BASE_URL/intercept" \
  -H "Content-Type: application/json" \
  -d "{\"uid\":\"call-2\",\"session_id\":\"$SESSION\",\"agent_id\":\"claims-lab\",\"agent_role\":\"read_only_agent\",\"user_intent\":\"overwrite the policy file\",\"tool_call\":{\"name\":\"write_file\",\"parameters\":{\"path\":\"/workspace/policy.yaml\"}}}" \
  | jq '{uid, decision, reason}'

narrate "DENY — blocked. The audit record shows it; the upstream never saw the request."
narrate "Call 3: read_policy  —  the tool is in scope this time, but a parameter contains an SSN."
narrate "The PII validator catches it and routes it to the operator review queue instead of allowing it through."
pause

log "Call 3/3 — read_policy carrying an SSN-shaped parameter -> expect HUMAN_REVIEW"
INTERCEPT_RESP=$(curl -s -X POST "$BASE_URL/intercept" \
  -H "Content-Type: application/json" \
  -d "{\"uid\":\"call-3\",\"session_id\":\"$SESSION\",\"agent_id\":\"claims-lab\",\"agent_role\":\"read_only_agent\",\"user_intent\":\"look up the customer policy\",\"tool_call\":{\"name\":\"read_policy\",\"parameters\":{\"customer_ssn\":\"123-45-6789\"}}}")
echo "$INTERCEPT_RESP" | jq '{uid, decision, reason}'

narrate "HUMAN_REVIEW — the agent is paused. The call sits in the operator queue until a human makes a decision."
narrate "We now put on the operator hat: submit the call for review, then approve it."
narrate "The approval is written back onto the original audit record — full chain of custody is preserved."
pause

log "Operator: submitting the HUMAN_REVIEW call for review, then approving it"
REVIEW_ID=$(curl -s -X POST "$BASE_URL/api/review" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$SESSION"'","agent_id":"claims-lab","tool_name":"read_policy","risk_score":0.5,"reason":"PII-shaped parameter (SSN) in scope-ambiguous call","request_uid":"call-3"}' \
  | jq -r .id)
echo "pending review id: $REVIEW_ID"
curl -s -X POST "$BASE_URL/api/review/$REVIEW_ID/approve" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"reviewed_by":"demo-operator"}'
echo "approved by demo-operator -- write-back lands on audit record call-3"

narrate "Approved. Now we close the session."
narrate "On close, PerchGuard emits a governance record: all 3 calls, every decision, risk scores, drift scores, intent baseline."
pause

log "Closing the session -> emits a governance record"
curl -s -X DELETE "$BASE_URL/api/sessions/$SESSION" \
  -H "Authorization: Bearer $API_KEY" | jq .

# Governance record write is async (FileSink.EmitAsync) — give it a moment.
sleep 1
narrate "Session closed. The governance snapshot is the verifiable record of what happened — decisions, risk, drift, and operator actions."
pause
log "Governance snapshot (snapshots/governance.json, latest entry)"
jq '.[-1]' ./snapshots/governance.json | tee "$OUT_DIR/governance-latest.json"

narrate "That's the full audit trail in one record. The same data now drives the compliance export."
pause

log "Stopping PerchGuard"
kill "$PERCHGUARD_PID"
wait "$PERCHGUARD_PID" 2>/dev/null || true
unset PERCHGUARD_PID

narrate "EU AI Act — Art. 26 deployer obligations: 11 obligations, each mapped to a policy surface or audit evidence."
narrate "This is generated directly from the audit store you just saw — not a static document."
pause

log "Compliance export — Article 26 deployer-obligations coverage"
"$BIN" --mode=compliance --doc=art26-coverage --db="$DB_PATH" --format=markdown \
  | tee "$OUT_DIR/art26-coverage.md"

narrate "Annex IV: technical documentation required for high-risk AI systems — generated from the live audit store."
pause

log "Compliance export — Annex IV technical documentation"
"$BIN" --mode=compliance --doc=annex-iv --db="$DB_PATH" --format=markdown \
  | tee "$OUT_DIR/annex-iv.md"

log "Done"
echo "Full transcript: $OUT_DIR/server.log"
echo "Reports written to: $OUT_DIR/art26-coverage.md, $OUT_DIR/annex-iv.md"
echo "snapshots/governance.json and snapshots/audit.jsonl have been restored to their pre-demo state."
