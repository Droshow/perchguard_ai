#!/usr/bin/env bash
# PerchGuard Demo Video Recording Script
# 
# Full walkthrough Scenes 2-11: stack up, agent tasks, Grafana, compliance export.
# Built-in pauses for narration and screen transitions.
#
# Usage (from repo root):
#   bash scripts/demo-admissions.sh              # Run demo (services stay up)
#   bash scripts/demo-admissions.sh --clean      # Clean up services after recording
#   bash scripts/demo-admissions.sh --help       # Show help
#
# Prerequisites:
#   - .env at repo root with ANTHROPIC_API_KEY_USED_BY_PERCHGUARD set
#   - Terminal font 18-20pt, high contrast theme, wide window
#   - Three browser tabs open: Grafana (:3000), Jaeger (:16686), spare
#   - OBS or screen recorder ready

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

# Load .env so ANTHROPIC_API_KEY is available for docker compose exec
if [[ -f .env ]]; then
  set -o allexport
  source .env
  set +o allexport
fi

# Colors for terminal output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;36m'
NC='\033[0m' # No Color

log() { printf "\n${BLUE}=== %s ===${NC}\n" "$1"; }
scene_title() { printf "\n${GREEN}[SCENE %s]${NC} %s\n" "$1" "$2"; }
narrator() { printf "\n${YELLOW}NARRATOR:${NC} %s\n" "$*"; }
pause() { printf "\n${YELLOW}[Press Enter to continue]${NC}\n"; read -r; }

# Handle --help flag
if [[ "${1:-}" == "--help" ]]; then
  printf "Usage: bash scripts/demo-admissions.sh [OPTIONS]\n\n"
  printf "Options:\n"
  printf "  --clean       Clean up Docker services after recording\n"
  printf "  --help        Show this help message\n\n"
  printf "By default, services stay running so you can record the video.\n"
  printf "Run with --clean afterward to tear down the stack.\n"
  exit 0
fi

# Handle --clean flag (just cleanup and exit)
if [[ "${1:-}" == "--clean" ]]; then
  log "Cleaning up Docker services"
  docker compose \
    -f deployments/docker-compose/docker-compose.yaml \
    -f deployments/docker-compose/docker-compose.observability.yml \
    down
  printf "\n${GREEN}Services cleaned up.${NC}\n"
  exit 0
fi

# Default behavior: DON'T cleanup on exit (user will run --clean manually)
cleanup() {
  printf "\n${YELLOW}To clean up services after recording, run:${NC}\n"
  printf "  bash scripts/demo-admissions.sh --clean\n\n"
  printf "${GREEN}Services are still running — switch to recording now if needed.${NC}\n"
}
trap cleanup EXIT

# ──────────────────────────────────────────────────────────────────
# SCENE 2 — Stack up [0:25–1:00]
# ──────────────────────────────────────────────────────────────────

scene_title "2" "Stack up (0:25–1:00)"

narrator "One docker compose command. That's it. Two compose files stacked—the base stack plus observability."
pause

log "Starting Docker services"
printf "${BLUE}Running:${NC}\n"
printf "  docker compose -f deployments/docker-compose/docker-compose.yaml \\\\\n"
printf "    -f deployments/docker-compose/docker-compose.observability.yml \\\\\n"
printf "    up -d --build\n\n"

docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  -f deployments/docker-compose/docker-compose.observability.yml \
  up -d --build \
  perchguard insurance-mcp-server insurance-agent-python \
  jaeger prometheus loki grafana

narrator "PerchGuard itself—the admission controller, running on 8080. The insurance MCP server at 8090 with real tools the agent can call. Jaeger for traces. Prometheus scraping metrics. Grafana dashboard, which we'll watch in real time."
pause

log "Waiting for services to be healthy"
for i in {1..30}; do
  if curl -s http://localhost:8080/healthz >/dev/null 2>&1; then
    echo "✓ PerchGuard is healthy"
    break
  fi
  echo "Waiting... ($i/30)"
  sleep 1
done

pause

log "Checking service status"
docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  -f deployments/docker-compose/docker-compose.observability.yml \
  ps
pause

log "Testing healthz endpoint"
curl -s http://localhost:8080/healthz | jq .
pause

narrator "No mocks. No fake tool servers. No stubbed-out policies. Real binary, real MCP server, real metrics stack. Everything runs in the same network—agent calls agent, agent calls PerchGuard, PerchGuard calls MCP, and every single decision gets traced and logged."
narrator "Now let's look at the dashboard before anything runs. Empty slate. This is the baseline."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 3 — Grafana baseline [1:00–1:20]
# ──────────────────────────────────────────────────────────────────

scene_title "3" "Grafana baseline (1:00–1:20)"

narrator "Switch to Grafana at http://localhost:3000 (admin / perchguard). Open the PerchGuard Fleet dashboard. Everything is zero — no decisions yet."
narrator "The dashboard is live and empty. Every decision the agent triggers will show up here in real time. This is what 'no agents have run yet' looks like."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 4 — Task 1: clean path → all ALLOW [1:20–2:10]
# ──────────────────────────────────────────────────────────────────

scene_title "4" "Task 1: clean path → all ALLOW (1:20–2:10)"

narrator "First task: summarise a policy. developer_agent role, read_policy in scope, clean parameters, clean output. Every validator passes. This is the happy path — the agent is fast, autonomous, and completely ungoverned-looking from the outside. PerchGuard saw everything and said yes."
pause

log "Running Task 1: summarise_policy"
docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  exec -e ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-${ANTHROPIC_API_KEY_USED_BY_PERCHGUARD:-}}" \
  insurance-agent-python \
  python agent.py summarise_policy
pause

narrator "Look at the INBOUND and OUTBOUND symbols: ✓ means ALLOW. Every call cleared. Now switch to Grafana and watch the ALLOW count tick up."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 5 — Task 2: outbound prompt injection → SANITIZE [2:10–3:00]
# ──────────────────────────────────────────────────────────────────

scene_title "5" "Task 2: outbound prompt injection → SANITIZE (2:10–3:00)"

narrator "Second task: read_policy is allowed for this role — inbound clears. But the document the MCP server returns contains a hidden instruction injected into the policy text. PerchGuard's output validator catches it and sanitizes the content before it ever reaches the Claude context. The agent never sees the injection."
pause

log "Running Task 2: poisoned_claim"
docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  exec -e ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-${ANTHROPIC_API_KEY_USED_BY_PERCHGUARD:-}}" \
  insurance-agent-python \
  python agent.py poisoned_claim
pause

narrator "Look for the ~ MUTATE symbol on the OUTBOUND line. That means the output was modified. The hidden instruction was stripped. Inbound ALLOW + outbound MUTATE is different from a DENY."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 6 — Task 3: role escalation → DENY × 2 [3:00–3:50]
# ──────────────────────────────────────────────────────────────────

scene_title "6" "Task 3: role escalation → DENY × 2 (3:00–3:50)"

narrator "Third task: the same agent, but now running as read_only_agent and told to write a report and run a SQL query. It tries. PerchGuard blocks both — not because the agent changed its mind, but because the policy says no for this role. The gate doesn't care how the agent was prompted."
narrator "This is the money shot. Hold on the two DENY lines longer than anything else in the video."
pause

log "Running Task 3: escalation"
docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  exec -e ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-${ANTHROPIC_API_KEY_USED_BY_PERCHGUARD:-}}" \
  insurance-agent-python \
  python agent.py escalation
pause

narrator "Look for the ✗ symbols. The write_report and run_sql calls are both blocked. These are the DENYs. Switch to Grafana now and watch the DENY count spike."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 7 — Task 4: data exfiltration → DENY [3:50–4:30]
# ──────────────────────────────────────────────────────────────────

scene_title "7" "Task 4: data exfiltration → DENY (3:50–4:30)"

narrator "Fourth task: the agent searches claims, writes a report — both allowed — then tries to POST the data to an external URL. PerchGuard's data exfiltration validator extracts every URL from tool parameters and checks it against the allowlist. finbridge-external.io is not on it. Blocked."
pause

log "Running Task 4: exfiltration"
docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  exec -e ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY:-${ANTHROPIC_API_KEY_USED_BY_PERCHGUARD:-}}" \
  insurance-agent-python \
  python agent.py exfiltration
pause

narrator "The send_notification call gets DENY — blocked at inbound. The destination URL triggered the data exfiltration validator."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 8 — Grafana: the full picture [4:30–5:10]
# ──────────────────────────────────────────────────────────────────

scene_title "8" "Grafana: the full picture (4:30–5:10)"

narrator "Four tasks, four sessions, one dashboard. Walk through the dashboard panels: decision breakdown (ALLOW/DENY/MUTATE counts), risk score timeline, tool call rate, session count. You can see exactly where the agent was clean, where it was blocked, and where an output was sanitized. This is what Article 14 oversight looks like in practice — not a log file you search after an incident, a live view you can act on."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 9 — Jaeger: one trace end-to-end [5:10–5:40]
# ──────────────────────────────────────────────────────────────────

scene_title "9" "Jaeger: one trace end-to-end (5:10–5:40)"

narrator "Switch to Jaeger at http://localhost:16686. Search for service 'perchguard', find the escalation session trace. Expand it — show the two DENY spans back to back. Every admission decision is an OTEL span. The full trace shows the exact chain of events, timestamped, linked. If something goes wrong in production, this is how you reconstruct it."
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 10 — Governance snapshot + compliance export [5:40–6:30]
# ──────────────────────────────────────────────────────────────────

scene_title "10" "Governance snapshot + compliance export (5:40–6:30)"

narrator "The governance JSON captures every session: declared intent, actual decisions, risk scores, drift from baseline. The compliance export turns that same database into the documents an auditor would ask for — Article 26 deployer obligations and Annex IV technical documentation. Not written retroactively. Generated from what actually happened."
pause

log "Showing latest governance record"
jq '.[-1]' snapshots/governance.json
pause

log "Generating Article 26 compliance export"
docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  exec perchguard /app/perchguard --mode=compliance --doc=art26-coverage --format=markdown \
  | head -40
pause

log "Generating Annex IV compliance export"
docker compose \
  -f deployments/docker-compose/docker-compose.yaml \
  exec perchguard /app/perchguard --mode=compliance --doc=annex-iv --format=markdown \
  | head -40
pause

# ──────────────────────────────────────────────────────────────────
# SCENE 11 — Stack down + close [6:30–6:50]
# ──────────────────────────────────────────────────────────────────

scene_title "11" "Stack down + close (6:30–6:50)"

narrator "One compose down command to clean everything up. Everything is gone—no state left behind. Open source. One compose file. Links in the description."
pause

printf "\n${GREEN}Demo recording complete!${NC}\n"
printf "Total runtime: ~6m 50s\n"
printf "\nServices are still running. When you're done recording, run:\n"
printf "  ${YELLOW}bash scripts/demo-admissions.sh --clean${NC}\n"
