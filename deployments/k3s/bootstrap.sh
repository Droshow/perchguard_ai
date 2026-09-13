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

# Kata sandbox layer (Phase 10e) — the operator Deployment (applied right after)
# mounts a perchguard-kata-webhook-tls secret, so it must exist first or the pod
# sits in CreateContainerConfigError. deployments/eks/kata.tf generates this cert
# with Terraform's tls provider for EKS; there's no Terraform in this k3s path, so
# openssl does the equivalent here. Skipped (idempotent) if the secret already
# exists, same pattern as the LLM secret above.
KATA_TLS_SECRET="perchguard-kata-webhook-tls"
command -v openssl >/dev/null || { echo "ERROR: openssl is required to generate the Kata webhook TLS cert." >&2; exit 1; }
echo "Kata sandbox layer — webhook TLS + kata-deploy..."
if kubectl get secret "${KATA_TLS_SECRET}" --namespace "${NAMESPACE}" &>/dev/null; then
  echo "  Secret '${KATA_TLS_SECRET}' already exists — skipping cert generation."
else
  CERT_DIR="$(mktemp -d)"
  trap 'rm -rf "${CERT_DIR}"' EXIT
  openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -keyout "${CERT_DIR}/tls.key" -out "${CERT_DIR}/tls.crt" \
    -subj "/CN=perchguard-operator.perchguard.svc" \
    -addext "subjectAltName=DNS:perchguard-operator.perchguard.svc,DNS:perchguard-operator.perchguard.svc.cluster.local" \
    2>/dev/null
  kubectl create secret tls "${KATA_TLS_SECRET}" \
    --namespace "${NAMESPACE}" \
    --cert="${CERT_DIR}/tls.crt" --key="${CERT_DIR}/tls.key"
  echo "  Secret '${KATA_TLS_SECRET}' created."
fi

kubectl apply -f "${SCRIPT_DIR}/perchguard-operator/deployment.yaml"

# kata-deploy is scoped via nodeSelector to perchguard/kata-capable nodes
# (deployments/eks/kata.tf) — inert on a stock k3s node with no such label, same
# as the EKS node group defaulting to desired_size=0.
echo "  Applying kata-deploy (DaemonSet + RuntimeClass — inert without a kata-capable node)..."
kubectl apply -f "${SCRIPT_DIR}/kata/kata-deploy.yaml"

echo "  Rendering and applying the Kata mutating webhook..."
CA_BUNDLE="$(kubectl get secret "${KATA_TLS_SECRET}" --namespace "${NAMESPACE}" -o jsonpath='{.data.tls\.crt}')"
sed "s|\${WEBHOOK_CA_BUNDLE}|${CA_BUNDLE}|" "${SCRIPT_DIR}/kata/mutating-webhook.yaml" | kubectl apply -f -

echo ""
echo "Done. Check rollout status with:"
echo "  kubectl rollout status deployment/perchguard -n ${NAMESPACE}"
