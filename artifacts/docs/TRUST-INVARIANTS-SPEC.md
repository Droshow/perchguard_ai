# PerchGuard Trust Invariants Specification

**Status:** Draft  
**Scope:** Defines the unconditional guarantees PerchGuard makes regardless of operator
configuration, the per-agent constraints an agent can declare in its manifest, and the
contract versioning model that makes custom invariants auditable and SLA-safe.

---

## 1. Problem

Every PerchGuard policy is currently operator-configurable, which means a misconfigured
or compromised deployment can silently reduce governance to zero. The product is positioned
as a *trust anchor* — that claim only holds if there is a non-negotiable enforcement floor
that no configuration can remove, and if any change to the governance contract is itself
a governed, auditable event.

This spec defines that floor, the per-agent constraint surface, and the contract versioning
model that ties them together for paid/SLA deployments.

---

## 2. Two-layer model

```
┌─────────────────────────────────────────────────────────┐
│  MANIFEST INVARIANTS  (per-agent, declared at register) │
│  Agent asserts what it will never do.                   │
│  Enforced regardless of global policy state.            │
├─────────────────────────────────────────────────────────┤
│  GLOBAL POLICY  (operator-configured, hot-reloadable)   │
│  Operator configures what agents in this deployment     │
│  are allowed to do. Lives in policies.yaml.             │
├─────────────────────────────────────────────────────────┤
│  GLOBAL INVARIANTS  (unconditional floor)               │
│  PerchGuard guarantees these regardless of policy.      │
│  No configuration key disables them.                    │
└─────────────────────────────────────────────────────────┘
```

The effective permission for any tool call is the intersection of all three layers.
A DENY from any layer cannot be overridden by a more permissive layer above it.

---

## 3. Global invariants (unconditional)

These hold in every deployment. There is no configuration key that disables them.

| # | Invariant | Rationale |
|---|-----------|-----------|
| G1 | Every tool call decision is written to the audit log | Without a complete decision record the governance claim is unverifiable |
| G2 | A session cannot register itself as ungoverned or bypass the admission pipeline | PerchGuard is the trust anchor; no agent can opt out of governance |
| G3 | A child agent's effective tool scope must be a subset of its parent's | Delegation cannot escalate privilege |
| G4 | Delegation depth is always finite (minimum floor: 1) | Prevents unbounded agent recursion regardless of depthLimiter config |
| G5 | A TERMINATE decision ends the session immediately and cannot be retried by the same session | Prevents session resurrection after a hard safety breach |
| G6 | Manifest `out_of_scope` declarations are hard denies for the lifetime of the session | An agent's own declared out-of-scope is a stronger signal than any policy permitting the tool |
| G7 | Manifest `invariants` are enforced even if the matching global policy validator is disabled | An agent's self-declared safety properties survive policy changes |
| G8 | The audit log is append-only during a session; no in-flight record can be modified or deleted | Audit integrity must survive a compromised admission pipeline |

---

## 4. Manifest invariants

Declared by the agent at `POST /agents/register` in the `AgentManifest` struct.
These are per-agent, session-scoped commitments.

### 4.1 `invariants []string`

Currently parsed, not enforced. **This is the gap.**

Each string is a named invariant from the deployment's active invariant contract
(see section 6). The pipeline enforces it unconditionally for this session regardless
of global policy state.

**Built-in names (always available, no contract required):**

| Name | Meaning |
|------|---------|
| `no-file-delete` | DENY any tool call that deletes or truncates a file |
| `no-network-egress` | DENY any tool call that opens an outbound connection |
| `no-shell-exec` | DENY any tool call that executes a shell command |
| `no-credential-access` | DENY access to credential-pattern paths (`.env`, `*.pem`, `*_rsa`, etc.) |
| `human-review-destructive` | Require human approval for any tool call classified as destructive |
| `read-only` | Alias for `no-file-delete` + `no-shell-exec` + `no-network-egress` |

Operator-defined names (from the invariant contract) are also valid once the contract
is approved. Unknown names are rejected at registration — fail closed.

### 4.2 `authorization.human_review_required_for []string`

Tool name patterns that require human review for this specific agent, regardless of
whether `humanReview.enabled` is true in global policy. Agent-declared, not
operator-granted.

**Current gap:** parsed and stored, not wired into the admission pipeline.

### 4.3 `mission.out_of_scope []string`

Currently advisory context for the semantic firewall.

**Proposed change:** evaluated as a hard deny-list before any other validator.
One string/glob match pass per call. G6 is then structural, not probabilistic.

---

## 5. What operators can and cannot configure

### Can be disabled / tuned

- SemanticFirewall (requires LLM API key; fail-open by design)
- HumanReview webhook URL and timeout
- DataExfiltration destination allow/block lists
- ParameterSanitization rules
- LeastPrivilege path constraints
- SessionBudget limits (but not the existence of budget enforcement)
- AgentFleet drift and behavior thresholds
- Audit log level and export destinations (but not audit itself — G1)

### Cannot be disabled

- Audit write on every decision (G1)
- Admission pipeline traversal — no bypass header, no skip flag (G2)
- Delegation scope containment check (G3)
- Depth floor of 1 (G4)
- TERMINATE finality (G5)
- Manifest `out_of_scope` deny evaluation (G6)
- Manifest `invariants` enforcement (G7)
- Audit append-only guarantee (G8)

---

## 6. Invariant contract versioning

### 6.1 Motivation

Operator-defined (custom) invariants are the right design — they cover domain-specific
business rules that no fixed vocabulary anticipates. But a custom invariant is only as
strong as its definition, and a definition that changes silently undermines the SLA.

The solution is to treat the invariant contract itself as a governed artifact:
every definition is versioned, every change requires explicit operator approval,
and the audit log records every contract transition.

For open-source deployments this is optional and unenforced — operators own the risk.
For paid/SLA deployments the contract snapshot is the deliverable that goes into the
SLA annex.

### 6.2 Contract structure

The invariant contract is a separate file from `policies.yaml`, co-located at
`configs/invariant-contract.yaml` (or mounted as a separate ConfigMap in K8s).

```yaml
# configs/invariant-contract.yaml
apiVersion: perchguard/v1
kind: InvariantContract
metadata:
  version: "3"
  approvedAt: "2026-04-01T09:15:00Z"
  approvedBy: "ops-lead@example.com"
  hash: "sha256:abc123..."       # SHA-256 of the definitions block below
  previousHash: "sha256:def456..." # hash of the prior approved version

definitions:
  - name: "no-prod-db-write"
    description: "Prevent any write operation against production database tools"
    denyIfToolMatches: "db:*"
    denyIfParamContains: ["INSERT", "UPDATE", "DELETE", "DROP", "TRUNCATE"]

  - name: "no-external-api-call"
    description: "Block calls to any tool that reaches external APIs"
    denyIfToolMatches: "http:*"
    denyIfParamMatches:
      url: "^https?://(?!.*\\.internal)"

  - name: "audit-only-file-ops"
    description: "Allow file operations but always flag for human review"
    humanReviewIfToolMatches: "write_file|delete_file"
```

### 6.3 Approval flow

A contract change does not take effect on file write alone. It requires an explicit
approval signal before PerchGuard loads the new definitions:

```
1. Operator edits configs/invariant-contract.yaml
2. Operator calls: POST /api/contract/approve
   Body: { "hash": "sha256:abc123...", "approvedBy": "ops-lead@example.com" }
3. PerchGuard verifies the hash matches the file on disk
4. PerchGuard atomically swaps the active contract
5. Audit log records: CONTRACT_APPROVED { version, hash, previousHash, approvedBy, at }
6. Snapshot written to snapshots/invariant-contract-v3.json (immutable, append-only)
```

If the file changes but no approval call is made, PerchGuard continues enforcing
the last approved contract and logs a `CONTRACT_DRIFT_DETECTED` warning.

### 6.4 Audit events

| Event | When emitted |
|-------|-------------|
| `CONTRACT_APPROVED` | New contract version takes effect |
| `CONTRACT_DRIFT_DETECTED` | File hash differs from last approved hash at startup or hot-reload |
| `CONTRACT_INVARIANT_VIOLATED` | A tool call was denied by a contract-defined invariant |
| `CONTRACT_UNKNOWN_INVARIANT` | An agent manifest declared an invariant name not in the active contract |

### 6.5 Snapshot artifact

On every approval, PerchGuard writes an immutable snapshot:

```
snapshots/
  invariant-contract-v1.json
  invariant-contract-v2.json
  invariant-contract-v3.json   ← current
```

Each snapshot is the complete contract at that version: definitions, hash, approver,
timestamp, and the SHA-256 of the previous snapshot (chain of custody).

The current snapshot hash is included in every `AuditRecord` so any decision can be
traced back to the exact contract version that governed it.

### 6.6 SLA use

The invariant contract snapshot is the artifact that goes into the SLA annex:

> *"As of 2026-04-01, contract version 3, hash sha256:abc123, the following invariants
> were in effect for all governed sessions in this deployment. Any change to this contract
> requires explicit operator approval and is recorded in the audit log."*

This means the SLA commitment is not "PerchGuard guarantees X forever" — it is
"PerchGuard guarantees that any change to X is traceable, approved, and auditable."
That is a stronger and more honest commitment.

### 6.7 Open-source behaviour

When `contract.enforcement` is set to `advisory` (default in open-source builds):

- Contract drift is logged but does not block hot-reload
- Approval API exists but is a no-op (always returns 200)
- Snapshots are still written for operators who want the audit trail

When set to `strict` (default in paid builds):

- Unapproved contract changes are ignored; last approved version stays active
- `CONTRACT_DRIFT_DETECTED` is emitted and surfaced on `GET /api/pipeline`

---

## 7. values.yaml — missing manifest and contract policy blocks

```yaml
manifest:
  # Reject registrations that declare unknown invariant names (fail closed).
  strictInvariantValidation: true
  # Enforce out_of_scope as a hard deny before other validators.
  outOfScopeIsDeny: true
  # Honour agent-declared human_review_required_for even without global webhook.
  honorManifestReviewRequirements: true
  # Maximum invariants an agent may declare (guards against degenerate manifests).
  maxInvariants: 20

contract:
  # Path to the invariant contract file.
  path: "./configs/invariant-contract.yaml"
  # advisory: drift logged, hot-reload proceeds. strict: unapproved changes ignored.
  enforcement: "advisory"
  # Directory for immutable contract version snapshots.
  snapshotDir: "./snapshots"
```

---

## 8. Enforcement gaps (current state → target)

| Gap | Current state | Target |
|-----|--------------|--------|
| `invariants []string` | Parsed, ignored | `InvariantValidator` — first in pipeline, checks built-in + contract names |
| `out_of_scope` | Advisory to semantic firewall | Hard deny-list pre-validator pass |
| `human_review_required_for` | Stored, not wired | Emits `HUMAN_REVIEW` regardless of global policy |
| `projectContext` ADR/PRD refs | Stored, unused | Feed into semantic firewall context window |
| Audit disable prevention | Configurable | Audit config controls destination/level only |
| Delegation scope per-call | Checked at registration only | Re-checked per call for dynamic escalation |
| Invariant contract versioning | Does not exist | `InvariantContract` type, approval API, snapshot chain |

---

## 9. Implementation priority

1. `InvariantValidator` — new validator, first in pipeline, enforces manifest `invariants`
2. `out_of_scope` hard deny — pre-validator pass in the interceptor
3. `human_review_required_for` wiring — check manifest on every call
4. `InvariantContract` type + loader — parse and validate `invariant-contract.yaml`
5. `POST /api/contract/approve` — approval API, atomic swap, audit emission
6. Snapshot writer — immutable per-version JSON files with hash chain
7. values.yaml manifest + contract blocks
8. Audit unconditional write — separate destination config from enable/disable
