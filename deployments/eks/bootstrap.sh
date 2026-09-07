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
ECR_URL_REDTEAM="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/${CLUSTER_NAME}-redteam-mcp-agent"
ECR_URL_OPERATOR="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/${CLUSTER_NAME}-operator"

# Project-local kubeconfig (deployments/eks/.kubeconfig), rendered once by
# Terraform (local_file.kubeconfig in kubeconfig.tf) and never merged into
# ~/.kube/config — see kubeconfig.tf for why that used to get corrupted.
export KUBECONFIG="${SCRIPT_DIR}/.kubeconfig"

# ── 1. Create ECR repos first so we have somewhere to push ───────────────────
echo "[1/4] Provisioning ECR repositories..."
cd "${SCRIPT_DIR}"
terraform init -input=false
terraform apply -input=false -auto-approve \
  -target=aws_ecr_repository.perchguard \
  -target=aws_ecr_repository.redteam_mcp_agent \
  -target=aws_ecr_repository.perchguard_operator \
  -var="aws_account_id=${AWS_ACCOUNT_ID}" \
  -var="aws_region=${AWS_REGION}"

# ── 2. Build and push images ──────────────────────────────────────────────────
echo "[2/4] Building and pushing images to ECR..."
aws ecr get-login-password --region "${AWS_REGION}" \
  | docker login --username AWS --password-stdin "${ECR_URL}"

cd "${REPO_ROOT}"
docker build -t "${CLUSTER_NAME}:latest" .
docker tag "${CLUSTER_NAME}:latest" "${ECR_URL}:latest"
docker push "${ECR_URL}:latest"
echo "  Pushed: ${ECR_URL}:latest"

# Phase 10c: the operator binary and the redteam-mcp-agent demo image, moved
# onto the isolated-agent node group (see deployments/eks/redteam-mcp-agent/).
docker build -t "perchguard-operator:latest" -f Dockerfile.operator .
docker tag "perchguard-operator:latest" "${ECR_URL_OPERATOR}:latest"
docker push "${ECR_URL_OPERATOR}:latest"
echo "  Pushed: ${ECR_URL_OPERATOR}:latest"

docker build -t "redteam-mcp-agent:latest" deployments/redteam-mcp-agent
docker tag "redteam-mcp-agent:latest" "${ECR_URL_REDTEAM}:latest"
docker push "${ECR_URL_REDTEAM}:latest"
echo "  Pushed: ${ECR_URL_REDTEAM}:latest"

# ── 3. Two-phase terraform apply ─────────────────────────────────────────────
# Phase A: AWS-only resources (VPC, EKS cluster, Fargate, IAM, OIDC, CW dashboard).
# The kubernetes/helm providers need a live cluster endpoint — they fail at plan
# time if we attempt a full apply before the cluster exists.
echo "[3/4] Phase A — provisioning AWS infrastructure (EKS ~12 min)..."
cd "${SCRIPT_DIR}"
TF_VARS="-var=aws_account_id=${AWS_ACCOUNT_ID} -var=aws_region=${AWS_REGION} -var=llm_api_key=${PERCHGUARD_LLM_API_KEY:-} -var=perchguard_api_key=${PERCHGUARD_API_KEY:-}"
terraform apply -input=false -auto-approve $TF_VARS \
  -target=aws_internet_gateway.perchguard \
  -target=aws_eip.nat \
  -target=aws_nat_gateway.perchguard \
  -target=aws_route_table.public \
  -target=aws_route_table.private \
  -target=aws_route_table_association.public \
  -target=aws_route_table_association.private \
  -target=aws_eks_cluster.perchguard \
  -target=aws_eks_fargate_profile.perchguard \
  -target=aws_eks_fargate_profile.kube_system \
  -target=aws_iam_role.adot \
  -target=aws_iam_role_policy.adot_cloudwatch \
  -target=aws_cloudwatch_dashboard.perchguard \
  -target=aws_eks_node_group.agent_workloads

# ── 4. Render the project-local kubeconfig (local_file.kubeconfig) ───────────
# Nothing here calls `aws eks update-kubeconfig` — that writes to the shared
# ~/.kube/config, and Terraform's own kubectl provisioners running in parallel
# is exactly what corrupted it before. Terraform is the sole writer of
# ${KUBECONFIG}; everything below only reads it.
echo "[4/5] Rendering project-local kubeconfig..."
terraform apply -input=false -auto-approve $TF_VARS -target=local_file.kubeconfig
kubectl --kubeconfig "${KUBECONFIG}" config view --raw >/dev/null
kubectl --kubeconfig "${KUBECONFIG}" cluster-info >/dev/null
echo "  KUBECONFIG=${KUBECONFIG} — validated."

# Phase B: CoreDNS patch + Gateway API CRDs.
# kubernetes_manifest validates GVK at plan time — the CRDs must exist in the
# cluster before we run the full apply, otherwise the plan fails with
# "no matches for kind GatewayClass".
echo "[4/5] Phase B — installing CoreDNS patch and Gateway API CRDs..."
terraform apply -input=false -auto-approve $TF_VARS \
  -target=null_resource.coredns_fargate_patch \
  -target=null_resource.gateway_api_crds

# Phase C: Full apply — all remaining resources (LBC, PerchGuard Helm, ADOT,
# Gateway, HTTPRoutes, Cilium, the AgentIsolationPolicy CRD, the operator) now
# that the cluster is healthy and CRDs are installed.
echo "[4/5] Phase C — deploying workloads (LBC, PerchGuard, ADOT, Gateway, Cilium, operator ~8 min)..."
terraform apply -input=false -auto-approve $TF_VARS

# Phase 10c: redteam-mcp-agent is deliberately NOT a Terraform resource — see the
# shift-left note in artifacts/docs/PHASE10-K8S-ISOLATION-OPERATOR.md's Phase 10c
# section. Onboarding a governed agent stays "write a Deployment, label the
# namespace, kubectl apply", same as it already is on k3s.
echo "[4/5] Deploying redteam-mcp-agent onto the isolated-agent node group..."
kubectl apply -f "${SCRIPT_DIR}/redteam-mcp-agent/namespace.yaml"
sed "s|__REDTEAM_MCP_AGENT_IMAGE__|${ECR_URL_REDTEAM}:latest|" \
  "${SCRIPT_DIR}/redteam-mcp-agent/deployment.yaml" | kubectl apply -f -

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
