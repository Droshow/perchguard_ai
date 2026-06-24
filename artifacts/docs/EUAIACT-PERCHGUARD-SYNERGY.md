# EU AI Act ↔ PerchGuard Synergy — An Article 26 Deep Dive

**Companion to**: `EUAIACT-COMPLIANCE-ARTICLE-MAP.md` (article-by-article catalog, Art. 26
table) and `PHASE8-AGENTIC-COMPLIANCE.md` (product thesis, cap 8.2 scoping).

**Purpose**: the article map already gives Art. 26 a full table — 11 sub-obligations,
each with a status (`configured` / `evidenced` / `outside_layer` / `not_applicable`). This
document doesn't repeat that table. It picks at the one row that doesn't have a clean
classical answer — Art. 26(3), "input data quality" — and asks what that obligation
*means* for a system that has no fixed input dataset, only a looping sequence of
tool calls. Along the way it traces the code that already exists for this, finds the gap
in it, and does the same exercise for PII/biometric data, which the user correctly
flagged as the area with the most room to do "more."

Same non-negotiable framing as the article map: **PerchGuard produces evidence that maps
onto a regime's requirements — it does not certify compliance.** Everything below is
framed as "what evidence could exist" and "what's missing to produce it," not "what makes
you compliant."

---

## 1. The reframe: what is "input data" for a looping agent?

Art. 26(3) reads, paraphrased: *the deployer shall, to the extent it controls the input
data, ensure that input data is relevant and sufficiently representative in view of the
intended purpose of the high-risk AI system.*

The classical reading assumes a roughly static model fed a dataset — a credit-scoring
model scoring a batch of applicant records, say. "Input data quality" there is a
*dataset-curation* question: is this batch of records representative of the population
the model was validated on?

An agentic system has no such dataset. Step 5's "input" is step 4's tool output, shaped by
the agent's own plan, which the agent itself is continuously revising. There is nothing
to curate in advance. The only thing a deployer *can* control, continuously, is whether
each step's actual input still belongs to the task the agent was declared to perform.

That's not a data-curation question — it's a **mission-fidelity** question. Read that
way, Art. 26(3) for an agentic deployer becomes: *"is the work the agent is doing right
now still the work it was declared to do, and can I show that for every step?"* That is
exactly the "work snapshot vs. declared goal, tracked through the loop" framing in the
user's original question — and it turns out PerchGuard already has a working,
code-level answer to a version of this question. The rest of this section traces it.

---

## 2. What already exists: registration-time intent baseline + per-call drift

Three pieces, already wired together:

**a) The declared mission** — `pkg/manifest/manifest.go`. Every registered agent submits
an `AgentManifest` with `Mission.Summary`, `Mission.Scope`, `Mission.OutOfScope`.
`AgentManifest.IntentText()` (`pkg/manifest/manifest.go:99-103`) concatenates
`Summary` + `Scope` into one string — this is the agent's *self-declared* work snapshot
at t=0.

**b) Project context enrichment** — `pkg/audit/context.go` + `pkg/localfs/reader.go`.
When `PERCHGUARD_CONTEXT_ROOT` is set, `Reader.ReadContext(limit)`
(`pkg/localfs/reader.go:63-76`) loads:
  - the first 800 characters of the most recently authored PRD under `context/prds/`
  - the `Summary` field of the last `limit` entries in `context/snapshots/entries.json`
    (the same governance-snapshot file shown in `snapshots/governance.json`)

`Context.IntentText()` (`pkg/audit/context.go:28-42`) concatenates these into a second
string — call it the *project's* work snapshot, as opposed to the agent's self-declared
one.

**c) Registration-time fusion** — `pkg/api/agents.go:82-91`. On `POST /agents/register`:

```go
intentText := m.IntentText()                 // (a) agent's declared mission
ctx := s.contextProvider.ReadContext(3)      // (b) PRD + last 3 governance snapshots
if ctx.Loaded {
    if extra := ctx.IntentText(); extra != "" {
        intentText = strings.TrimSpace(intentText + " " + extra)
    }
}
s.fleet.SeedFromManifest(sessionID, intentText, ...)
```

`SeedFromManifest` → `pkg/agent/fleet.go:103` → `IntentModel.SetBaseline(intentText)`
(`pkg/agent/intent.go:18-21`). From that point, every tool call is scored via
`DriftScore()` (`pkg/agent/intent.go:28-35`, word-frequency cosine similarity) and drift
above `cfg.DriftThreshold` feeds `RiskAccumulator` (`pkg/agent/session.go:60-65`).

**This is, concretely, an Art. 26(3) control for an agentic system.** It is not "is this
training dataset representative" — it's "is each step's input still consistent with the
fused agent-manifest-plus-project-context baseline captured at registration." For the
article map's Art. 26(3) row, the natural **Report** deliverable this paper proposes is
a **drift timeline**: baseline text (manifest + PRD excerpt + recent snapshots, as
actually concatenated) alongside each session's per-call `DriftScore` and any threshold
crossings. That timeline *is* the "input data quality" evidence — arguably a more
faithful rendering of Art. 26(3)'s intent for an agentic deployer than a literal
dataset-representativeness check would be, and it's already 90% code-complete.

---

## 3. The gap: one snapshot, taken once, never revisited

`SetBaseline()` is called exactly once — at registration
(`pkg/agent/fleet.go:103`, reached via `pkg/api/agents.go:90`). `IntentModel` has no
method to update `baseline` afterward; `pkg/agent/session.go:49` only calls
`SetBaseline` `if !a.intent.HasBaseline()`, i.e., the first time, ever.

For short sessions this is fine. For the kind of long-running agentic loop that Art. 9(2)
("continuous, iterative... throughout the lifecycle") and Art. 72 ("actively and
systematically" post-market monitoring) anticipate, a single t=0 snapshot — manifest
mission + 800 chars of whatever PRD existed *at registration* + the last 3 governance
snapshots *at registration* — goes stale as the agent legitimately progresses into later
phases of its own plan that weren't represented in that initial snapshot. The failure
mode becomes **false-positive drift on normal task progression**, not just true-positive
drift on exfiltration-style misuse. A risk-scoring signal that fires on normal behavior
is worse than useless for Art. 14 human-oversight purposes — it trains reviewers to
ignore it.

Notably, the manifest already has a field shaped for exactly this:
`AgentManifest.ProjectContext` (`pkg/manifest/manifest.go:51-56`,
`ProjectContextRef{ProjectID, PRDVersion, ADRRefs}`). Grep confirms it is parsed but
**never read** anywhere outside the struct definition — only `Mission` and
`ParentSessionID` are consumed during registration. The field exists, presumably, as a
placeholder for exactly the "what work snapshot are we currently on" tracking this
section is describing — it just isn't connected to anything yet.

Two ways to close this gap — presented as options, neither implemented, both small and
additive to `pkg/agent/intent.go`:

- **(a) Re-anchor on manifest re-registration.** If a long-running agent re-registers
  (same `Metadata.ID`, bumped `Metadata.Version` / `ProjectContext.PRDVersion`)
  mid-task, treat that as a new declared work snapshot. `IntentModel` gains a method to
  push a new baseline, and the *sequence* of baselines — each timestamped, each tied to
  a manifest version — becomes the literal "work snapshot history" the user described.
  This reuses the existing `context/snapshots/entries.json` mechanism as the storage
  substrate; the only new code is in `IntentModel` and the registration handler's
  re-registration path.

- **(b) Multi-snapshot baseline.** Keep the last *N* manifest/context-derived baseline
  vectors as a small reference set; score `DriftScore` against the *nearest* one rather
  than a single fused vector. Cheaper than (a) — no re-registration event required —
  and avoids the single-stale-snapshot problem by letting the project's own snapshot
  history (already being written by `localfs.Writer.WriteGovernanceSnapshot`,
  `pkg/localfs/writer.go:29`) double as the reference set.

Either direction is a refinement of an existing, working primitive — not a new
subsystem. The audit substrate (`context/snapshots/entries.json`, `governance.json`)
already exists; the gap is purely in how `IntentModel` consumes it (once, vs.
continuously).

---

## 4. Oversight, transparency, regulatory cooperation — confirming the "good track"

Your read on these matches what the article map already documents in detail (Art. 26
rows 2/4/5/6/10/11, and the Art. 14/50/73/21 entries). Briefly, for completeness:

- **Oversight (26(2)/(4))** — `REVIEW`/`TERMINATE` decisions, `dualApprovalRoles`,
  `pkg/humanreview/webhook.go` Dispatcher, `/api/review/{id}` — this *is* Art. 14,
  almost verbatim, and is the strongest mapping in the whole Act. No gap worth adding
  here beyond what the article map already covers.

- **Transparency / workplace disclosure (26(6)/(10))** — correctly mostly
  Knowledge/Workflow, not Code Automation (HR/communications process). One observation:
  Art. 26(2)'s "natural persons with the necessary competence, training, authority"
  has a documentation gap, not a code gap — `dualApprovalRoles` declares *who* can
  approve, but nothing declares *what competence that role is assumed to have*. The
  Art. 4 AI-literacy checklist (article map, Art. 4 entry) already covers this; it's a
  process item, not a missing capability.

- **Regulatory cooperation (26(11))** — `/api/audit` export, same as Art. 21. No new
  ground; the article map's treatment is complete.

These three categories don't need a deeper paper — they're already evidenced by shipped
primitives. The interesting gaps are in §3 (above) and §5 (below).

---

## 5. PII and biometric data — the concrete "do more" lever

This is where your instinct is sharpest, and for a **biometrics-company deployment
specifically**, it's the single highest-leverage gap in the current build. Biometric
data is GDPR Art. 9 "special category" data, an explicitly named EU AI Act Annex III
point 1 high-risk category, and a trigger for several Art. 5 prohibited-practice
categories (biometric categorization inferring protected attributes). Getting this right
isn't just "more PII coverage" — it's the difference between Art. 26(3)/Art. 10 evidence
existing at all for this vertical, and an Art. 5 control that can actually fire.

**What exists today is shape-based, not content-based:**

- `policies.audit.redactedFields` (`configs/profiles/open-banking.yaml:453-458`) —
  matches by *parameter name*: `password`, `secret`, `api_key`, `token`, `authorization`.
  A biometric template passed as `face_embedding`, `voiceprint`, or `iris_template`
  would not match any of these — the redaction is blind to it.

- `policies.audit.auditRedactPatterns` (`configs/profiles/open-banking.yaml:364-365`) —
  regex on *audit log text*, scoped to financial-identifier shapes (IBAN-pattern) in the
  open-banking profile. Two limits: it's vertical-specific (no biometric-shaped patterns
  exist anywhere), and it only redacts the *audit copy* — the agent itself still sees and
  forwards the real data downstream.

- `AgentManifest.Invariants` (`pkg/manifest/manifest.go:21`) — has exactly the right
  *shape* for this. `pkg/manifest/manifest_test.go` even includes the example
  `"never_exfiltrate_pii: tool_output must not contain SSN"` as a documented invariant.
  But grep across `pkg/` confirms **no validator or mutator reads `Invariants` at all**.
  It is parsed into the struct and goes nowhere. It's pure documentation today — a
  declared intent with no enforcement path.

- Phase 5 finding F1 (`project_phase5_lab_findings`) is the empirical confirmation: the
  one component that *could* plausibly interpret a free-text invariant like the SSN one
  above — `SemanticFirewallValidator` — let an SSN-exfiltration scenario through. The
  component a biometrics deployment would most need to trust on this exact question is
  the one with a known gap on this exact question.

**Direction**: a **content-based PII/biometric classification step** — deterministic
pattern/entity matching (SSN/national-ID formats, email/phone shapes, biometric-template
byte signatures, and `face_embedding`/`voiceprint`/`*_template`-shaped parameter keys
regardless of profile), running as:

- a **validator**, pre-call — is this tool call about to send biometric/PII content
  somewhere outside the declared `allowedTools`/`AllowedSystems`? This is an Art. 5
  control (biometric data leaving the agent's authorized boundary is closer to "DENY,
  unconditionally" than "REVIEW") — same enforcement primitive as
  `ToolAuthorizationValidator`'s `deniedTools`, but content-triggered rather than
  tool-name-triggered.

- a **mutator**, post-call — redact biometric/PII content from `data_ref_out` before
  it's written to the audit store / `context/snapshots/entries.json`, independent of
  whether the policy author anticipated that specific field name. This generalizes
  `auditRedactPatterns` from "IBAN-shaped strings, open-banking only" to "PII/biometric
  shapes, every profile" and would be deterministic — not dependent on the semantic
  firewall's judgment, closing the F1 gap for this category specifically.

One capability, four payoffs: it's an **Art. 5** control (biometric categorization /
exfiltration → DENY), an **Art. 10** improvement (the runtime data-flow report now has
an actual content classification, not just `data_refs_in`/`data_ref_out` pointers), the
*data-sensitivity* half of **Art. 26(3)** (complementing the mission-fidelity half from
§2–3), and direct input to **Art. 26(8)**'s DPIA (a content-based PII inventory is
exactly what a DPIA asks for). It's narrower in scope than cap 8.2's Annex IV/Art. 26
export — and complementary to it: cap 8.2 exports evidence, this generates better
evidence to export, and closes a named adversarial-validation finding (F1) at the same
time.

---

## 6. Synthesis

| Art. 26 area | Current state | This paper's finding | Candidate next step |
|---|---|---|---|
| (3) Input data quality | `ParameterSanitizer` only (article map's prior mapping) | A second, more direct mechanism already exists: manifest+PRD-fused `IntentModel` baseline + per-call `DriftScore` (§2) | Drift-timeline report (no new code); baseline re-anchoring (§3, options a/b) |
| (2)/(4) Oversight | `dualApprovalRoles`, REVIEW/TERMINATE | Confirmed strong — no gap | none |
| (6)/(10) Transparency | Knowledge/Workflow | Confirmed correct scoping | none |
| (3)+(8)+Art.10/Art.5 PII & biometric | Shape-based redaction only; `Invariants` unenforced; F1 open | Content-based classification absent (§5) | Content-based PII/biometric validator + mutator |
| (11) Regulatory cooperation | `/api/audit` | Confirmed complete | none |

Two candidates fall out of this analysis, neither implemented, both additive to existing
primitives rather than new subsystems:

1. **Re-anchorable / multi-snapshot intent baseline** (§3) — turns a one-shot drift check
   into a genuine work-snapshot timeline; the natural Art. 26(3) evidence artifact for an
   agentic deployer.
2. **Content-based PII/biometric validator + mutator** (§5) — highest relevance for a
   biometrics-vertical pilot specifically; closes Phase 5 finding F1; strengthens
   Art. 5, 10, 26(3), and 26(8) simultaneously.

Both are about producing **better evidence**, consistent with the framing repeated
throughout this document and the article map: PerchGuard's job is the evidence trail,
not the compliance determination.
