#!/usr/bin/env bash
# PerchGuard Red Team Observer — convenience wrapper
# Phase 5 + Phase 5B Governance Depth (Caps 1-5)
#
# Surfaces observed:
#   [fleet]      GET /api/fleet/summary  — risk distribution, delegated sessions, delegation depth
#   [audit]      snapshots/audit.jsonl   — per-call decisions, policy_version, lineage refs
#   [governance] snapshots/governance.json — session close events, parent/child chain
#   [policy]     GET /api/pipeline        — policy hash baseline + hot-reload alerts (Cap 1)
#
# Usage (from perchguard/ repo root):
#   ./scripts/run_observe.sh
#   ./scripts/run_observe.sh --interval 10
#   ./scripts/run_observe.sh --url http://localhost:8080 --snapshots ./snapshots
#
# The script sources .env if present so PERCHGUARD_API_KEY and PERCHGUARD_URL
# are picked up automatically when running locally outside Docker.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Source .env from repo root if present (sets PERCHGUARD_API_KEY etc.)
if [[ -f "$REPO_ROOT/.env" ]]; then
  set -o allexport
  # shellcheck source=/dev/null
  source "$REPO_ROOT/.env"
  set +o allexport
fi

exec python3 "$SCRIPT_DIR/observe.py" "$@"
