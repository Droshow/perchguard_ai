# EKS Fargate Deployment — Lessons Learned

First real deployment: 2026-05-15. Expected 15–20 min. Took ~2.5 hours.
This doc records every gotcha so the next run is actually 15 minutes.

---

## 1. Fargate pods need a NAT gateway — not an IGW directly

**What happened:** The original `main.tf` comment said
_"Fargate pods get public IPs via the Internet Gateway."_ This is wrong.
Fargate pod ENIs are assigned private IPs only. `map_public_ip_on_launch`
on the subnet applies to EC2 instances, not Fargate ENIs. Pods couldn't
reach ECR to pull images and sat in `Pod provisioning timed out` forever.

**Fix:** Private subnets + NAT gateway. Public subnets stay for the ALB.
Fargate profiles now point at the private subnets.

**TF resources added:** `aws_eip.nat`, `aws_nat_gateway.perchguard`,
`aws_subnet.private`, `aws_route_table.private`,
`aws_route_table_association.private`.

**Time lost:** ~25 min

---

## 2. Terraform two-phase apply — kubernetes provider needs a live cluster

**What happened:** Running `terraform apply` on the full stack fails at
plan time with:
```
Error: Failed to construct REST client
cannot create REST client: no client config
```
The `kubernetes` and `helm` providers try to connect to the cluster during
planning. If the cluster doesn't exist yet, they crash.

**Fix:** Three-phase bootstrap in `bootstrap.sh`:
1. AWS resources only (`-target=aws_eks_cluster.perchguard` + deps)
2. CoreDNS patch + Gateway API CRDs (`-target=null_resource.*`)
3. Full apply (cluster is live, k8s provider connects cleanly)

**Time lost:** ~15 min

---

## 3. Internet gateway and route table were missing from Phase A targets

**What happened:** Phase A targeted `aws_eks_cluster.perchguard`. Terraform
resolved its direct dependencies but not the IGW and route tables, which are
dependencies of the subnets rather than the cluster. The VPC had no internet
route — CoreDNS also failed to pull images for the same reason as #1.

**Fix:** Explicitly target `aws_internet_gateway.perchguard`,
`aws_route_table.public`, `aws_route_table_association.public` in Phase A,
or run a clean `terraform apply` with no targets after the cluster is live
(Phase C catches everything).

**Time lost:** part of the ~25 min in #1

---

## 4. Gateway API with AWS LBC is not production-ready for Fargate

**What happened:** Spent ~75 min trying to make the Gateway API / GatewayClass
/ HTTPRoute path work. Every LBC version had a different failure mode:

| LBC chart | App version | Gateway API outcome |
|-----------|-------------|---------------------|
| v1.8.0 | v2.8.0 | `EnableGatewayAPI` feature gate does not exist |
| v1.11.0 | v2.11.0 | Same — `featureGates` in values.yaml does not render into args |
| v3.3.0 | v3.3.0 | Feature gate is `ALBGatewayAPI=true` via `controllerConfig.featureGates`; controller name changed from `eks.amazonaws.com/alb` to `gateway.k8s.aws/alb`; chart CRDs not installed on Helm upgrade (only on fresh install); requires `TargetGroupConfiguration` CRD for IP target mode on Fargate — no documentation |

**Fix:** Switch to NLB via `Service type: LoadBalancer`. Three annotations,
works immediately with any LBC version, battle-tested on Fargate:

```yaml
service.beta.kubernetes.io/aws-load-balancer-type: "external"
service.beta.kubernetes.io/aws-load-balancer-nlb-target-type: "ip"
service.beta.kubernetes.io/aws-load-balancer-scheme: "internet-facing"
```

**Gateway API future note:** If you want to revisit this with LBC v3.x,
you need `TargetGroupConfiguration` + `LoadBalancerConfiguration` CRDs
from `gateway.k8s.aws` configured per-route, not just the chart-level
feature gate. The CRDs must be installed separately — Helm skips CRDs on
upgrade (only installs on fresh deploy). Install manually:
```bash
kubectl apply -f <chart>/crds/gateway-crds.yaml
kubectl rollout restart deployment/aws-load-balancer-controller -n kube-system
```

**Time lost:** ~75 min — the biggest single item

---

## 5. LBC IAM policy must match the LBC app version

**What happened:** Upgraded LBC from v2.8.0 to v3.3.0. NLB provisioning
failed with:
```
AccessDenied: elasticloadbalancing:DescribeListenerAttributes
```
The IAM policy URL in `iam.tf` still pointed at the v2.8.0 policy JSON.

**Fix:** Keep the `data.http.lbc_iam_policy` URL in sync with the chart
version in `helm.tf`. Currently `v3.3.0`:
```hcl
url = "https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/v3.3.0/docs/install/iam_policy.json"
```

**Time lost:** ~5 min

---

## 6. Kubernetes Secret key names must match Helm chart defaults

**What happened:** `secrets.tf` created the secret with keys `api-key` and
`llm-api-key`. The Helm chart `values.yaml` defaults expect `key` and
`api-key`. Pod crashed with:
```
Error: couldn't find key key in Secret perchguard/perchguard-secrets
```

**Fix:** `secrets.tf` now uses `key` (management API key) and `api-key`
(LLM key) to match `values.yaml` `apiKeySecretKey` and `llmApiKeySecretKey`.

**Time lost:** ~5 min

---

## 7. Helm chart path was wrong

**What happened:** `helm.tf` referenced `${path.module}/../../helm/perchguard`
(two levels up from `deployments/eks/` = repo root). The chart lives at
`deployments/helm/perchguard` = one level up.

**Fix:** `${path.module}/../helm/perchguard`

**Time lost:** ~2 min

---

## 8. GatewayClass.spec.controllerName is immutable

**What happened:** Tried to update the `controllerName` from
`eks.amazonaws.com/alb` to `gateway.k8s.aws/alb` in-place. Kubernetes
rejected it:
```
spec.controllerName: Invalid value: "string": Value is immutable
```

**Fix:** Always `kubectl delete gatewayclass <name>` and remove from
Terraform state (`terraform state rm`) before changing `controllerName`.
Then re-apply.

**Time lost:** ~3 min

---

## 9. ConfigMap hot-reload needs all required policy fields

**What happened:** Patched the ConfigMap to flip `enforcement: enforce`
but omitted `sessionBudget.limits.maxTokensPerSession`. The policy watcher
rejected every reload:
```
policy validation error: sessionBudget.limits.maxTokensPerSession must be > 0
```
The pod silently kept the old policy.

**Fix:** Always include the full policy block when patching. If the watcher
keeps the old policy after a patch, check pod logs for validation errors.
Fastest recovery: `kubectl rollout restart deployment/perchguard -n perchguard`
(new pod mounts the ConfigMap fresh).

**Time lost:** ~5 min

---

## 10. CoreDNS cold start on Fargate takes 3–5 minutes

**What happened:** After Fargate profiles are created, CoreDNS pods sit in
`Pending` or `Pod provisioning timed out` for several minutes. The
`null_resource.coredns_fargate_patch` script times out if the 3-minute
threshold is hit before the first pod is ready.

**Fix:** The `|| true` on the first `rollout status` call prevents bootstrap
from aborting. Don't panic at the first timeout — Fargate retries every ~3
minutes. Once the NAT gateway is in place (see #1) provisioning succeeds
on the next retry. Budget 10 minutes for CoreDNS to be fully ready after
cluster creation.

**Time lost:** folded into #1/#3

---

## 11. `terraform destroy` orphans LBC-managed AWS resources if the node group dies first

**What happened (2026-09-18 teardown):** A plain `terraform destroy` got stuck three
separate times, each ~2-16 minutes, none with a useful error until the final one:

1. `aws_internet_gateway.perchguard` hung "Still destroying" for 16 minutes with **no
   error printed** — the AWS provider silently retries `DetachInternetGateway` on
   `DependencyViolation`. Root cause: two load balancers (an ALB from the Gateway API
   resource, an NLB from the `perchguard` Service) were still alive and holding ENIs in
   the public subnets. The AWS Load Balancer Controller pods run on Fargate and survive
   node group destruction, so in principle they should have cleaned these up — they
   didn't, because `aws_eks_node_group.agent_workloads` was destroyed *before*
   `kubernetes_manifest.gateway`/`helm_release.perchguard` in Terraform's graph, and
   something about that ordering left the LBC's delete reconcile for the Gateway/Service
   incomplete (finalizer never cleared, ALB/NLB never actually deleted by the controller).
2. Terraform then errored outright on `kubernetes_manifest.gateway` ("Timed out waiting
   for resource to be deleted", stuck on the `gateway.k8s.aws/alb` finalizer) and
   `helm_release.perchguard` (uninstall `--wait` timeout — the release was actually
   already gone; this one was a false alarm).
3. Re-running destroy after manually fixing the above got stuck again:
   `kubernetes_namespace.perchguard` wouldn't terminate (3 `targetgroupbindings.elbv2.k8s.aws`
   + 1 `service` with LBC-owned finalizers), then `aws_vpc.perchguard` wouldn't delete
   (3 security groups auto-created by the LBC controller for the ALB/NLB — never in
   Terraform state, so `terraform destroy` can never remove them itself).

**Fix applied each time — diagnose with AWS CLI/kubectl directly, don't just wait:**
```bash
# What's actually blocking an IGW/VPC delete:
aws elbv2 describe-load-balancers --region eu-central-1
aws ec2 describe-security-groups --region eu-central-1 --filters "Name=vpc-id,Values=<vpc-id>"

# Clear a stuck namespace: find what still has finalizers
kubectl get namespace <ns> -o json | jq '.status.conditions'
kubectl get targetgroupbindings.elbv2.k8s.aws -n <ns>
kubectl patch <resource> <name> -n <ns> --type=merge -p '{"metadata":{"finalizers":[]}}'

# Orphaned LBC security groups terraform will never touch:
aws ec2 delete-security-group --region eu-central-1 --group-id <sg-id>
```
Only strip a finalizer or delete an AWS resource by hand once you've confirmed via AWS
CLI that the underlying resource it was guarding (the ALB/NLB) is already gone — otherwise
you leak it.

**Real fix, not yet applied:** destroy the AWS Load Balancer Controller's own resources
(`helm_release.lbc`, the Gateway/Service, or at minimum give the controller a beat to
finish its reconcile) **before** `aws_eks_node_group.agent_workloads`, so it can actually
delete the ALB/NLB/security groups itself instead of leaving them orphaned for a human to
clean up. Consider a `terraform destroy -target=...` pass for the Gateway/Service/helm
release first, then a full `terraform destroy` for everything else.

**Time lost:** ~25 min across three destroy attempts, all manual AWS CLI/kubectl cleanup.

---

## Fast-path checklist for next deployment

If running `bootstrap.sh` fresh:

- [ ] `AWS_PROFILE=devsbridge` exported before running
- [ ] `.env` in repo root has `PERCHGUARD_LLM_API_KEY` and optionally `PERCHGUARD_API_KEY`
- [ ] Docker Desktop running with WSL2 integration enabled
- [ ] Bootstrap phases: ECR → image push → Phase A (AWS) → kubectl config → Phase B (CRDs) → Phase C (full)
- [ ] CoreDNS ready before proceeding: `kubectl rollout status deployment/coredns -n kube-system`
- [ ] NLB hostname: `kubectl get svc perchguard -n perchguard -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'`
- [ ] API key: `kubectl get secret perchguard-secrets -n perchguard -o jsonpath='{.data.key}' | base64 -d`
- [ ] Healthcheck: `curl http://<NLB>:8080/healthz`

Expected time from clean account: **25–35 minutes** (mostly EKS + Fargate cold start).
