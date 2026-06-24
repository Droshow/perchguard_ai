# PerchGuard Phase 6 — Open Source Polish

**Date**: 2026-05-06
**Branch**: `phase6-open-sourcing`
**Status**: In progress — 5 of 8 code capabilities shipped

| Cap | Name | Status |
|-----|------|--------|
| 2.7 | Audit-only mode (`enforcement: observe`) | ✅ Shipped — `07175b0` |
| 2.2 | Prometheus `/metrics` endpoint | ✅ Shipped — `86150e1` |
| 2.3 | Helm chart | ✅ Shipped — `78da050` |
| 2.6 | EKS Fargate production deployment | ✅ Shipped — `fe9cc10` |
| 2.4 | `perchguard watch` terminal command | ✅ Shipped — `cmd/watch.go` |
| 2.8 | Management API auth hardening | 🔧 Next — key rotation + multi-key + prod TLS warning (OIDC docs deferred) |
| 2.1 | Operator documentation | Last — write once everything is stable |
| 2.5 | Postgres backend | Stretch |

---

## 1. What Phases 1–5 Leave Unfinished

After Phase 5, PerchGuard will have:
- A complete, adversarially validated admission pipeline
- Governance snapshots with full per-step agent traces
- Management API with authentication
- Session expiry, durable audit trail, live fleet visibility
- Proof from a real red team exercise that the claims hold under pressure

What it will not have is the surface a public open source project needs: documentation a stranger can follow, a metrics endpoint an SRE expects, a deployment path that doesn't require reading the source, and a baseline UI. Phase 6 is that surface.

---

## 2. Capabilities

### 2.1 — Documentation

The current docs are written for contributors who already understand the codebase. Phase 6 produces docs for a first-time operator.

| Document | What it covers |
|----------|---------------|
| `artifacts/docs/getting-started.md` | Install → configure → run → first intercept in under 10 minutes |
| `artifacts/docs/configuration-reference.md` | Every field in `configs/policies.yaml`, every env var, with defaults and examples |
| `artifacts/docs/deployment.md` | Docker Compose (dev), k3d (local K8s), bare metal; TLS setup |
| `artifacts/docs/agent-integration.md` | How to wire an existing LLM agent to PerchGuard: `/intercept` direct, MCP proxy, manifest registration |
| `artifacts/docs/red-team-guide.md` | What was learned in Phase 5; how to run your own adversarial exercise |
| `CONTRIBUTING.md` | Dev setup, test run, PR conventions, how policies and validators are structured |

---

### 2.2 — Prometheus `/metrics` Endpoint

Every production Go service exposes `/metrics`. Without it, PerchGuard cannot integrate with a standard observability stack.

**Metrics to expose:**

```
perchguard_intercept_total{decision="ALLOW|DENY|MUTATE|HUMAN_REVIEW|TERMINATE"}
perchguard_intercept_duration_seconds (histogram)
perchguard_active_sessions
perchguard_session_risk_score (gauge, per session_id label — or histogram buckets)
perchguard_audit_ring_utilization  (count / capacity)
perchguard_policy_reload_total
perchguard_semantic_firewall_duration_seconds (histogram — LLM latency)
```

Implementation: standard `prometheus/client_golang` counter/histogram wrappers around existing audit and interceptor hooks. No new logic — instrumentation only.

---

### 2.3 — Helm Chart Upgrade

The current k3d bootstrap (`deployments/k3s/bootstrap.sh`) is a convenience script. A real Helm chart is what teams actually use to deploy.

**Chart scope:**
- `values.yaml`: addr, TLS cert refs, policy ConfigMap path, API key secret ref, LLM key secret ref, resource limits
- Liveness and readiness probes (`/healthz`)
- ConfigMap for `policies.yaml` — edit in place, policy hot-reload picks it up within 30 seconds
- PersistentVolumeClaim for the JSONL audit sink (from Phase 5)
- ServiceMonitor for Prometheus scraping (from 2.2 above)

Not in scope: Operator pattern, CRDs, multi-tenant namespacing — those are post-Phase-6.

---

### 2.4 — Minimal Governance Dashboard

The current `GET /api/fleet/summary` (Phase 5) + `GET /api/audit` give all the data needed. Phase 6 puts a minimal read-only UI on top.

**Scope — terminal dashboard (Day 1):**
- A `perchguard watch` subcommand: polls `/api/fleet/summary` and `/api/stats` on a configurable interval, renders a compact table in the terminal
- No browser dependency, no build step, ships as part of the binary

**Scope — web dashboard (stretch):**
- Single-page, no framework, served from the binary as embedded static files
- Three views: live session list with risk scores, audit log with filters, governance snapshot browser
- Read-only — no write operations from the UI

The web dashboard is a stretch goal. The terminal watch command is the minimum.

---

### 2.5 — Postgres Backend (Optional)

The in-memory `MemoryStore` and the JSONL audit sink (Phase 5) are sufficient for single-instance deployments. Multi-instance deployments need shared state.

**Scope:**
- `pkg/store/postgres.go` — implements `SessionStore` against a Postgres table
- `pkg/audit/postgressink.go` — implements `audit.Sink`, inserts governance records
- Both are opt-in via config (`PERCHGUARD_STORE=postgres`, `PERCHGUARD_STORE_DSN=...`)
- In-memory remains the default; no dependency introduced unless opted in

This is explicitly optional. A single-instance deployment with the JSONL sink covers most enterprise use cases at Phase 6 maturity.

---

### 2.6 — EKS Fargate Production Deployment

PerchGuard's design is directly inspired by the Kubernetes admission control pattern — it belongs in the Kubernetes ecosystem as a first-class citizen. The k3d path covers local dev; this capability covers a real production deployment.

**Platform choice: EKS Fargate**
Fargate removes node management entirely. No EC2 fleet to patch, no node groups to scale — pods run serverless. The right level of "production without too much ceremony".

**Scope:**

| Item | What |
|------|------|
| `deployments/eks/` | New directory; all EKS-specific manifests |
| `deployments/eks/cluster.yaml` | `eksctl` cluster spec: Fargate profile scoped to `perchguard` namespace, no managed node groups |
| `deployments/eks/bootstrap.sh` | One-shot: create cluster → install ALB controller → deploy Helm chart → verify |
| Helm chart extension (from 2.3) | Add `aws.enabled`, `fargate.enabled` value switches; EFS StorageClass for the JSONL audit sink PVC (Fargate cannot use EBS) |
| Secrets | K8s `Secret` objects; `bootstrap.sh` creates them from env/AWS Secrets Manager via `aws secretsmanager get-secret-value` — no External Secrets Operator dependency |
| Ingress | AWS Load Balancer Controller + `IngressClass: alb`; `/intercept` and `/api/*` on separate listener rules; TLS termination at ALB with ACM cert |
| HPA | `HorizontalPodAutoscaler` on CPU + custom metric `perchguard_intercept_total` (requires Prometheus Adapter, optional); min 2 replicas, max 10 |
| IAM | IRSA (IAM Role for Service Account) scoped to read from Secrets Manager and write to CloudWatch Logs if no in-cluster Prometheus |

**What it does not include:**
- Multi-region / cross-account federation
- Service mesh (Istio / Linkerd)
- EKS Blueprints or Terraform — `eksctl` is enough for Phase 6
- RDS/Aurora for the Postgres backend (that's 2.5, independently optional)

**Why Fargate over managed node groups:**
No AMI management, no capacity planning for the admission layer itself. PerchGuard is a latency-sensitive sidecar concern — Fargate's per-pod isolation also means a compromised agent cannot escape to a shared node.

**Deployment doc update (2.1):**
`docs/deployment.md` gains a third section: "Production — EKS Fargate" alongside Docker Compose (dev) and k3d (local K8s).

---

### 2.7 — Audit-Only Mode (`enforcement: observe`)

**This is a GTM prerequisite, not a stretch goal.** No team approves a new admission controller in their critical path on day one. Audit-only mode removes that barrier entirely.

**How it works:**
A single top-level policy flag:

```yaml
# configs/policies.yaml
enforcement: observe   # observe | enforce (default on public release: observe)
```

In `observe` mode the full pipeline runs — quota checks, all validators, fleet risk accumulation, JSONL audit records, governance snapshots — but every decision is overridden to `ALLOW` before the response is returned. The agent sees no blocked calls, no latency surprises, no operational impact.

What the operator gets: a complete governance record of what *would* have happened. After a week of observe mode they can read `audit.jsonl` and answer "how many calls would have been blocked, by which policy, for which agent?" That is immediately valuable. Zero risk to say yes to.

**Default on public GitHub release: `enforcement: observe`**

When the repo goes public, the shipped `configs/policies.yaml` defaults to `observe`. New adopters get governance visibility with no operational risk. The path to enforcement is:
1. Run in `observe` for 1–2 weeks — read the audit log, understand the decisions
2. Flip to `enforce` in dev/staging — first real enforcement, bugs surface here safely
3. Promote to production after a clean staging run

This mirrors exactly how K8s admission webhooks are validated: `dryRun: true` before live enforcement. The pattern is already familiar to the target audience.

**Implementation scope:**
- `configs/policies.yaml` — add `enforcement: observe | enforce` top-level field; default `observe`
- `pkg/policy/loader.go` — expose `Config.Enforcement` string
- `pkg/admission/interceptor.go` — after full pipeline runs in observe mode, replace any non-ALLOW decision with `ALLOW` and add `observe_mode: true` to the audit record before writing; the original decision is preserved in `observed_decision` field so the audit trail is truthful
- `GET /api/pipeline` — surface current enforcement mode in the response
- Startup log: `[perchguard] enforcement mode: OBSERVE — decisions logged but not enforced`

**What it does not do:**
- Skip the pipeline (the pipeline always runs — observe mode is about the response, not the evaluation)
- Hide decisions in the audit log (every `DENY`/`HUMAN_REVIEW`/`TERMINATE` that would have fired is recorded with `observed_decision` — the audit trail is complete)

---

### 2.8 — Management API Auth Hardening (Baseline)

The current management API uses a static bearer token (`pgmk-...` auto-generated at startup). That is acceptable for a single-operator deployment but below the bar for an open source project that will be deployed by teams.

Phase 6 adds a baseline that meets open source standards without requiring a full identity provider:

| Item | What |
|------|------|
| Item | Plan | Status |
|------|------|--------|
| Single static bearer token | `pgmk-...` auto-generated, logged once, `crypto/subtle.ConstantTimeCompare` | ✅ Already implemented (`pkg/api/middleware.go`) |
| API key rotation | `POST /api/keys/rotate` — generates new `pgmk-` key, invalidates old; returned once, never stored in plaintext | ❌ Not built |
| Multiple named keys | `GET /api/keys` lists labels + creation time, never key material; N active keys per team/service | ❌ Not built |
| Production TLS warning | Startup warns loudly when `PERCHGUARD_ENV=production` and TLS is off | ❌ Not built |
| OIDC documentation | `artifacts/docs/security.md` — OIDC reverse proxy in front of `/api/*`; PerchGuard does not implement OIDC natively | Deferred to docs pass |

**What this is not:** full OAuth/OIDC flow, mTLS, PKI-issued agent identity — those remain post-Phase-6 enterprise hardening. What it is: a baseline that a security-conscious open source adopter will not reject outright.

**Implementation scope remaining:** `POST /api/keys/rotate`, `GET /api/keys`, `KeyStore` in `pkg/api/`, production TLS warning in `cmd/main.go`.

---

## 3. What Phase 6 Does Not Include

- Full OAuth / OIDC native implementation on the management API — post-Phase-6 (Phase 6 covers baseline key rotation + TLS enforcement + OIDC proxy docs)
- Multi-tenancy — separate policy namespaces per team or org
- Agent identity via PKI
- SaaS / cloud-hosted control plane (Regentics.ai platform layer — separate product track)
- Billing, usage metering

---

## 4. Capability Summary

| # | Capability | Blocks open source? |
|---|-----------|-------------------|
| 1 | First-time operator documentation | Yes — without it, adoption is zero |
| 2 | Prometheus `/metrics` | Yes — non-negotiable for SRE teams |
| 3 | Helm chart upgrade | No — Docker Compose works; Helm is expected but not blocking |
| 4 | Terminal watch command | No — `watch curl /api/fleet/summary | jq` works today |
| 5 | Web dashboard | No — stretch goal |
| 6 | Postgres backend | No — optional, JSONL + in-memory covers single-instance |
| 7 | Audit-only mode (`enforcement: observe`, default on public release) | **Yes — GTM prerequisite; no team adopts an admission controller without a passive mode first** |
| 8 | EKS Fargate production deployment | No — but required for enterprise credibility; PerchGuard's DNA is K8s |
| 9 | Management API auth hardening (key rotation + TLS enforcement + OIDC proxy docs) | No — but below open source bar without it |

Items 1, 2, and 7 are the real gates. Documentation and metrics are what determine whether a project gets discovered; audit-only mode is what determines whether anyone actually tries it. Documentation and metrics are what determine whether a project gets adopted or ignored. Item 7 is the enterprise credibility gate — a governance controller that cannot deploy to the platform it was designed to complement is a harder sell.

---

## 5. Sequence

Phase 6 begins after the Phase 5 red team exercise concludes and findings are incorporated. The red team will surface documentation gaps (things an operator needed to know but couldn't find) and missing metrics (things the defender wanted to see but couldn't). Those findings directly shape the Phase 6 scope — particularly the getting-started guide and the metrics selection.

**Build order (actual):**
1. ✅ **Audit-only mode** — `enforcement: observe` default, full pipeline, audit trail complete (`07175b0`)
2. ✅ **Prometheus `/metrics`** — 7 metrics, `TeeAuditLogger` hook, `/metrics` unauthenticated (`86150e1`)
3. ✅ **Helm chart** — production-quality, 9 templates, ServiceMonitor, HPA, EFS/IRSA stubs (`78da050`)
4. ✅ **EKS Fargate deployment** — Terraform, Gateway API + ALB, single `apply`/`destroy` (`fe9cc10`)
5. ✅ **`perchguard watch` terminal command** — `cmd/watch.go`, polls `/api/fleet/summary` + `/api/stats`, ANSI terminal dashboard
6. 🔧 **Management API auth hardening** — key rotation + multi-key + prod TLS warning (next)
7. **Operator documentation** — last; written once everything is stable and repo is about to go public
8. CONTRIBUTING.md + deployment guide (community readiness)
9. Web dashboard + Postgres backend (stretch, time-permitting)
