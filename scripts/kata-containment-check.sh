#!/usr/bin/env bash
# Phase 10f — runs the runc/kata negative-control pair (deployments/k3s/kata/
# containment-test-*.yaml) and asserts they report a different kernel version.
# That's the real containment signal: a Kata pod runs its own guest kernel, so
# if KERNEL matches between the two pods, Kata isn't actually providing kernel
# isolation on this node — a RuntimeClass label alone proves nothing.
set -euo pipefail

NAMESPACE="redteam-mcp-agent"
MANIFEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../deployments/k3s/kata" && pwd)"

cleanup() {
  kubectl -n "${NAMESPACE}" delete pod containment-test-runc containment-test-kata \
    --ignore-not-found --wait=false >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Applying containment-test pods..."
kubectl apply -f "${MANIFEST_DIR}/containment-test-runc.yaml"
kubectl apply -f "${MANIFEST_DIR}/containment-test-kata.yaml"

for pod in containment-test-runc containment-test-kata; do
  echo "Waiting for ${pod} to complete..."
  for _ in $(seq 1 24); do
    phase="$(kubectl -n "${NAMESPACE}" get pod "${pod}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
    [[ "$phase" == "Succeeded" || "$phase" == "Failed" ]] && break
    sleep 5
  done
done

runc_out="$(kubectl -n "${NAMESPACE}" logs containment-test-runc)"
kata_out="$(kubectl -n "${NAMESPACE}" logs containment-test-kata)"

echo
echo "== runc =="
echo "$runc_out"
echo
echo "== kata-qemu =="
echo "$kata_out"

runc_kernel="$(echo "$runc_out" | grep '^KERNEL:' | cut -d' ' -f2)"
kata_kernel="$(echo "$kata_out" | grep '^KERNEL:' | cut -d' ' -f2)"

echo
if [[ -n "$runc_kernel" && -n "$kata_kernel" && "$runc_kernel" != "$kata_kernel" ]]; then
  echo "RESULT: PASS — runc kernel (${runc_kernel}) != kata-qemu kernel (${kata_kernel}). Real kernel-level isolation confirmed."
  exit 0
else
  echo "RESULT: FAIL — kernels match or are unreadable (runc='${runc_kernel}' kata='${kata_kernel}'). Kata is not providing kernel isolation on this node."
  exit 1
fi
