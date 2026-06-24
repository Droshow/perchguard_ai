#!/usr/bin/env bash
# PerchGuard EKS bootstrap
#
# Usage:
#   export AWS_ACCOUNT_ID=123456789012
#   export AWS_REGION=eu-central-1                 # optional, default: eu-central-1
#   export PERCHGUARD_LLM_API_KEY=sk-ant-...       # optional
#   export PERCHGUARD_API_KEY=pgmk-...             # optional, auto-generated if unset
#   ./deployments/eks/bootstrap.sh
#
# Tear down:
#   cd deployments/eks && terraform destroy -var="aws_account_id=$AWS_ACCOUNT_ID"
#
# Prerequisites: aws-cli, terraform >= 1.6, docker, kubectl, helm

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Load .env from repo root if present (local secrets — never committed)
if [[ -f "${REPO_ROOT}/.env" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "${REPO_ROOT}/.env"
  set +a
  echo "[bootstrap] loaded .env from ${REPO_ROOT}/.env"
fi

AWS_REGION="${AWS_REGION:-eu-central-1}"
CLUSTER_NAME="${CLUSTER_NAME:-perchguard}"

if [[ -z "${AWS_ACCOUNT_ID:-}" ]]; then
  AWS_ACCOUNT_ID="$(aws sts get-caller-identity --query Account --output text)"
  echo "[bootstrap] detected AWS account: ${AWS_ACCOUNT_ID}"
fi

ECR_URL="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/${CLUSTER_NAME}"

# ── 1. Create ECR repo first so we have somewhere to push ────────────────────
echo "[1/4] Provisioning ECR repository..."
cd "${SCRIPT_DIR}"
terraform init -input=false
terraform apply -input=false -auto-approve \
  -target=aws_ecr_repository.perchguard \
  -var="aws_account_id=${AWS_ACCOUNT_ID}" \
  -var="aws_region=${AWS_REGION}"

# ── 2. Build and push the PerchGuard image ───────────────────────────────────
echo "[2/4] Building and pushing PerchGuard image to ECR..."
aws ecr get-login-password --region "${AWS_REGION}" \
  | docker login --username AWS --password-stdin "${ECR_URL}"

cd "${REPO_ROOT}"
docker build -t "${CLUSTER_NAME}:latest" .
docker tag "${CLUSTER_NAME}:latest" "${ECR_URL}:latest"
docker push "${ECR_URL}:latest"
echo "  Pushed: ${ECR_URL}:latest"

# ── 3. Two-phase terraform apply ─────────────────────────────────────────────
# Phase A: AWS-only resources (VPC, EKS cluster, Fargate, IAM, OIDC, CW dashboard).
# The kubernetes/helm providers need a live cluster endpoint — they fail at plan
# time if we attempt a full apply before the cluster exists.
echo "[3/4] Phase A — provisioning AWS infrastructure (EKS ~12 min)..."
cd "${SCRIPT_DIR}"
TF_VARS="-var=aws_account_id=${AWS_ACCOUNT_ID} -var=aws_region=${AWS_REGION} -var=llm_api_key=${PERCHGUARD_LLM_API_KEY:-} -var=perchguard_api_key=${PERCHGUARD_API_KEY:-}"
terraform apply -input=false -auto-approve $TF_VARS \
  -target=aws_eks_cluster.perchguard \
  -target=aws_eks_fargate_profile.perchguard \
  -target=aws_eks_fargate_profile.kube_system \
  -target=aws_iam_role.adot \
  -target=aws_iam_role_policy.adot_cloudwatch \
  -target=aws_cloudwatch_dashboard.perchguard

# ── 4. Configure kubectl so the kubernetes/helm providers can connect ─────────
echo "[4/5] Configuring kubectl..."
aws eks update-kubeconfig --name "${CLUSTER_NAME}" --region "${AWS_REGION}"

# Phase B: CoreDNS patch + Gateway API CRDs.
# kubernetes_manifest validates GVK at plan time — the CRDs must exist in the
# cluster before we run the full apply, otherwise the plan fails with
# "no matches for kind GatewayClass".
echo "[4/5] Phase B — installing CoreDNS patch and Gateway API CRDs..."
terraform apply -input=false -auto-approve $TF_VARS \
  -target=null_resource.coredns_fargate_patch \
  -target=null_resource.gateway_api_crds

# Phase C: Full apply — all remaining resources (LBC, PerchGuard Helm, ADOT,
# Gateway, HTTPRoutes) now that the cluster is healthy and CRDs are installed.
echo "[4/5] Phase C — deploying workloads (LBC, PerchGuard, ADOT, Gateway ~6 min)..."
terraform apply -input=false -auto-approve $TF_VARS

echo "[5/5] Waiting up to 3 minutes for ALB provisioning..."
for i in $(seq 1 24); do
  ALB=$(kubectl get gateway perchguard -n perchguard \
    -o jsonpath='{.status.addresses[0].value}' 2>/dev/null || true)
  if [[ -n "${ALB}" ]]; then
    echo ""
    echo "PerchGuard is live:"
    echo "  Intercept:   http://${ALB}/intercept"
    echo "  Management:  http://${ALB}/api/sessions  (Bearer: check startup logs)"
    echo "  Metrics:     http://${ALB}/metrics"
    echo "  Health:      http://${ALB}/healthz"
    exit 0
  fi
  sleep 5
done

echo ""
echo "ALB still provisioning. Check status with:"
echo "  kubectl get gateway perchguard -n perchguard"
echo "  kubectl get httproute -n perchguard"
