#!/usr/bin/env bash
# PerchGuard K3s bootstrap
# Creates the namespace, injects the LLM API key as a Secret, then applies all manifests.
#
# Usage:
#   export ANTHROPIC_API_KEY_USED_BY_PERCHGUARD=sk-ant-...
#   ./deployments/k3s/bootstrap.sh
#
# Re-running is safe — existing resources are left untouched (apply is idempotent,
# secret create is skipped if the secret already exists).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
NAMESPACE="perchguard"
SECRET_NAME="perchguard-llm"

# --- load .env from repo root if present ---
ENV_FILE="${REPO_ROOT}/.env"
if [[ -f "${ENV_FILE}" ]]; then
  set -o allexport
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +o allexport
fi

# --- require API key ---
if [[ -z "${ANTHROPIC_API_KEY_USED_BY_PERCHGUARD:-}" ]]; then
  echo "ERROR: ANTHROPIC_API_KEY_USED_BY_PERCHGUARD is not set."
  echo "  Add it to ${ENV_FILE} or export it before running this script."
  exit 1
fi

echo "[1/3] Applying namespace..."
kubectl apply -f "${SCRIPT_DIR}/namespace.yaml"

echo "[2/3] Creating LLM secret (skipped if already exists)..."
if kubectl get secret "${SECRET_NAME}" --namespace "${NAMESPACE}" &>/dev/null; then
  echo "  Secret '${SECRET_NAME}' already exists — skipping."
else
  kubectl create secret generic "${SECRET_NAME}" \
    --namespace "${NAMESPACE}" \
    --from-literal=api-key="${ANTHROPIC_API_KEY_USED_BY_PERCHGUARD}"
  echo "  Secret '${SECRET_NAME}' created."
fi

echo "[3/3] Applying manifests..."
kubectl apply -f "${SCRIPT_DIR}/rbac.yaml"
kubectl apply -f "${SCRIPT_DIR}/configmap.yaml"
kubectl apply -f "${SCRIPT_DIR}/service.yaml"
kubectl apply -f "${SCRIPT_DIR}/deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/webhook-config.yaml"

# Demo agent workloads (Phase 10a) — idle pods, exec in to run their demos.
kubectl apply -f "${SCRIPT_DIR}/insurance-agent/namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/insurance-agent/deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/healthcare-agent/namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/healthcare-agent/deployment.yaml"
kubectl apply -f "${SCRIPT_DIR}/redteam-mcp-agent/namespace.yaml"
kubectl apply -f "${SCRIPT_DIR}/redteam-mcp-agent/deployment.yaml"

# Isolation operator (Phase 10b) — CRD before consumer: the operator's own RBAC/
# Deployment reference the security.perchguard.io/v1alpha1 API this CRD registers.
kubectl apply -f "${SCRIPT_DIR}/agent-isolation-policy-crd.yaml"
kubectl apply -f "${SCRIPT_DIR}/perchguard-operator/rbac.yaml"
kubectl apply -f "${SCRIPT_DIR}/perchguard-operator/deployment.yaml"

echo ""
echo "Done. Check rollout status with:"
echo "  kubectl rollout status deployment/perchguard -n ${NAMESPACE}"
