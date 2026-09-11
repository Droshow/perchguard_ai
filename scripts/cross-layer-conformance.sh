#!/usr/bin/env bash
# Phase 10f — cross-layer conformance check.
#
# Diffs PerchGuard's app-layer audit denials (DataExfiltrationValidator, via
# /api/audit) against Cilium's Hubble flow-level drops for the same window.
# The finding that actually matters is asymmetric: an app-layer DENY with NO
# matching Hubble drop means the network layer would let through exactly what
# the app layer blocks — i.e. governance that only exists on paper. The
# reverse (a Hubble drop with no app-layer DENY) is expected noise (DNS,
# health checks, anything the app layer never saw as a tool call) and is
# reported as informational, not a finding.
#
# Requires: curl, jq, the `hubble` CLI (https://github.com/cilium/hubble/releases)
# port-forwarded or otherwise reachable at $HUBBLE_SERVER, and PerchGuard's
# management API reachable at $PERCHGUARD_URL with $PERCHGUARD_API_KEY.
#
# Usage:
#   kubectl port-forward -n cilium-system svc/hubble-relay 4245:80 &
#   export PERCHGUARD_URL=http://<alb>  PERCHGUARD_API_KEY=pgmk-...
#   ./scripts/cross-layer-conformance.sh [since]   # since defaults to 1h

set -euo pipefail

SINCE="${1:-1h}"
PERCHGUARD_URL="${PERCHGUARD_URL:-http://localhost:8080}"
HUBBLE_SERVER="${HUBBLE_SERVER:-localhost:4245}"

for bin in curl jq hubble; do
  command -v "$bin" >/dev/null || { echo "missing required binary: $bin" >&2; exit 1; }
done
if [[ -z "${PERCHGUARD_API_KEY:-}" ]]; then
  echo "PERCHGUARD_API_KEY is required (management API bearer key)" >&2
  exit 1
fi

echo "== App-layer denials (last ${SINCE}, PerchGuard /api/audit) =="
denied_json="$(curl -sf -H "Authorization: Bearer ${PERCHGUARD_API_KEY}" \
  "${PERCHGUARD_URL}/api/audit?decision=DENY&limit=1000")"

# Extract the destination host from reasons shaped like:
#   "Destination 'http://reports.finbridge-external.io/submit' is not in the allow list..."
# pkg/admission/validator/data_exfiltration.go is the source of this exact wording.
mapfile -t app_denials < <(echo "$denied_json" | jq -r '
  .[] | select(.reason | test("^Destination ")) |
  (.reason | capture("Destination '\''(?<url>[^'\'']+)'\''").url)
' | sed -E "s#^[a-z]+://##; s#/.*##" | sort -u)

echo "  ${#app_denials[@]} distinct denied destination(s): ${app_denials[*]:-none}"

echo
echo "== Hubble-observed drops (last ${SINCE}) =="
hubble_json="$(hubble observe --server "${HUBBLE_SERVER}" --verdict DROPPED \
  --since "${SINCE}" --output json 2>/dev/null || true)"

mapfile -t hubble_dropped_names < <(echo "$hubble_json" | jq -r '
  select(.flow.destination_names != null) | .flow.destination_names[]
' | sort -u)

echo "  ${#hubble_dropped_names[@]} distinct dropped destination name(s): ${hubble_dropped_names[*]:-none}"

echo
echo "== Cross-layer diff =="
findings=0
for host in "${app_denials[@]:-}"; do
  [[ -z "$host" ]] && continue
  matched=false
  for dropped in "${hubble_dropped_names[@]:-}"; do
    if [[ "$dropped" == *"$host"* || "$host" == *"$dropped"* ]]; then
      matched=true
      break
    fi
  done
  if [[ "$matched" == true ]]; then
    echo "  OK    ${host}: app-layer DENY, Hubble also dropped it — both layers agree"
  else
    echo "  DRIFT ${host}: app-layer DENY but NO matching Hubble drop found — network layer may not be enforcing this"
    findings=$((findings + 1))
  fi
done

echo
if [[ "$findings" -gt 0 ]]; then
  echo "RESULT: ${findings} drift finding(s) — network-layer CiliumNetworkPolicy does not yet cover every app-layer denial."
  echo "        Check the target namespace's AgentIsolationPolicy mode and egress.allowedDestinations/blockedDestinations."
  exit 1
else
  echo "RESULT: no drift — every app-layer denial in this window is also enforced at the network layer."
fi
