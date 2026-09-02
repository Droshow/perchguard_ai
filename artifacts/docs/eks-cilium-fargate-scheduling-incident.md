# EKS Cilium/Fargate Scheduling Incident

Phase 10c live deploy, 2026-09-02, account `961477247679` / `eu-central-1`.
`bootstrap.sh` needed 7 iterations end-to-end. This records the real root
causes, in the order hit, so the next Cilium-on-EKS-with-Fargate deploy
doesn't rediscover them.

---

## 1. `cilium-operator` stuck Pending — Fargate's webhook, not tolerations

**Root cause:** `cilium-operator` is a Deployment. The `kube-system` Fargate
profile (`main.tf`) used a blanket namespace selector with no label filter,
so EKS's Fargate mutating webhook force-set `schedulerName: fargate-scheduler`
on every pod created in `kube-system` — including `cilium-operator`.
Pod-level tolerations and `nodeAffinity` cannot override a scheduler
assignment made by that webhook; they only matter to whichever scheduler
actually receives the pod. Fargate then rejected it outright:
`Pod not supported on Fargate: fields not supported: HostNetwork`.

**Why the agent DaemonSet worked and the operator didn't:** Fargate
structurally excludes DaemonSet-owned pods from webhook mutation — that's
why `cilium-p6dwg` (the agent) scheduled fine on the real EC2 node while
`cilium-operator` (a Deployment) sat Pending forever.

**Fix that didn't work:** adding `operator.tolerations` / `operator.affinity`
Helm values pointing at the agent node group. Necessary, but not sufficient
— the webhook still intercepts first.

**Actual fix:** move the whole Cilium release out of `kube-system` into its
own namespace (`cilium-system`, `create_namespace = true`) that has no
Fargate profile at all. No Fargate profile selecting a namespace means the
webhook never touches pods created there — tolerations/affinity then do
their normal job against the default scheduler. `operator.replicas = 1` also
needed: the chart's default HA pod anti-affinity can't be satisfied by a
single-node agent group (lab budget, see `variables.tf`
`agent_node_desired_size`).

## 2. Same bug again, different sub-chart values: `cilium-envoy`

**What happened:** After fixing the operator, `helm_release.cilium` still
timed out (`context deadline exceeded`) — Helm's `wait` never succeeded even
though cilium-agent, cilium-operator, and the one real cilium-envoy pod were
all `Running`. `kubectl get daemonset -n cilium-system` showed
`cilium-envoy: 8 desired, 1 ready` — it was targeting all 8 nodes in the
cluster, including the 7 Fargate virtual nodes it can never run on, and
`helm_release`'s wait condition blocks on `desiredNumberScheduled ==
numberReady` for every DaemonSet in the release.

**Root cause:** the top-level `affinity`/`tolerations` Helm values only
scope the main `cilium-agent` DaemonSet. `cilium-envoy` (a separate
DaemonSet Cilium 1.16+ installs by default, unrelated to Hubble) has its own
`envoy.affinity` / `envoy.tolerations` values path — exactly the same shape
of bug as `operator.*` in §1, just a different component.

**Fix:** scope `envoy.affinity.nodeAffinity...` and `envoy.tolerations[0]`
identically to the operator ones.

**Guardrail:** in this chart, any Cilium sub-component beyond the main agent
(`operator`, `envoy`, and presumably `hubble`/`hubble-relay` if ever
enabled) needs its *own* affinity/tolerations values. Don't assume the
top-level `affinity`/`tolerations` keys are inherited — check each
component's values path explicitly, and verify with
`kubectl get daemonset/deployment -n <ns>` (desired vs. ready counts) rather
than trusting `helm install` to report accurately.

## 3. Gateway ALB silently internal, not internet-facing

**What happened:** After the whole stack came up, `PerchGuard is live` printed
an `internal-*` ALB hostname — unreachable from outside the VPC — despite
`gateway.tf` setting `alb.ingress.kubernetes.io/scheme: internet-facing` on
the `Gateway` resource. No error, no warning; the annotation was just
silently ignored.

**Root cause:** those `alb.ingress.kubernetes.io/*` annotations are the
legacy AWS Load Balancer Controller *Ingress* API. The LBC's Gateway API
integration reads a completely different mechanism: a
`LoadBalancerConfiguration` CRD (`gateway.k8s.aws/v1beta1`), attached via
`parametersRef` on the `GatewayClass` (or the `Gateway` itself, on chart
versions where `Gateway.spec.infrastructure.parametersRef` exists — not the
Gateway API v1.1.0 CRD bundle installed here). With no
`LoadBalancerConfiguration`, the ALB scheme defaults to internal.

**Fix:** added `kubernetes_manifest.gateway_lb_config`
(`LoadBalancerConfiguration`, `spec.scheme = internet-facing`) and wired it
into `gatewayclass.spec.parametersRef`. Dropped the now-dead
`alb.ingress.kubernetes.io/*` annotations from the `Gateway` resource so
nothing implies they still do something.

**Recovery note:** ALB `scheme` is immutable on an existing load balancer —
LBC responded to the new config by provisioning a *new* ALB
(`TargetGroupAssociationLimit` errors are expected and transient while the
old and new ALB both briefly reference the same target group) and tearing
down the old internal one once the new one went `active`. This took a few
reconcile cycles (~3 min); no manual AWS Console intervention was needed or
should be attempted while it's converging.

---

## Guardrails for next time

- **Any namespace with a Fargate profile is Fargate-owned for scheduling
  purposes**, full stop — a pod's own tolerations/affinity cannot opt it out
  once the webhook has claimed it. If a workload needs real EC2 nodes
  (`hostNetwork`, DaemonSet-adjacent behavior, privileged, etc.), it needs a
  namespace with *no* Fargate profile selecting it, not just
  tolerations/affinity tuning.
- **A Helm chart's top-level values rarely cover every sub-component.**
  Before trusting `affinity`/`tolerations` (or similar) values to scope an
  entire release, check whether each Deployment/DaemonSet the chart installs
  has its own values sub-path. Verify post-install with
  `kubectl get daemonset/deployment -n <ns>` — desired vs. ready — not just
  `helm status`.
- **Gateway API on AWS LBC uses different config surfaces than Ingress.**
  `alb.ingress.kubernetes.io/*` annotations are Ingress-only and fail
  silently (no rejection, no warning) when put on a `Gateway`. Use
  `LoadBalancerConfiguration` referenced via `GatewayClass.spec.parametersRef`
  instead.
- **`helm_release`'s default `wait` blocks on DaemonSet desired-vs-ready
  across every node the DaemonSet's selector matches**, including nodes it
  can structurally never run on. A stuck-forever wait is a signal to check
  `desiredNumberScheduled` before assuming the workload itself is broken.
