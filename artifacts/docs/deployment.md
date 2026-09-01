# Deployment

Three deployment paths in order of complexity.

---

## Docker Compose — local development

The fastest path. Runs PerchGuard, a mock agent, Jaeger for traces, and optional MCP servers in one command.

**Requirements:** Docker, Docker Compose.

```bash
# 1. Configure
cp .env.example .env   # or: touch .env
# Optional: add ANTHROPIC_API_KEY_USED_BY_PERCHGUARD=sk-ant-... to .env

# 2. Start
docker compose -f deployments/docker-compose/docker-compose.yaml up --build

# 3. Verify
curl http://localhost:8080/healthz
```

Services started:

| Service | Port | Purpose |
|---------|------|---------|
| `perchguard` | 8080 | Admission controller |
| `jaeger` | 16686 | Trace UI |
| `insurance-mcp-server` | 8090 | Example MCP tool server |
| `mock-agent` | — | Scripted smoke test (exits after run) |

Policy hot-reload: edit `configs/policies.yaml` on the host — changes take effect within 30 seconds, no restart.

Audit trail: `snapshots/audit.jsonl` on the host, appended live.

---

## k3d — local Kubernetes

Runs PerchGuard in a local single-node Kubernetes cluster. Good for testing the Helm chart and K8s RBAC before EKS.

**Requirements:** Docker, k3d, kubectl, helm.

```bash
# 1. Create cluster
k3d cluster create perchguard --port "8080:30080@loadbalancer"

# 2. Build image into the cluster (no registry needed)
docker build -t perchguard:latest .
k3d image import perchguard:latest -c perchguard

# 2b. Build the demo agent images too (Phase 10a — real k8s pods, not just docker-compose)
docker build -t insurance-agent:latest -f deployments/insurance-agent-python/Dockerfile .
docker build -t healthcare-agent:latest deployments/healthcare-agent-python
docker build -t redteam-mcp-agent:latest deployments/redteam-mcp-agent
k3d image import insurance-agent:latest healthcare-agent:latest redteam-mcp-agent:latest -c perchguard

# 2c. Build the isolation-operator image (Phase 10b — separate binary/Deployment
# from the admission controller, see cmd/perchguard-operator/main.go)
docker build -t perchguard-operator:latest -f Dockerfile.operator .
k3d image import perchguard-operator:latest -c perchguard

# 3. Deploy
export ANTHROPIC_API_KEY_USED_BY_PERCHGUARD=sk-ant-...   # optional

./deployments/k3s/bootstrap.sh

# 4. Verify
curl http://localhost:8080/healthz
```

The bootstrap script creates the `perchguard` namespace, injects secrets, and applies all manifests. Re-running is safe (idempotent).

It also applies one namespace + Deployment per demo agent (`insurance-agent`,
`healthcare-agent`, `redteam-mcp-agent`), each labeled `perchguard/intercept: "true"`.
These pods run `sleep 3600` — they exist as real, hardened k8s pods so later
`NetworkPolicy`/`RuntimeClass` work has something to attach to, but nothing runs their
task flow automatically yet. `kubectl exec -n insurance-agent deploy/insurance-agent --
python agent.py` (or the equivalent for the other two) to drive a demo by hand.

It also applies the `AgentIsolationPolicy` CRD and the `perchguard-operator` Deployment
(Phase 10b). The operator only reads `configs/policies.yaml` and reports status in this
phase — `kubectl get agentisolationpolicy default -o yaml` shows a `Reconciled`
condition and the translated `spec.egress`, but nothing is enforced against cluster
traffic until a later phase flips `spec.mode` to `Enforce`.

To use the Helm chart instead:

```bash
kubectl create namespace perchguard

helm install perchguard deployments/helm/perchguard \
  --namespace perchguard \
  --set image.repository=perchguard \
  --set image.tag=latest \
  --set image.pullPolicy=IfNotPresent \
  --set audit.pvc.enabled=false   # no persistent storage in k3d dev
```

---

## EKS Fargate — production

Single `terraform apply` from zero to a running cluster. `terraform destroy` removes everything.

**Requirements:** AWS CLI (authenticated), Terraform ≥ 1.6, Docker, kubectl, helm.

**What gets created:** VPC (2 public + 2 private subnets), EKS Fargate cluster, ECR repositories (PerchGuard, `perchguard-operator`, `redteam-mcp-agent`), AWS Load Balancer Controller, Kubernetes Gateway API (GatewayClass: alb), ALB with HTTPRoutes, PerchGuard deployment — plus, since Phase 10c, a 1-node EC2 node group running Cilium CNI, the `AgentIsolationPolicy` CRD, the `perchguard-operator` Deployment, and `redteam-mcp-agent` scheduled onto that node group.

No bastion, no VPN. kubectl works directly from your machine.

**Real cost note (Phase 10c):** everything else here is Fargate/serverless — near-zero idle cost. The new node group is a real EC2 instance billed per-hour (`t3.medium` ≈ $0.04/hr in `eu-central-1`) for as long as it exists. `terraform destroy -target=aws_eks_node_group.agent_workloads` (plus its Cilium/agent dependents) between test sessions if you don't need it running continuously. Cilium/`IsolationEnforcer` ship in `Observe` mode by default — nothing blocks real traffic until a human flips `AgentIsolationPolicy/default`'s `spec.mode` to `Enforce`.

```bash
# 1. Set environment
export AWS_ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
export AWS_REGION=eu-central-1                    # or your region
export PERCHGUARD_LLM_API_KEY=sk-ant-...          # optional
export PERCHGUARD_API_KEY=pgmk-...               # optional, auto-generated if unset

# 2. Bootstrap (ECR create → docker build → terraform apply → kubeconfig)
./deployments/eks/bootstrap.sh

# Output after ~15 minutes:
# PerchGuard is live:
#   Intercept:  http://<alb-dns>/intercept
#   Management: http://<alb-dns>/api/sessions
#   Metrics:    http://<alb-dns>/metrics
```

**Tear down:**

```bash
cd deployments/eks
terraform destroy -var="aws_account_id=$AWS_ACCOUNT_ID"
# Everything removed — VPC, EKS, ECR, ALB, IAM roles
```

**Variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `cluster_name` | `perchguard` | Cluster and resource name prefix. |
| `aws_region` | `eu-central-1` | AWS region. |
| `aws_account_id` | required | Your AWS account ID. |
| `kubernetes_version` | `1.31` | EKS version. |
| `llm_api_key` | `""` | Anthropic key for semantic firewall. |
| `perchguard_api_key` | `""` | Management API key. Auto-generated if empty. |
| `agent_node_instance_type` | `t3.medium` | EC2 instance type for the isolated-agent node group (Phase 10c). |
| `agent_node_desired_size` | `1` | Node count for the isolated-agent node group (Phase 10c). |

**TLS:** The ALB terminates HTTP by default. To add TLS, provision an ACM certificate for your domain and add to `gateway.tf`:

```hcl
# In the Gateway listeners block:
listeners = [{
  name     = "https"
  port     = 443
  protocol = "HTTPS"
  tls = {
    certificateRefs = [{
      group = "gateway.networking.k8s.io"
      kind  = "Gateway"
      name  = "<your-acm-cert-arn>"
    }]
  }
}]
```

---

## TLS setup (all environments)

Set two environment variables and PerchGuard switches to HTTPS automatically:

```bash
PERCHGUARD_TLS_CERT=/path/to/tls.crt
PERCHGUARD_TLS_KEY=/path/to/tls.key
PERCHGUARD_ADDR=:8443
```

In the Helm chart:

```yaml
tls:
  enabled: true
  secretName: perchguard-tls   # K8s Secret with tls.crt and tls.key
```

For local dev, generate a self-signed cert:

```bash
openssl req -x509 -newkey rsa:4096 -keyout tls.key -out tls.crt \
  -days 365 -nodes -subj "/CN=perchguard.local"
```

---

← [Docs index](README.md)
