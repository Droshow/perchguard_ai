# EU AI Act — Article-by-Article Delivery Map for PerchGuard / Regentics

**Source**: Regulation (EU) 2024/1689 (CELEX `32024R1689`), 113 articles + 13 annexes.
**Companion docs**: `PHASE8-AGENTIC-COMPLIANCE.md` (product thesis), `SALES-MARKETING-AGENTIC-COMPLIANCE.md` (go-to-market).

## Purpose

When Regentics (PerchGuard's platform brand) is in front of a customer talking about the
EU AI Act, this document is the working reference for **what we can actually put on the
table, article by article** — and what we deliberately do not claim.

**The non-negotiable framing, repeated from `PHASE8-AGENTIC-COMPLIANCE.md` §4 and
`SALES-MARKETING-AGENTIC-COMPLIANCE.md` §7:**

> PerchGuard produces **evidence that maps onto** a regime's requirements. It does not
> certify compliance, replace legal review, or guarantee a regulator's acceptance.
> Say "here is the evidence trail," never "this makes you compliant."

Every entry below should be read with that sentence attached. Where an article imposes an
obligation on a "provider of an AI system" or "notified body" that has nothing to do with
a runtime governance layer, this document says so plainly and moves on — padding every
article with invented deliverables would be the opposite of the evidence-based posture
this product is built on.

## How to read each entry

Each in-scope article gets up to four deliverable types:

| Type | Meaning | Example |
|------|---------|---------|
| **Knowledge** | Plain-language explainer, applicability question, or checklist a consultant walks the customer through | "Does your agent fall under Annex III point 4(a)?" |
| **Report** | A document/export generated from PerchGuard's existing audit data, mapped to the article's evidence requirements | Annex IV technical-documentation export |
| **Workflow** | A process the customer runs using PerchGuard primitives — human steps plus PerchGuard outputs | REVIEW → Slack approval → audit record |
| **Code Automation** | A concrete PerchGuard capability (shipped or Phase 8 planned) that produces the artifact automatically | `pkg/audit.SessionAuditRecord`, `/api/audit` |

Articles are grouped by their official Title/Chapter. Articles with no realistic mapping
to an agentic admission-control layer (notified-body administration, sectoral legislative
amendments, committee procedure, etc.) are listed in compact tables with a one-line note —
covering them for completeness ("article after article") without manufacturing deliverables
that don't exist.

**Depth is proportional to relevance**, not to article number. Articles 5, 9, 14, 19, 26,
50, 72, 73 and Annexes III/IV are the spine of the consulting offer — they get full
treatment. Everything else is covered, but briefly.

---

# TITLE I — General Provisions (Articles 1–4)

### Article 1 — Subject matter

Lays down harmonised rules for the EU AI market: prohibited practices, requirements for
high-risk systems, transparency rules, market rules for GPAI models, governance and
enforcement.

- **Knowledge**: One-page "is this regulation relevant to you" briefing — if a customer is
  placing AI systems on the EU market or deploying them for EU-based effects, the rest of
  this document applies to them.

### Article 2 — Scope

Defines who the Act applies to (providers, deployers, importers, distributors, product
manufacturers, authorised representatives, affected persons) and the exclusions (military/
defence/national security, pure scientific R&D, free/open-source components outside Annex
III/prohibited-practice/transparency obligations, personal non-professional use).

- **Knowledge**: Applicability worksheet — walks the customer through Art. 2(1)–(12) to
  determine which role(s) (provider vs. deployer — usually **deployer** for our customers,
  since they're running agents against internal/external systems, not placing a packaged
  AI product on the market) and which exclusions might apply.
- **Report**: A one-page "scope determination memo" — not a compliance certificate, but a
  documented record of the reasoning, useful as the first artifact in a customer's AI Act
  file.

### Article 3 — Definitions

68 defined terms. The ones that recur throughout this document:

| Term | Art. 3 point | Why it matters here |
|------|-------------|---------------------|
| "AI system" | (1) | Determines whether the agent PerchGuard governs is in scope at all |
| "provider" / "deployer" | (3) / (4) | Almost all PerchGuard customers are **deployers** |
| "high-risk AI system" | (63) | Gateway to Title III obligations — see Art. 6 / Annex III |
| "serious incident" | (49) | Maps directly to Art. 73 reporting — see below |
| "substantial modification" | (23) | Relevant to when a re-assessment of risk classification is triggered |
| "general-purpose AI model" / "systemic risk" | (63)/(65) | Relevant if the governed agent is itself built on a GPAI model the customer also provides |

- **Knowledge**: A glossary mapping these 68 terms to PerchGuard/Regentics vocabulary
  (e.g., "deployer" ≈ the customer; "AI system" ≈ the governed agent; "serious incident" ≈
  a `DENY`/`TERMINATE` decision plus the underlying behavior pattern that triggered it).
  This glossary is reused across every other article entry in this document.

### Article 4 — AI literacy

Providers and deployers must take measures ensuring sufficient AI literacy of staff and
others dealing with the operation/use of AI systems on their behalf, considering technical
knowledge, experience, education, and the context of use.

- **Knowledge**: Training-needs checklist tailored to the roles that actually touch the
  governed agent (operators who can approve `REVIEW` decisions, on-call engineers who read
  the audit trail, etc.).
- **Workflow**: A short onboarding workflow for new approvers in the HITL review queue —
  "before you're added to `dualApprovalRoles`, you complete this AI-literacy checklist."
  The checklist completion record itself becomes part of the evidence trail (who is
  qualified to approve what).

---

# TITLE II — Prohibited AI Practices (Article 5)

### Article 5 — Prohibited AI practices

Eight categories of AI practice are **banned outright** in the Union (subject to narrow
exceptions): subliminal/manipulative or deceptive techniques that materially distort
behaviour and cause significant harm; exploitation of vulnerabilities (age, disability,
socioeconomic situation); social scoring leading to detrimental/unfavourable treatment;
profiling-based prediction of criminal offences (with narrow exception for supporting human
assessment based on objective verifiable facts); untargeted scraping of facial images to
build/expand facial-recognition databases; emotion inference in workplace/education
(narrow medical/safety exceptions); biometric categorisation to infer race, political
opinions, trade union membership, religion, sex life, or sexual orientation; and real-time
remote biometric identification in publicly accessible spaces for law enforcement (subject
to a tightly scoped, judicially-authorised exception list).

This is the article with the cleanest possible mapping onto PerchGuard's enforcement model:
**Article 5 is a `DENY` list, expressed as policy.**

- **Knowledge**: Walk the customer through each of the eight Art. 5(1) points against their
  agent's actual tool surface and prompts. Most B2B agentic deployments will not be doing
  social scoring or remote biometric ID — but prompt-injection-driven "manipulative
  technique" exposure (5(1)(a)) and workplace emotion-inference via sentiment-analysis
  tools (5(1)(f)) are realistic findings for customer-support or HR-adjacent agents.
- **Report**: A "prohibited-practice exposure scan" — query the audit store for any tool
  calls whose `tool` name or parameters resemble the eight categories (emotion-recognition
  APIs, biometric/facial-recognition endpoints, social-scoring data sources) even if no
  such call was ever attempted. Absence of matches is itself reportable evidence.
- **Workflow**: Where a tool genuinely could realize a prohibited practice (e.g., an HR
  agent with access to an emotion-recognition API), the remediation workflow is: add the
  tool to `policies.toolAuthorization.agentRoles.<role>.deniedTools` — not `alwaysReview`,
  because Art. 5 practices cannot be cured by human sign-off. A `REVIEW` outcome on a
  prohibited practice would itself be evidence of a process failure, not a control.
- **Code Automation**:
  - `pkg/admission/validator/tool_authorization.go` (`ToolAuthorizationValidator`) —
    `deniedTools` entries return `DENY` unconditionally, before any human-review path is
    considered. This is the enforcement primitive for Art. 5.
  - `pkg/admission/validator/semantic_firewall.go` (`SemanticFirewallValidator`) — for the
    "manipulative technique" and "exploitation of vulnerabilities" categories that can't be
    captured by a tool allow/deny list (the *intent* behind a call, not the tool itself),
    the semantic firewall's policy prompt is the place to encode "would this action
    manipulate or exploit the end user" as an explicit check.
  - `pkg/telemetry/metrics.go` — `BlockedToolsTotal{tool, reason}` gives a live count of
    blocked-as-prohibited-practice calls; `perchguard_blocked_tools_total{reason="art5_prohibited"}`
    would be the Grafana panel for this article (reason string is policy-author-defined
    today — Phase 8 would standardize a small set of regulation-tagged reason codes).

---

# TITLE III — High-Risk AI Systems

## Chapter 1 — Classification (Articles 6–7)

### Article 6 — Classification rules for high-risk AI systems

An AI system is "high-risk" if (a) it's a safety component of, or is itself, a product
covered by Annex I harmonisation legislation requiring third-party conformity assessment,
or (b) it falls within an Annex III use-case — **unless** it performs only a narrow
procedural task, improves the result of a previously completed human activity, detects
decision-making patterns without replacing/influencing human assessment, or performs a
preparatory task (the profiling exception always makes it high-risk regardless). Providers
must document this assessment **before** placing the system on the market.

- **Knowledge**: Classification interview against Annex III's eight areas (see Annex III
  entry below) plus the four derogation conditions in Art. 6(3) — produces a documented
  "is this agent high-risk" determination with reasoning, not a self-certification.
- **Report**: A "risk classification record" — the interview output, timestamped, stored
  alongside the policy profile. If the agent is later modified in ways that could change
  the answer (new tool added, new Annex III-adjacent use case), PerchGuard's policy-reload
  audit trail (`PolicyReloadTotal`) gives a timestamped record of *when* the profile
  changed, supporting the "documented before placing on market / before substantial
  modification" requirement.
- **Code Automation**:
  - The policy profile (`configs/profiles/<vertical>.yaml`) is itself a place to record the
    classification outcome as metadata — e.g., a top-level `compliance.euAiActRiskTags:
    [annexIII-4a, annexIII-4b]` field (Phase 8, cap 8.2) that downstream report templates
    read directly, so the classification and the enforcement policy can never silently
    drift apart.

### Article 7 — Amendments to Annex III

Empowers the Commission to add new high-risk use-cases to Annex III via delegated acts,
under specific conditions (similar level of risk to existing entries, considering purpose,
extent of use, data, autonomy/human-oversight interplay, etc.).

- **Knowledge**: This is a **monitoring obligation**, not an action item — flag to the
  customer that Annex III is a living list and their risk classification (Art. 6) should be
  re-checked periodically. (Phase 8 cap 8.5's regulatory-text-ingestion sync, once built,
  would surface Annex III diffs automatically — see `PHASE8-AGENTIC-COMPLIANCE.md` §7.4.)

---

## Chapter 2 — Requirements for High-Risk AI Systems (Articles 8–15)

This chapter is the heart of the Act for any deployer running high-risk agentic workflows,
and the heart of PerchGuard's mapping. Articles 8–15 are the "Section 2" requirements
referenced by name throughout the rest of the Act (Art. 16, 26, 43, 99, Annex IV, etc.).

### Article 8 — Compliance with the requirements

High-risk AI systems must comply with Articles 9–15, taking into account their intended
purpose and the generally acknowledged state of the art; the risk management system in
Art. 9 ensures this compliance.

- **Knowledge**: This article is the umbrella — the entries for Art. 9–15 below are its
  substance. No separate deliverable beyond framing the rest of this chapter as "what Art.
  8 requires in practice."

### Article 9 — Risk management system

A continuous, iterative risk management process across the AI system's lifecycle:
identify and analyse known/foreseeable risks; estimate/evaluate risks from intended use and
reasonably foreseeable misuse; evaluate risks emerging from post-market monitoring data;
adopt targeted risk-management measures; test to identify the most appropriate measures;
give particular consideration to whether the system is likely to be accessed by or have an
impact on children/vulnerable groups.

This is the article that **`PHASE8-AGENTIC-COMPLIANCE.md` §7.2 maps directly onto the
policy profile structure**.

- **Knowledge**: Risk-management workshop using the policy profile as the artifact —
  walking the customer through "what are the foreseeable misuse paths for this agent"
  produces the `agentRoles`, `behaviorPatterns`, and `alwaysReview` entries of their
  profile, which *is* the documented risk management measure.
- **Report**: A "risk management system record" generated from the policy profile +
  `governance.json` snapshot — for each named behavior pattern (e.g., open-banking's
  `balance_sweep_transfer`), the report shows: the foreseeable-misuse rationale (from the
  profile's comments/metadata), the control (the pattern definition + threshold), and
  evidence it has fired (or not) in the audit store.
- **Workflow**: The "test to identify the most appropriate measures" requirement maps onto
  the Phase 5 adversarial validation lab — running the red-team agent
  (`cmd/redteam-agent/`) against a policy profile **is** the testing activity Art. 9(8)
  describes, and its findings (`project_phase5_lab_findings` — 15/18 pass) are exactly the
  kind of "iterative" evidence Art. 9(2) calls for: each finding becomes a profile change,
  each change gets re-tested.
- **Code Automation**:
  - `configs/profiles/<vertical>.yaml` — the structural artifact: `policies.agentFleet.behaviorPatterns`,
    `policies.toolAuthorization.agentRoles.*.alwaysReview`, `policies.sessionBudget`
    (anomaly detection thresholds) are all risk-management *measures* in the Art. 9 sense.
  - `pkg/agent/behavior.go` (`BehaviorAnalyzer`) — the runtime component that detects when
    a foreseeable-misuse sequence (a "behavior pattern") actually occurs.
  - `pkg/agent/risk.go` (`RiskAccumulator`) — the cumulative 0.0–1.0 risk score with named
    thresholds (`RiskThresholdHumanReview = 0.7`, `RiskThresholdTerminate = 0.9`) is a
    direct, inspectable implementation of "risk-management measures" with documented
    decision points.

### Article 10 — Data and data governance

Training, validation and testing datasets for high-risk AI systems must be subject to data
governance practices appropriate to their intended purpose: relevant design choices,
provenance, collection processes, data preparation (annotation, labelling, cleaning,
aggregation), formulation of assumptions, assessment of availability/quantity/suitability,
examination for biases likely to affect health/safety/fundamental rights, and identification
of data gaps.

PerchGuard does not train or curate the underlying models — this article's primary
addressee is the model/system **provider**. For the **deployer** side that PerchGuard
governs, the relevant slice is *what data the agent reads and writes at runtime*.

- **Knowledge**: A scoping conversation distinguishing "Art. 10 obligations belong to your
  model/system provider — here's what to ask them for" from "what PerchGuard can evidence
  about *your* agent's runtime data flows."
- **Report**: A data-flow report derived from the audit store's `data_refs_in` /
  `data_ref_out` fields (`pkg/audit/sink.go:53-54`) — every tool call's declared data
  inputs and outputs, which is the runtime-data-governance evidence a deployer *can*
  produce, complementary to (not a substitute for) the provider's training-data governance
  documentation.
- **Code Automation**: `pkg/admission/validator/lineage.go` (`LineageValidator`) +
  `pkg/store/lineage.go` — already track data lineage across tool calls within a session;
  this is the existing primitive the Art. 10 data-flow report would query. (Note from Phase
  5 findings: `policy_hit` is currently `null` on lineage-validator decisions — fixing that
  is a prerequisite for a clean Art. 10 report, tracked separately from this document.)

### Article 11 — Technical documentation

Technical documentation must be drawn up *before* the system is placed on the market, kept
up to date, and contain the information in Annex IV (see Annex IV entry below), in a form
demonstrating compliance and providing authorities with the information needed to assess
it. SMEs may use a simplified form.

- **Report**: This is the **Annex IV export** — PerchGuard's contribution is populating the
  *operational* sections of Annex IV (human oversight measures actually configured, logging
  capabilities, post-market monitoring plan and its evidence) from the live policy profile
  and audit store, while the *design/training* sections (architecture, training
  methodology, datasets) remain the provider's to author. See Annex IV below for the
  section-by-section breakdown.
- **Code Automation**: `perchguard compliance export --regime=eu-ai-act --doc=annex-iv`
  (Phase 8, cap 8.2/8.4 — not yet built) is the planned CLI surface. Today, the same
  sections can be assembled manually from `/api/audit`, the policy profile YAML, and a
  `governance.json` snapshot — a "hand-formatted Day 0 pilot artifact while the export
  tooling is built in parallel" (per `SALES-MARKETING-AGENTIC-COMPLIANCE.md` §7).

### Article 12 — Record-keeping

High-risk AI systems must technically enable automatic recording of events ("logs") over
their lifetime, to a level ensuring traceability of functioning appropriate to the
intended purpose and conforming to recognised standards. For remote biometric
identification systems specifically (Annex III point 1(a)), logs must include the period
and reference database for each use, input data for which the search led to a match, and
the identity of the persons who verified the result.

This is, almost word for word, **what PerchGuard's audit pipeline already does for every
governed tool call.**

- **Report**: The session audit record (`pkg/audit/sink.go` — `SessionAuditRecord` with
  `Steps []ToolCallStep`, each carrying `Seq`, `Timestamp`, `Tool`, `Decision`, `RiskScore`,
  `Reason`, `PolicyHit`) **is** the Art. 12 log, generated automatically rather than as a
  bolt-on. No customer-side logging code is required for tool calls that go through the
  admission pipeline.
- **Workflow**: For the Annex III point 1(a) biometric-specific fields (reference database,
  match result, human verifier identity) — if a customer's agent does call a biometric
  system, the workflow is to ensure those fields are passed as tool-call parameters so the
  `ParameterSanitizer`/audit step captures them under `data_refs_in`/`data_ref_out`, and
  that the "verified by" identity corresponds to the human reviewer recorded by the HITL
  workflow (Art. 14 below).
- **Code Automation**:
  - `pkg/audit/sink.go` (`TeeAuditLogger`, `AuditRingBuffer`) — automatic, per-call logging.
  - `pkg/store/sqlite.go` — durable, queryable log storage (the "over their lifetime"
    requirement — ring buffers alone don't satisfy this; SQLite persistence does).
  - `pkg/store/loki.go` — log shipping for centralized retention beyond the local store.

### Article 13 — Transparency and provision of information to deployers

High-risk AI systems must be designed so their operation is sufficiently transparent for
deployers to interpret and use the output appropriately, and accompanied by instructions
for use covering: provider identity/contact, characteristics/capabilities/limitations
(including intended purpose and accuracy/robustness/cybersecurity levels), known/
foreseeable circumstances leading to risks, performance with respect to specific persons or
groups, input data specifications, human oversight measures (including the technical
measures to facilitate interpretation of outputs by deployers — referenced explicitly by
Annex IV point 2(e)), and expected lifetime/maintenance.

- **Knowledge**: For deployers, Art. 13 is what they should be **receiving** from their
  model/agent-framework provider — a checklist for "what's missing from the instructions
  for use we were given."
- **Report**: Where PerchGuard *is* effectively part of the "system" the deployer is
  operating (e.g., the policy profile + admission pipeline configuration), the policy
  profile YAML plus this document's article-by-article mapping doubles as the "instructions
  for use" artifact for *that layer* — what it does, its limitations (e.g., semantic
  firewall provider must not be the same as the governed agent's provider — already
  documented in `configs/profiles/open-banking.yaml`), and the human-oversight measures
  configured.

### Article 14 — Human oversight

High-risk AI systems must be designed to be effectively overseen by natural persons during
use, with measures proportionate to the risks, identified before market placement, and
enabling the overseeing person(s) to: understand the system's capacities and limitations
and properly monitor its operation (including detecting anomalies/malfunctions);
remain aware of "automation bias"; correctly interpret the system's output; decide, in any
particular situation, **not to use** the system or to **disregard, override, or reverse**
its output; and **intervene or interrupt** operation via a "stop" mechanism. For remote
biometric identification (Annex III point 1(a)), no action or decision may be based solely
on the identification result without separate verification by at least two natural persons.

**This is the headline mapping** — `PHASE8-AGENTIC-COMPLIANCE.md` §7.2 and
`SALES-MARKETING-AGENTIC-COMPLIANCE.md` §1 both lead with this article: *"Article 14
requires human oversight of high-risk AI systems — that's our REVIEW outcome, verbatim."*

- **Knowledge**: Map each Art. 14(4) capability to a concrete PerchGuard configuration
  decision: "decide not to use / disregard / override / reverse output" → which
  `agentRoles.*.alwaysReview` entries exist and what reviewers are shown; "intervene or
  interrupt via stop mechanism" → session termination at `RiskThresholdTerminate = 0.9`
  and/or manual `DELETE /api/sessions/{id}`.
- **Report**: The **REVIEW decision record** is the Art. 14 evidence artifact, full stop.
  For each `REVIEW` outcome: the tool call that triggered it, the policy/threshold that
  fired (`PolicyHit`), the reviewer who acted, their decision (approve/deny), and the
  timestamp — i.e., exactly "a natural person oversaw this AI system's output and made a
  decision about it." `policies.humanReview.regulatoryAuditFields` in
  `configs/profiles/open-banking.yaml` already names the fields a regulator would expect in
  this record.
- **Workflow**: The end-to-end HITL flow **is** the Art. 14 workflow: agent proposes a tool
  call → `Interceptor` returns `REVIEW` → `pkg/humanreview/webhook.go` (`Dispatcher`)
  notifies a human via Slack → reviewer approves/denies via `/api/review/{id}` →
  `pkg/review/store.go` records the `PendingReview` outcome → the agent's call proceeds or
  is blocked → the full sequence is in the audit trail. For Annex III point 1(a)'s
  "two natural persons" requirement, `policies.admission.dualApprovalRoles` is the existing
  configuration knob.
- **Code Automation**:
  - `pkg/review/store.go` (`Store`, `PendingReview`, `Status`) — the review queue and its
    state machine.
  - `pkg/humanreview/webhook.go` (`Dispatcher`) — Slack notification on `REVIEW`.
  - `/api/review/{id}/wait` long-poll endpoint (Phase 7) — for synchronous agent flows that
    must block on human sign-off.
  - `pkg/telemetry/metrics.go` — `PendingReviewsGauge` is a live "how many Art. 14
    decisions are awaiting a human right now" metric, suitable for an ops dashboard panel.
  - `policies.admission.dualApprovalRoles` (policy profile) — the two-person rule.

### Article 15 — Accuracy, robustness and cybersecurity

High-risk AI systems must be designed to achieve appropriate levels of accuracy, robustness
and cybersecurity, and perform consistently throughout their lifecycle. They must be
resilient to errors, faults or inconsistencies (with technical redundancy/backup/fail-safe
measures where appropriate), and resilient against attempts by unauthorised third parties
to alter their use, outputs or performance by exploiting vulnerabilities — including data
poisoning, model poisoning, adversarial examples, confidentiality attacks, and model
flaws/exfiltration.

- **Knowledge**: This article's "robustness against adversarial exploitation" language maps
  closely onto PerchGuard's own adversarial-validation discipline (Phase 5) — useful as a
  talking point: *"we red-team our own admission layer for exactly the attack classes Art.
  15 names; here's what we found and fixed."* (per
  `SALES-MARKETING-AGENTIC-COMPLIANCE.md` §5, "we ran the red team against our own agent.")
- **Report**: The Phase 5 adversarial-validation findings (`project_phase5_lab_findings` —
  15/18 pass, including the F1 semantic-firewall SSN-exfiltration gap and its fix) become a
  template for the customer's own "Art. 15 resilience evidence": run the same red-team
  categories (prompt injection, data exfiltration, tool-authorization bypass, semantic
  firewall evasion) against *their* agent + policy profile, document pass/fail, document
  remediation.
- **Code Automation**:
  - `pkg/admission/validator/prompt_injection.go`, `data_exfiltration.go`,
    `tool_authorization.go`, `semantic_firewall.go` — the validators that *are* the
    Art. 15 robustness controls for an agentic system (adversarial-prompt resistance,
    exfiltration resistance, scope-of-action resistance).
  - `cmd/redteam-agent/` — the adversarial test harness; its findings are the Art. 15
    "appropriate level of accuracy/robustness... throughout the lifecycle" evidence,
    re-runnable on every policy change.

---

## Chapter 3 — Obligations of Providers and Deployers (Articles 16–27)

### Article 16 — Obligations of providers of high-risk AI systems

A 12-point checklist (a)–(l): ensure conformity with Chapter III Section 2; have
provider identification on the system/documentation; have a QMS (Art. 17); retain
documentation (Art. 18); keep logs (Art. 19) where under provider control; conformity
assessment before market placement; EU declaration of conformity (Art. 47); CE marking
(Art. 48); registration (Art. 49); corrective actions (Art. 20); demonstrate conformity on
request (Art. 21); accessibility requirements.

- **Knowledge**: A crosswalk table — for each of the 12 points, either "provider-side
  obligation outside this layer's scope" or a pointer to the article entry above/below
  where PerchGuard contributes evidence (logs → Art. 19, registration → Art. 49,
  corrective actions → Art. 20, demonstrating conformity → Art. 21). Most PerchGuard
  customers are **deployers**, not providers — this checklist's main use is helping a
  deployer-customer ask their model/agent-framework *provider* the right questions.

### Article 17 — Quality management system

Providers must operate a documented QMS covering: regulatory compliance strategy, design
control, V&V procedures, technical specifications/standards applied, data management,
the Art. 9 risk management system, the Art. 72 post-market monitoring system, Art. 73
incident-reporting procedures, communication with authorities, record-keeping, resource
management, and an accountability framework — proportionate to organisation size (SME
allowances).

- **Knowledge**: For Regentics' own QMS-equivalent (relevant if Regentics is ever itself a
  "provider" in a customer's value chain — see Art. 25 below), the project's own engineering
  gates double as a worked example: `go vet` post-edit hooks, the pre-push security gate,
  `/security-review`/`/code-review` agent checklist, and `SECURITY_CHECKLIST.md` are a QMS
  in miniature for the admission-control layer itself.
- **Report**: For the customer's QMS chapter on "post-market monitoring" and "incident
  reporting," the Art. 72/Art. 73 entries below are the evidence sources to cite directly.

### Article 18 — Documentation keeping

Providers must retain technical documentation (Art. 11), QMS documentation, notified-body
decisions, and the EU declaration of conformity (Art. 47) for **10 years** after the AI
system is placed on the market, available to national competent authorities on request.

- **Knowledge**: A 10-year retention requirement is materially longer than most
  application log-retention defaults — flag this explicitly during policy-profile design.
- **Code Automation**: `pkg/store/sqlite.go` (durable audit store) + `pkg/store/reaper.go`
  (retention/expiry) — the reaper's retention window is currently an operational
  configuration choice, not a regulatory one. For an EU AI Act–scoped deployment, the
  retention policy should be set with Art. 18's 10-year figure (and Art. 19's 6-month
  *minimum*, see below) explicitly in mind, with `pkg/store/loki.go` as the long-term
  archive tier beyond what SQLite retains locally.

### Article 19 — Automatically generated logs

Providers must keep the logs automatically generated by their high-risk AI systems (to the
extent under their control) for a period appropriate to the system's intended purpose, and
**at least six months**, unless other Union/national law (especially data protection law)
provides otherwise.

- **Knowledge**: Retention-period determination is a three-way negotiation: Art. 19's
  6-month *floor*, Art. 18's 10-year figure for the *technical documentation* (a different
  artifact), and GDPR/data-minimisation pressure toward *shorter* retention for personal
  data within those logs. The `policies.audit.redactedFields` pattern (already in
  `configs/profiles/open-banking.yaml`) is the mechanism for keeping the audit trail
  long-lived while minimising personal-data content within it.
- **Report**: A "log retention configuration record" — what's retained, for how long, where
  (ring buffer / SQLite / Loki), and what's redacted before storage — is itself a piece of
  Art. 19 evidence (demonstrating the retention *policy* exists and is enforced, not just
  that logs happen to exist).
- **Code Automation**: `pkg/audit/sink.go` (`SessionAuditRecord`, automatic per-call) +
  `pkg/store/sqlite.go` (durable persistence, satisfying "kept" beyond process lifetime) +
  `pkg/store/reaper.go` (the configurable retention window that should be set ≥ 6 months
  for Art. 19 purposes).

### Article 20 — Corrective actions and duty of information

If a provider has reason to believe a high-risk AI system they placed on the market is not
in conformity, they must immediately take corrective action (bring into conformity,
withdraw, disable, or recall) and inform relevant operators. If the system presents a risk
under Art. 79(1), the provider must immediately investigate the causes and inform the
relevant market surveillance authorities (and notified body, where applicable), with
details of the non-compliance and corrective action taken.

- **Workflow**: For a deployer running PerchGuard, the closest analogue is a
  `TERMINATE`-level decision (`RiskThresholdTerminate = 0.9`, `pkg/agent/risk.go`) or a
  repeated/critical `DENY` matching a named behavior pattern: this is the trigger event for
  "we have reason to believe..." The workflow is: `TERMINATE`/critical `DENY` fires → audit
  record captures the triggering sequence → incident workflow (same shape as Art. 73 below)
  → if the root cause traces back to the AI system/model itself (not just a misconfigured
  policy), the customer has the documented basis to notify their *provider* per Art. 20.
- **Report**: The audit record for the triggering session (`SessionAuditRecord` with
  `TerminatedEarly: true`) is the "details of the non-compliance" artifact referenced in
  Art. 20's information duty.

### Article 21 — Cooperation with competent authorities

On reasoned request, providers must give competent authorities all information and
documentation (in an understandable language) demonstrating conformity, and access to the
Art. 19 logs to the extent under their control — all subject to the Art. 78 confidentiality
regime.

- **Report**: A "regulator export" — `/api/audit` filtered by date range / session / tool,
  packaged with the relevant policy-profile excerpt and (once built) the Annex IV export —
  is the artifact this article calls for. This is explicitly an **export of evidence**, not
  a compliance attestation; the framing to the customer is "if a regulator asks, here is
  what you hand them, generated in minutes, not weeks."
- **Code Automation**: `GET /api/audit?session_id=...&decision=...&tool=...` (existing,
  `pkg/api/audit.go`) is the query primitive; a `perchguard compliance export` wrapper
  (Phase 8, cap 8.4) would add the packaging/formatting layer on top.

### Articles 22–24 — Authorised representatives, importers, distributors

| Article | Topic | PerchGuard relevance |
|---------|-------|----------------------|
| 22 | Authorised representatives of non-EU providers | Supply-chain role PerchGuard customers don't occupy as deployers. **Knowledge only**: if a customer *is* a non-EU provider needing an EU authorised representative, that's a legal/corporate-structure question outside this layer. |
| 23 | Obligations of importers | Same — importer is a product-supply-chain role, not the deployer-of-an-agent role PerchGuard governs. |
| 24 | Obligations of distributors | Same. |

These three articles are listed for completeness but produce no PerchGuard-specific
deliverable — they describe roles in the hardware/software *product* supply chain that an
agentic-admission-control deployment does not occupy.

### Article 25 — Responsibilities along the AI value chain

A distributor, importer, deployer, or other third party becomes a **"provider"** (and
takes on the full Art. 16 obligation set) if they: (a) put their name/trademark on a
high-risk AI system already on the market; (b) make a **substantial modification** to a
high-risk AI system that keeps it high-risk; or (c) modify the intended purpose of a
non-high-risk system (including a GPAI-based system) such that it **becomes** high-risk
under Art. 6. Providers of GPAI models and providers integrating them into AI systems must
cooperate to support each other's compliance, subject to IP/trade-secret protections.

This is the article that makes "what did this agent do, and did its purpose drift from what
it was declared for" a *legal* question, not just an operational one — and that is exactly
what PerchGuard's intent/delegation tracking already watches for.

- **Knowledge**: A "have you crossed the line into being a provider" checklist — has the
  customer's agent been given tools/scopes that go meaningfully beyond the upstream
  model/framework's declared intended purpose? If so, Art. 25(1)(c) may have just made them
  a provider of a (now) high-risk AI system, with the full Art. 16 obligation set attaching.
- **Report**: A "purpose-drift report" comparing the agent's `AgentManifest.Mission`
  (`pkg/manifest/manifest.go` — `Summary`, `Scope`, `OutOfScope`) at registration time
  against the actual tool-call history in the audit store. Drift between declared scope and
  observed behavior is precisely the signal Art. 25(1)(c) asks an operator to watch for.
- **Code Automation**:
  - `pkg/manifest/manifest.go` (`AgentManifest.Mission.Scope` / `.OutOfScope`,
    `IntentText()`) — the declared-intent baseline.
  - `pkg/agent/intent.go` (`IntentModel`) + `pkg/agent/session.go` (`SessionAgent`) — the
    runtime drift-detection primitives that compare observed tool calls against that
    baseline.
  - `ParentSessionID` / `pkg/agent/delegation.go` (`DelegationStore`) — when one agent
    delegates to another, the delegation chain is recorded; a "substantial modification"
    introduced partway down a delegation chain is traceable to the session that introduced
    it.

### Article 26 — Obligations of deployers of high-risk AI systems

**The single most relevant article in the entire Act for PerchGuard's customer base** —
nearly every PerchGuard customer is a *deployer* of (potentially high-risk) AI agents, and
Art. 26's eleven obligations map almost one-to-one onto PerchGuard configuration surfaces:

| Art. 26(§) | Obligation | PerchGuard mapping |
|---|---|---|
| (1) | Use the system in accordance with the provider's instructions for use, via appropriate technical/organisational measures | The policy profile (`configs/profiles/<vertical>.yaml`) **is** the codified "appropriate measures" |
| (2) | Assign human oversight to natural persons with the necessary competence, training, authority, and support | `policies.admission.dualApprovalRoles` + the Art. 4 AI-literacy onboarding workflow (above) |
| (3) | Ensure input data is relevant and sufficiently representative, to the extent the deployer controls input data | `pkg/admission/mutator/parameter_sanitizer.go` (`ParameterSanitizer`) — validates/sanitises tool-call parameters before they reach the agent's downstream system. A more direct mechanism also already exists: the registration-time `IntentModel` baseline (`pkg/agent/intent.go`, fused from `AgentManifest.IntentText()` + project PRD/snapshot context per `pkg/api/agents.go:82-91`) scores every subsequent tool call's `DriftScore` against it — a **drift-timeline report** (baseline text + per-call drift scores + threshold crossings) is the more faithful Art. 26(3) evidence artifact for an agentic deployer. See `EUAIACT-PERCHGUARD-SYNERGY.md` §2-3 for the full mechanism and its current gap (static baseline, captured once at registration, never re-anchored). |
| (4) | Monitor operation per instructions; inform the provider under Art. 72; suspend use and notify the provider/distributor and market surveillance authority without undue delay if there's reason to believe the system presents a risk under Art. 79(1) | `REVIEW`/`TERMINATE` decisions + the Art. 73 incident workflow (below) — "suspend use" maps to session termination or denying the relevant tool/role pending investigation |
| (5) | Keep the Art. 19 logs to the extent under deployer control, ≥ 6 months unless otherwise provided | Same audit-store retention configuration as Art. 19 above — for deployers, this is **the** log |
| (6) | Inform workers' representatives and affected workers before deploying a high-risk AI system at the workplace | **Knowledge/Workflow only** — an HR/communications process, not a PerchGuard artifact, though the policy profile's `agentRoles` definitions are a useful input to "what will this system actually do" disclosures |
| (7) | Register in the EU database (Art. 49(3)) — applies to public-authority deployers and deployers of Annex III points 5(b)/(c) (creditworthiness, life/health insurance risk assessment) systems | See Annex VIII / Art. 49 below — the agent registration data already captured at session-registration time (`POST /agents/register`) overlaps substantially with Annex VIII Section C's required fields |
| (8) | Use Art. 13 information to support a GDPR Art. 35 / Directive 2016/680 Art. 27 data protection impact assessment, where applicable | **Knowledge** — DPIA is a separate legal artifact; PerchGuard's data-flow/lineage report (Art. 10 above) is a useful *input* to it |
| (9) | Real-time remote biometric identification by law enforcement: prior authorisation, notification, annual reporting | Out of scope for nearly all PerchGuard deployments — **Knowledge flag only**, and only if Annex III point 1(a) applies |
| (10) | Inform natural persons that they are subject to a decision involving a high-risk AI system | See Art. 50 (Title IV) below — the transparency-disclosure mapping |
| (11) | Cooperate with competent authorities | Same `/api/audit` export as Art. 21 above |

- **Knowledge**: This table itself, walked through with the customer, **is** the primary
  Art. 26 deliverable — it converts an 11-point legal obligation list into 11 concrete
  "do you have this configured" questions against an existing PerchGuard deployment.
- **Report**: A "deployer obligations coverage report" — for each of the 11 sub-obligations,
  a status (configured / evidenced / not applicable / outside this layer) with a pointer to
  the supporting policy-profile section or audit query. This is the centerpiece deliverable
  of an EU AI Act pilot engagement.
- **Workflow**: Items (4) and (6) are processes, not artifacts — (4)'s "suspend use"
  workflow should be a documented runbook ("on `TERMINATE`, who is notified, who decides
  whether to re-enable the session, what gets reported to whom"); (6) is a one-time
  pre-deployment HR communication checklist.
- **Code Automation**: See the per-row mappings above — items (1), (3), (4), (5), (7), (11)
  all have direct, already-shipped PerchGuard primitives; items (2) and (6) have partial
  configuration support (`dualApprovalRoles`); items (8)–(10) are knowledge/cross-reference
  only.

#### Art. 26 coverage report preview (`art26-coverage` export)

| § | Regulator Requirement (Art. 26) | PerchGuard Deliverable | Status |
|---|---|---|---|
| (1) | Use system per provider's instructions, via appropriate technical/organisational measures | Policy profile (`configs/profiles/<vertical>.yaml`) — exported as the codified "measures" | **configured** |
| (2) | Assign human oversight to competent, trained, authorized staff | `policies.admission.dualApprovalRoles` config + Art. 4 AI-literacy onboarding record | **configured** (needs `Config.Admission` parsing) |
| (3) | Ensure input data is relevant/representative (where deployer controls it) | `ParameterSanitizer` activity from audit store + drift-timeline report (per-call `IntentModel.DriftScore` vs. registration-time baseline, `pkg/agent/intent.go` — see `EUAIACT-PERCHGUARD-SYNERGY.md` §2) | **evidenced** (drift-timeline export packaging not yet built) |
| (4) | Monitor operation; suspend use & notify on risk (Art. 79(1)) | REVIEW/TERMINATE decision records + session-termination events from audit store | **evidenced** |
| (5) | Retain Art. 19 logs ≥ 6 months | Audit store retention config/window (`pkg/store/sqlite.go`) | **evidenced** |
| (6) | Inform workers' reps / affected workers pre-deployment | Policy profile `agentRoles` as input to HR disclosure — no PerchGuard artifact | **outside_layer** (workflow only) |
| (7) | Register in EU database (Annex VIII) — Annex III 5(b)/(c) only | `POST /agents/register` session data overlaps Annex VIII §C fields | **not_applicable** unless Annex III 5(b)/(c) |
| (8) | Feed Art. 13 info into GDPR Art. 35 DPIA | Data-flow/lineage report as DPIA input — separate legal artifact | **outside_layer** |
| (9) | Real-time biometric ID by law enforcement — authorisation/reporting | N/A for nearly all deployments | **not_applicable** (Annex III 1(a) only) |
| (10) | Inform natural persons subject to a high-risk AI decision | Maps to Art. 50 transparency disclosure (separate export) | **outside_layer** |
| (11) | Cooperate with competent authorities | `/api/audit` export (same as Art. 21) | **evidenced** |

6 of 11 rows (1, 3, 4, 5, 11 fully + 2 partially) are code-path-generated `configured`/`evidenced`
rows from the existing audit store/policy profile. Rows 6, 8, 9, 10 are `outside_layer`/
`not_applicable` placeholders with a cross-reference pointer. Row 7 is conditional on Annex III
classification.

### Article 27 — Fundamental rights impact assessment for high-risk AI systems

Before deploying a high-risk AI system from Annex III (excluding point 2, critical
infrastructure), **deployers that are bodies governed by public law, private entities
providing public services, or deployers of Annex III points 5(b)/(c) systems** (creditworthiness;
life/health insurance risk assessment and pricing) must perform a fundamental rights impact
assessment (FRIA) covering: (a) the deployer's processes in which the system will be used;
(b) the period and frequency of intended use; (c) categories of natural persons/groups
likely affected; (d) specific risks of harm to those categories, taking into account the
provider's Art. 13 information; (e) implementation of human oversight measures per the
instructions for use; (f) measures to be taken if those risks materialise, including
internal-governance arrangements and complaint mechanisms. The FRIA must be notified to the
market surveillance authority via a template the AI Office will publish (an automated tool
is anticipated). Where a DPIA already covers overlapping ground, the FRIA complements
(doesn't duplicate) it.

This is the second major deployer-side article, and it's the one place in the Act where
PerchGuard's audit data feeds directly into a **named, regulator-facing template**.

- **Knowledge**: An applicability test — is the customer a public-law body, a private
  entity providing public services, or running an Annex III 5(b)/(c) system? If not, Art. 27
  doesn't apply, and that determination itself is worth documenting (negative scoping is
  still scoping).
- **Report**: A FRIA-template-shaped export, where PerchGuard contributes directly to
  sections (e) and (f) and provides supporting evidence for (c) and (d):
  - **(c) categories of affected persons** — derived from the policy profile's
    `agentRoles` and the tool surface each role can reach (who is on the other end of each
    `allowedTools` entry).
  - **(d) specific risks of harm** — the policy profile's named `behaviorPatterns` *are*
    the documented foreseeable-harm scenarios (same artifact as the Art. 9 risk-management
    report).
  - **(e) human oversight implementation** — the same REVIEW/HITL evidence as Art. 14.
  - **(f) measures if risks materialise** — the incident-response workflow (Art. 20/73) plus
    historical evidence from the audit store of those measures actually firing.
  - Sections (a) and (b) (the deployer's business processes and usage cadence) remain
    customer-authored — PerchGuard doesn't observe business process outside the tool-call
    boundary.
- **Workflow**: A FRIA is not a one-time document — it should be refreshed whenever the
  policy profile changes materially. Tying a FRIA "version" to a policy-profile version (and
  the `PolicyReloadTotal` timestamp of when it took effect) gives the FRIA a defensible
  revision history.
- **Code Automation**: A `governance.json`-style snapshot (per `pkg/manifest/`, and the
  declared-intent lab referenced in the project's `CLAUDE.md`) is the natural source object
  for an automated FRIA-section-(c)/(d)/(e) generator (Phase 8, cap 8.2) — register agent →
  fire calls → snapshot governance state is already the integration smoke test; extending
  its output shape to match the AI Office's eventual FRIA template is additive, not a new
  enforcement path.

---

## Chapter 4 — Notifying Authorities and Notified Bodies (Articles 28–39)

This entire chapter governs how Member States designate, oversee, and coordinate the
**notified bodies** that perform third-party conformity assessment (Annex VII) for
high-risk AI systems. None of it has a PerchGuard mapping — it describes a layer of EU
institutional administration upstream of any individual deployment.

| Articles | Topic |
|---|---|
| 28 | Designation of notifying authorities by Member States |
| 29 | Application procedure for a conformity assessment body to become "notified" |
| 30 | Notification procedure (Commission/Member State objection window) |
| 31 | Eligibility requirements for notified bodies (independence, competence, confidentiality) |
| 32 | Presumption of conformity for notified bodies meeting harmonised standards |
| 33 | Subsidiaries and subcontracting by notified bodies |
| 34 | Operational obligations of notified bodies |
| 35 | Commission-maintained registry of notified bodies |
| 36 | Changes/withdrawal of a body's notified status |
| 37 | Commission challenge to a notified body's competence |
| 38 | Coordination forum for notified bodies |
| 39 | Recognition of third-country conformity assessment bodies |

**Listed for completeness; no Knowledge/Report/Workflow/Code Automation entries.**

---

## Chapter 5 — Standards, Conformity Assessment, Certificates, Registration (Articles 40–49)

### Articles 40–46 — Standards, common specifications, conformity assessment, certificates, derogation

These articles define the **provider/notified-body conformity-assessment machinery**
(harmonised standards, common specifications, the Annex VI/VII assessment routes,
certificate lifecycles, and a narrow public-security derogation). PerchGuard does not
perform or replace conformity assessment — but a deployer's understanding of *which* route
their provider used materially affects how much weight the deployer should put on their
own runtime evidence.

| Article | Topic | PerchGuard relevance |
|---|---|---|
| 40 | Harmonised standards & standardisation deliverables | **Knowledge only** — context for interpreting a provider's Art. 47 declaration, which references these standards. |
| 41 | Common specifications | **Knowledge only** — Commission fallback specs where harmonised standards are absent. |
| 42 | Presumption of conformity (training-data context fit; cybersecurity certification under Reg. (EU) 2019/881) | **Knowledge** — a provider's cybersecurity certificate is worth requesting; complementary to, not a substitute for, PerchGuard's runtime Art. 15 controls. |
| 43 | Conformity assessment (Annex VI internal control vs. Annex VII notified-body route) | **Knowledge** — if the provider self-assessed under Annex VI (no notified body), the deployer's own runtime evidence (audit trail, red-team findings) carries proportionally more weight as independent verification. |
| 44 | Certificate validity (4–5 years) | **Knowledge** — certificate-renewal dates are a natural trigger to schedule a refresh of the customer's Art. 15 red-team evidence. |
| 45 | Information obligations of notified bodies | Institutional, notified-body ↔ notifying-authority reporting — no mapping. |
| 46 | Derogation from conformity assessment (public security / critical infrastructure emergencies) | **Knowledge only** — relevant mainly to law-enforcement/critical-infrastructure deployers. |

### Article 47 — EU declaration of conformity

The provider must draw up a written (machine-readable/signed) EU declaration of
conformity per high-risk AI system, retain it for 10 years, and provide it to authorities
on request. Content is specified in Annex V (see below). By issuing it, the provider
assumes responsibility for compliance with Chapter III Section 2.

- **Knowledge**: For deployers, Art. 47 is primarily a **document to request and retain**
  from the provider — PerchGuard isn't the provider of the underlying AI system and doesn't
  generate this on a deployer's behalf. Confirm the customer holds a copy covering the
  specific model/version actually in production.
- **Report**: If Regentics/PerchGuard itself ever needs to issue conformity-style
  documentation for the admission-control layer as a component (a separate, future
  productization question, not in scope here), Annex V's structure is the template.

### Article 48 — CE marking

Physical/documentary CE marking requirements for high-risk AI systems, including notified-body
identification numbers where applicable.

- **Knowledge only** — a physical/packaging marking requirement for AI systems that are or
  are embedded in products. Not applicable to a runtime governance layer. Listed for
  completeness.

### Article 49 — Registration

Before placing on the market or putting into service a high-risk AI system from Annex III
(except point 2, critical infrastructure), providers (or authorised representatives) must
register the system in the **EU database** (Art. 71). Providers of systems classified as
non-high-risk under Art. 6(3) must also register that determination. Deployers that are
public authorities/bodies, EU institutions, or private entities providing public services,
plus deployers of Annex III points 5(b)/(c) systems (creditworthiness, life/health
insurance), must separately register **their use** of the system. Annex III points 1, 6, 7
(biometrics, law enforcement, migration/border) registrations go into a restricted,
non-public section of the database.

- **Knowledge**: Determine which registration obligation(s) apply — provider registration
  (Art. 49(1)/(2)) is generally not the deployer-customer's job, but **deployer
  registration of use** (Art. 49(3)) often is, for the categories listed above.
- **Report**: Annex VIII Section C (deployer registration fields — see Annex VIII below)
  overlaps substantially with data PerchGuard already collects at agent-registration time.
  A "registration data export" assembling Section C fields from the `AgentManifest` and
  policy profile is a small, concrete Phase 8 deliverable.
- **Code Automation**: `POST /agents/register` (`pkg/api/agents.go`) already captures
  `AgentManifest.Metadata` (id, owner, created, version) and `Mission.Summary` — the seeds
  of Annex VIII Section C's "name/contact of deployer," "AI system identification," and
  "summary of intended use" fields. A `perchguard compliance export --regime=eu-ai-act
  --doc=annex-viii-c` command (Phase 8, cap 8.4) would format these alongside the FRIA
  summary (Art. 27) and any DPIA reference into the Section C shape directly.

---

# TITLE IV — Transparency Obligations for Certain AI Systems (Article 50)

### Article 50 — Transparency obligations for providers and deployers of certain AI systems

Four distinct disclosure duties:

1. **Providers** of AI systems intended to interact directly with natural persons must
   ensure people are informed they're interacting with an AI system — unless this is
   obvious to a reasonably informed person from context (narrow law-enforcement exception).
2. **Providers** of AI systems generating synthetic audio/image/video/text must mark output
   as machine-readable and detectable as AI-generated/manipulated, "as far as technically
   feasible" (exceptions for assistive editing functions and minor alterations).
3. **Deployers** of emotion-recognition or biometric-categorisation systems must inform
   exposed natural persons of the system's operation (narrow law-enforcement exception).
4. **Deployers** of systems generating "deep fake" image/audio/video content must disclose
   that the content is artificially generated/manipulated; **deployers** publishing
   AI-generated text on matters of public interest must disclose this unless the text has
   undergone human editorial review with an accountable editor.

All disclosures must be clear, distinguishable, and provided no later than the time of
first interaction/exposure, and must meet accessibility requirements.

This is **the second headline mapping** alongside Art. 14 — `PHASE8-AGENTIC-COMPLIANCE.md`
§7.2 lists Art. 50 as mapping to "audit trail requirements; agent identity disclosure."

- **Knowledge**: For nearly all PerchGuard customers, the live question is **Art. 50(1)**
  (deployer point 4's "deployer of an AI agent that interacts with people" disclosure
  obligation) — does the end user of the governed agent know they're talking to an AI? A
  short interview against the four sub-obligations identifies which apply: most B2B
  internal-tooling agents won't trigger (2)–(4) (no synthetic media generation, no
  biometric/emotion inference, no public-interest text publication), but customer-facing
  chat agents virtually always trigger (1).
- **Report**: An "agent identity disclosure audit" — for agents with end-user-facing tool
  calls (e.g., a `send_message`/`reply_to_customer` tool), query the audit store for
  whether a disclosure was present in the relevant template/prompt at time of use. This is
  a *content* check more than a *decision* check — PerchGuard's role is evidencing *that*
  the disclosure mechanism existed and was exercised, not authoring the disclosure copy.
- **Workflow**: Where disclosure is missing, the remediation is a `parameterSanitization`
  / output-template change (inject a disclosure string into outbound messages), with the
  policy-reload timestamp serving as the "as of this date, disclosure was added" evidence
  point.
- **Code Automation**:
  - `pkg/admission/validator/output_validation.go` (`OutputValidator`) — the natural place
    to assert that outbound, user-facing content includes the required AI-disclosure marker
    before it's allowed through, analogous to the existing `auditRedactPatterns` checks in
    `configs/profiles/open-banking.yaml`.
  - `pkg/audit/sink.go` — every governed tool call already carries `Tool`, `Decision`,
    `Timestamp` — the audit trail itself is evidence of *when* an agent (whose AI status
    must be disclosed) interacted with a given counterparty, supporting Art. 50(5)'s "no
    later than the time of first interaction" timing requirement.
  - `pkg/manifest/manifest.go` (`AgentManifest.Metadata`) — the registered agent identity
    is the natural source for "which AI system was this" in a disclosure-audit report.

---

# TITLE V — General-Purpose AI Models (Articles 51–56)

This entire title regulates **providers of general-purpose AI models** (foundation models)
— their classification, documentation, copyright-policy, training-data-summary, and
(for "systemic risk" models) evaluation/adversarial-testing/incident-reporting obligations.
PerchGuard customers are, in nearly all cases, **users of** GPAI models (via API), not
**providers of** them — so this title is primarily **Knowledge** territory: helping a
customer understand what their model vendor (OpenAI, Anthropic, etc.) is and isn't
obligated to provide them, and the one circumstance (Art. 25, above) under which a
customer could themselves become subject to these obligations.

| Article | Topic | PerchGuard relevance |
|---|---|---|
| 51 | Classification of GPAI models as models "with systemic risk" (>10^25 FLOPs training compute presumption) | **Knowledge** — almost certainly not the customer; informs whether their chosen foundation model carries systemic-risk obligations that affect *its* availability/terms. |
| 52 | Notification procedure for systemic-risk classification | Commission ↔ model-provider procedure — no PerchGuard mapping. |
| 53 | Provider obligations: technical documentation (Annex XI), downstream-provider information (Annex XII), copyright policy, training-data summary | **Knowledge** — Annex XII is what the customer's model vendor *should* be providing them; useful as a "request this from your vendor" checklist when assembling the customer's own Annex IV technical documentation (Art. 11), since Annex IV §2(a) references "recourse to pre-trained systems or tools provided by third parties." |
| 54 | Authorised representatives of non-EU GPAI providers | Supply-chain role — no PerchGuard mapping. |
| 55 | Additional obligations for systemic-risk GPAI providers (adversarial testing, incident reporting, cybersecurity) | **Knowledge** — conceptually parallel to PerchGuard's own Art. 15 red-team discipline, but for the model itself rather than the agent built on it; useful talking point for customers evaluating model vendors ("does your vendor publish anything like our Phase 5 findings?"). |
| 56 | Codes of practice for GPAI providers | Commission/AI-Office ↔ model-provider mechanism — no PerchGuard mapping. |

**The one actionable item from this title**: the Art. 25 "purpose drift" report (above) is
also the trigger for re-checking this title — if a deployer's modifications to a GPAI-based
system are substantial enough to make *them* a provider, Title V's obligations (Art. 53 at
minimum) attach to them directly.

---

# TITLE VI — Measures in Support of Innovation (Articles 57–63)

This title obliges **Member States** (not companies) to stand up AI regulatory sandboxes
and SME-support measures. No direct compliance obligation falls on a PerchGuard customer —
but two articles describe a structure that is conceptually identical to PerchGuard's own
Phase 5 red-team discipline, which is worth surfacing as a consulting talking point.

| Article | Topic | PerchGuard relevance |
|---|---|---|
| 57 | Member States establish national AI regulatory sandboxes by 2 Aug 2026 | **Knowledge** — informs customers that a supervised-testing route exists for novel high-risk systems; relevant if a customer is considering a sandbox application alongside (not instead of) their own governance program. |
| 58 | Detailed arrangements for sandbox operation | Institutional — no mapping. |
| 59 | Further processing of personal data for sandbox participants | Data-protection waiver mechanism for sandbox participants — no PerchGuard mapping. |
| 60 | Testing of high-risk AI systems in real-world conditions outside sandboxes | **Knowledge** — Art. 60 requires a real-world testing plan, prior approval, monitoring, and the ability to "revoke, suspend or recover" with immediate effect. PerchGuard's Phase 5 red-team lab (`cmd/redteam-agent/`, `artifacts/docs/PHASE5-ADVERSARIAL-VALIDATION.md`) is structurally the same exercise — adversarial scenarios run against a governed agent with PerchGuard able to terminate/REVIEW mid-session via `RiskAccumulator`. **Workflow**: a customer's Art. 60 real-world testing plan can reuse the Phase 5 lab format (scenario catalogue, risk thresholds, kill-switch) as its monitoring/safeguards section. |
| 61 | Informed consent for real-world testing subjects | Subject-consent mechanism — no PerchGuard mapping (consent capture happens outside the admission pipeline). |
| 62 | Support measures for providers and deployers, in particular SMEs/start-ups | Institutional/funding — no mapping. |
| 63 | Derogations and reduced fees for SMEs and small-scale companies | Fee structure for conformity assessment — no mapping. |

---

# TITLE VII — Governance (Articles 64–70)

Establishes the EU-level institutional architecture: the **AI Office** (Commission body),
the **European Artificial Intelligence Board** (Member State representatives), an
**advisory forum** (stakeholder body), a **scientific panel of independent experts**
(systemic-risk alerts), and requires each Member State to designate **national competent
authorities** and a **single point of contact (SPOC)**.

None of these articles impose obligations on AI providers/deployers directly — they
describe *who enforces what*. The only consulting value is **Knowledge**: when a customer
asks "who would actually come knocking if something went wrong," the answer is their
national market-surveillance authority (Art. 70), which is the practical audience for any
evidence package PerchGuard helps assemble (Annex IV technical documentation, Art. 73
incident reports, Art. 26 deployer records).

| Article | Topic |
|---|---|
| 64 | AI Office (Commission body — already established by Commission Decision 24 Jan 2024) |
| 65 | European Artificial Intelligence Board — composition (one rep per Member State, max 25) |
| 66 | Tasks of the Board — advise/assist Commission and Member States on consistent application |
| 67 | Advisory forum — balanced stakeholder representation (industry, SMEs, civil society, academia) |
| 68 | Scientific panel of independent experts — supports enforcement, issues qualified alerts on systemic-risk GPAI models |
| 69 | Member States' access to the pool of scientific-panel experts |
| 70 | National competent authorities and single points of contact (SPOC) — the body a customer's incident reports (Art. 73) and registrations (Art. 49) ultimately reach |

---

# TITLE VIII — EU Database for High-Risk AI Systems (Article 71)

### Article 71 — EU database for high-risk AI systems listed in Annex III

The Commission, with Member States, maintains a public EU database. Providers/ARs enter
Annex VIII §A/B data (system description, conformity assessment, instructions for use);
deployers who are public-authority bodies enter Annex VIII §C data per Art. 49(3)–(4). Most
of the database (registrations under Art. 49) is publicly accessible and machine-readable;
real-world-testing registrations (Art. 60) are restricted to market-surveillance
authorities and the Commission unless the provider consents to publication.

This article is the **destination** for the registration work already mapped under
Article 49 above — it doesn't add a new obligation, it describes where the Art. 49 /
Annex VIII submission ends up and its visibility rules.

- **Knowledge**: Confirm for the customer whether their registration (if any) will be
  public (Art. 49 general case) or restricted (Art. 60 real-world-testing case), and what
  personal-data minimization applies (Art. 71(5) — only names/contact details of the
  individuals with legal authority to register).
- **Code Automation**: No new mapping beyond Article 49's — `POST /agents/register`
  capturing `AgentManifest.Metadata` remains the seed data; the EU-database submission
  itself is a manual provider/deployer action outside PerchGuard's runtime scope.

---

# TITLE IX — Post-Market Monitoring, Information Sharing and Market Surveillance (Articles 72–94)

## Section 1 — Post-market monitoring

### Article 72 — Post-market monitoring by providers and post-market monitoring plan for high-risk AI systems

Providers must establish a post-market monitoring **system**, based on a documented
**plan** (part of the Annex IV technical documentation), that "actively and systematically"
collects and analyses data on the high-risk AI system's real-world performance throughout
its lifetime — to evaluate continuous compliance with the Chapter III Section 2
requirements (the same requirements behind Articles 9–15 above).

- **Knowledge**: Explain that Art. 72 is the *ongoing* counterpart to the *point-in-time*
  Annex IV documentation — it requires a living feedback loop, not a one-off filing. The
  Commission must publish a template for the post-market monitoring plan by 2 Feb 2026.
- **Report**: PerchGuard's audit store is, in substance, the raw data feed Art. 72(2)
  describes. A **post-market monitoring report** — covering decision volumes by outcome
  (`ALLOW`/`DENY`/`MUTATE`/`REVIEW`), policy-violation trends, risk-score distributions, and
  any `TerminatedEarly` sessions over a reporting period — is a direct, queryable mapping
  from the existing audit store onto the Art. 72 plan's "what did we observe" section.
- **Workflow**: Establish a recurring (e.g., monthly) export cadence from the audit store
  feeding into the customer's Art. 72 monitoring plan, with material shifts (e.g., a new
  policy-violation pattern, a spike in `RiskScoreGauge`) flagged for review against the
  Annex IV technical documentation — i.e., "did this system's real-world behavior diverge
  from what the documentation describes?"
- **Code Automation**:
  - `pkg/telemetry/metrics.go` — `DecisionsTotal{session,tool,decision}`,
    `PolicyViolationsTotal{policy_name,tool}`, `SessionRiskScore` (histogram),
    `BlockedToolsTotal{tool,reason}` are exactly the "performance data" categories Art. 72(2)
    asks for, already exposed on `/metrics` and visualized in the Phase 6 Grafana
    dashboards.
  - Proposed Phase 8 cap 8.4: `perchguard compliance export --regime=eu-ai-act
    --doc=post-market-monitoring-report --since=<date>` — aggregates the above metrics plus
    `pkg/api/audit.go` query results into the Commission's forthcoming template once
    published.

## Section 2 — Sharing of information on serious incidents

### Article 73 — Reporting of serious incidents

Providers (and, where applicable, deployers) of high-risk AI systems must report **serious
incidents** to the relevant Member State market surveillance authority. "Serious incident"
(Art. 3(49)) covers: death or serious harm to health, serious and irreversible disruption
of critical infrastructure, infringement of fundamental-rights obligations causing serious
harm, or serious harm to property/environment. Deadlines are tiered: **2 days** for
widespread infringements, **10 days** for incidents involving death, **15 days**
otherwise — measured from when the provider/deployer *becomes aware*.

- **Knowledge**: Walk the customer through the Art. 3(49) "serious incident" definition and
  the tiered deadline structure — the critical operational point being that the clock
  starts at *awareness*, which makes detection latency itself a compliance variable.
- **Report**: A "serious-incident candidate" review packet — for any session where
  `TerminatedEarly = true`, `RiskScoreGauge` crossed `RiskThresholdTerminate` (0.9, see
  `pkg/agent/risk.go`), or a `GovernanceEvent` was recorded — assembled from
  `SessionAuditRecord{Steps, Decisions, PeakRiskScore, TerminatedEarly, GovernanceEvents}`
  for human/legal review of whether the Art. 3(49) threshold is met.
- **Workflow**: PerchGuard does **not** report to authorities — that determination (does
  this event meet the Art. 3(49) bar?) and the filing itself are legal/human decisions. What
  PerchGuard provides is the **detection-to-notification SLA**: an internal alert fires the
  moment a candidate event is recorded, giving the compliance team the maximum possible
  window inside the 2/10/15-day clock to make that determination and file if required.
- **Code Automation**:
  - `pkg/agent/risk.go` (`RiskThresholdTerminate = 0.9`, `RiskAccumulator.Severity()`) and
    `SessionAuditRecord.TerminatedEarly` are the concrete trigger conditions.
  - `pkg/humanreview/webhook.go` (`Dispatcher`) — already used for Art. 14 REVIEW
    escalation — is the natural channel for a "serious-incident candidate" alert to a
    compliance officer, timestamped for the Art. 73 awareness clock.
  - `pkg/telemetry/metrics.go` `PendingReviewsGauge` and the audit ring buffer give an
    immediate operational view of how many candidate events are awaiting that
    determination at any time.

## Section 3 — Enforcement (Articles 74–84)

This block governs how **market surveillance authorities** investigate, evaluate, and act
on AI systems — including non-compliant systems (Art. 79–80, 83), compliant systems that
nonetheless present a risk (Art. 82), cross-border escalation (Art. 81), confidentiality of
information shared with authorities (Art. 78), and fundamental-rights authorities' access
to documentation (Art. 77). These are **authority-to-operator** procedures; PerchGuard's
relevance is entirely in **Article 78 (confidentiality)** and in **being ready to respond**
when one of these procedures is triggered against the customer.

| Article | Topic |
|---|---|
| 74 | Market surveillance and control of AI systems — MS authorities exercise Reg. (EU) 2019/1020 powers, incl. conditional access to source code |
| 75 | Mutual assistance — market surveillance of GPAI systems (AI Office ↔ national authorities) |
| 76 | Supervision of real-world testing (Art. 60–61) by market surveillance authorities |
| 77 | Powers of fundamental-rights authorities to request/access documentation for high-risk Annex III systems |
| 78 | Confidentiality of information/data obtained by authorities, notified bodies, etc. — protects IP, trade secrets, source code |
| 79 | National procedure for AI systems presenting a risk — evaluation, corrective action, withdrawal/recall, EU-wide notification |
| 80 | Procedure where a provider's "non-high-risk" Annex III classification is found incorrect — fines under Art. 99 if deliberate misclassification |
| 81 | Union safeguard procedure — Commission arbitration of cross-border disagreements |
| 82 | Compliant AI systems that nevertheless present a risk — corrective action despite formal compliance |
| 83 | Formal non-compliance — missing/incorrect CE marking, EU declaration of conformity, registration, AR, or technical documentation |
| 84 | Union AI testing support structures — independent technical/scientific advice to authorities |

- **Knowledge**: The throughline across Art. 79/80/82/83 is that an authority's first ask is
  always **documentation and an account of corrective action taken** — exactly what Annex
  IV technical documentation (Art. 11) and the audit trail (Art. 12) exist to provide.
- **Report / Workflow**: A "regulatory inquiry response" workflow — on receipt of an Art. 79
  evaluation request, assemble (1) current Annex IV technical documentation, (2) the
  audit-store evidence trail for the period in question via `GET /api/audit`, (3) the
  policy-reload history (`PolicyReloadTotal`, policy file version) showing what corrective
  configuration changes were made and when. Article 78's confidentiality protections mean
  this package should go through the customer's legal counsel, not be auto-submitted.
- **Code Automation**: No new mapping — this section consumes the artifacts already mapped
  under Articles 9–15 and 26 above; its only distinct requirement is **speed of assembly**,
  which is what the audit store and policy-reload history already provide.

## Section 4 — Remedies (Articles 85–87)

### Article 85 — Right to lodge a complaint with a market surveillance authority

Any natural or legal person with grounds to consider this Regulation has been infringed may
lodge a complaint with the relevant market surveillance authority. **Knowledge only** — this
is a right held by external parties (employees, customers, the public), not a customer
obligation. Relevance is indirect: it's a reason the audit trail (which can demonstrate
*what was actually decided*, independent of a complainant's characterization) is valuable
defensively.

### Article 86 — Right to explanation of individual decision-making

Any person subject to a decision **made by the deployer on the basis of the output** of a
high-risk AI system listed in Annex III (excluding point 2 — law-enforcement migration/
asylum systems), where that decision **produces legal effects or similarly significantly
affects** them in a way they consider adverse to their health, safety, or fundamental
rights, has the right to obtain from the deployer **clear and meaningful explanations of
the role of the AI system in the decision-making procedure and the main elements of the
decision taken**.

This is the natural-person-facing complement to Art. 26(10) (deployer's duty to *inform*
someone a high-risk system was used) — Art. 26(10) is the notice; Art. 86 is the follow-up
right to an explanation, on request.

- **Knowledge**: Identify whether the customer's system falls in scope — Annex III,
  excluding point 2, and producing a "legal or similarly significant" effect (credit,
  employment, benefits-eligibility, etc. — the same systems anchored under Articles 9/14
  above).
- **Report**: An **explanation packet** per affected decision, built from
  `SessionAuditRecord` / `ToolCallStep{Tool, Decision, RiskScore, Reason, PolicyHit,
  DataRefsIn, DataRefOut}` — `Reason` and `PolicyHit` map directly onto "the role of the AI
  system in the decision-making procedure," and the decision sequence onto "the main
  elements of the decision taken." PerchGuard supplies the structured decision trail; the
  customer's compliance team turns it into the plain-language explanation owed to the
  individual.
- **Workflow**: A data-subject-request handling workflow — on receipt of an Art. 86 request,
  look up the relevant session(s) by data-subject reference, retrieve the
  `ToolCallStep` chain, and hand the structured trail to the team responsible for drafting
  the explanation. **This is currently a gap**: per the Phase 5 adversarial validation
  findings (finding F2), the lineage validator's `policy_hit` field is sometimes recorded
  as `null`, which would leave a hole in the "role of the AI system" explanation for
  affected decisions traced through that path — worth flagging as a fix priority for any
  customer where Art. 86 is in scope.
- **Code Automation**:
  - `pkg/api/audit.go` `GET /api/audit?session_id=<id>` already supports per-session
    retrieval of the full decision trail.
  - Proposed Phase 8 enhancement: index audit records by `DataRefsIn`/`DataRefOut` (the
    data-subject reference) so an Art. 86 request can be resolved by *who was affected*
    rather than by session ID, which the requester won't know.

### Article 87 — Reporting of breaches and protection of reporting persons

Whistleblower protections for persons reporting infringements of this Regulation, applying
Directive (EU) 2019/1937. **Knowledge only** — an HR/legal-policy matter (whistleblower
channel must cover AI Act infringements), no PerchGuard mapping.

## Section 5 — Supervision of providers of general-purpose AI models (Articles 88–94)

Parallel to Title V (Articles 51–56): this section governs the **AI Office's** powers to
monitor, investigate, and request action from **GPAI model providers** — the customer's
model vendor, not the customer. As with Title V, this is **Knowledge** territory.

| Article | Topic | PerchGuard relevance |
|---|---|---|
| 88 | Enforcement of GPAI provider obligations — AI Office has exclusive powers to monitor/enforce Title V | Knowledge only |
| 89 | Monitoring actions — AI Office may require GPAI providers to participate in evaluations | Knowledge only |
| 90 | Alerts of systemic risks by the scientific panel to the AI Office | Knowledge only |
| 91 | AI Office's power to request documentation/information from GPAI providers | Knowledge only |
| 92 | AI Office's power to conduct model evaluations, incl. requiring access | Knowledge only |
| 93 | AI Office's power to request mitigation measures from GPAI providers w/ systemic risk | Knowledge only |
| 94 | Procedural rights of GPAI economic operators (right to be heard, etc.) | Knowledge only |

**The recurring pattern across both Title V and this section**: it tells the customer what
leverage the EU has over their *model vendor*, which is useful context but generates no
direct deliverable. The one bridge back to PerchGuard's actual surface remains Article 25
(provider/deployer role-shift) — covered above.

---

# TITLE X — Codes of Conduct and Guidelines (Articles 95–96)

### Article 95 — Codes of conduct for voluntary application of specific requirements

The AI Office and Member States shall encourage codes of conduct under which providers and
deployers of **non-high-risk** AI systems voluntarily apply some or all of the Chapter III
Section 2 requirements (the Articles 8–15 requirements anchored above) — plus broader
commitments on environmental sustainability, AI literacy, accessibility, and protection of
vulnerable groups.

- **Knowledge**: This article describes, almost exactly, what PerchGuard customers
  routinely choose to do already — apply admission-control governance (risk thresholds,
  human review, audit trails) to agents that don't meet the Annex III high-risk bar,
  because the controls are valuable independent of legal obligation. Frame this as "you're
  ahead of Article 95, not behind it" for customers whose systems fall outside Annex III.
- **Workflow**: For a customer wanting a documented voluntary code, PerchGuard's policy
  profile (`configs/profiles/<vertical>.yaml`) — with its `alwaysReview` rules, behavior
  patterns, and audit redaction fields — *is* the operational expression of such a code;
  the profile file itself can be attached as evidence of the commitment.

### Article 96 — Guidelines from the Commission on implementation of this Regulation

The Commission will issue practical-implementation guidance on, among other things,
Articles 8–15 and 25 (high-risk requirements and the provider/deployer role-shift),
Article 5 (prohibited practices), substantial-modification rules, and Article 50
(transparency). **Knowledge only** — these guidelines, once published, are the kind of
source Cap 8.5's regulatory-text-ingestion pipeline (see Article 50/Title V notes above)
should track alongside the Act's article text itself, since they will materially affect how
articles like 25 and 50 get applied in practice.

---

# TITLE XI — Delegation of Power and Committee Procedure (Articles 97–98)

Articles 97 (delegated-act procedure for the Commission) and 98 (comitology / examination
procedure for implementing acts) are pure EU legislative-process mechanics — how the
Commission is empowered to update Annexes and adopt implementing acts referenced
throughout this document (e.g., Art. 6(6)–(7) Annex III amendments, Art. 11(3) Annex IV
updates, Art. 53(5)–(6) Annex XI/XII updates). No PerchGuard mapping; listed for
completeness.

---

# TITLE XII — Penalties (Articles 99–101)

This is the **headline "what's at stake" table** — the financial-exposure framing that
motivates the rest of this document. Three fine regimes apply depending on actor and
violation type.

### Article 99 — Penalties (providers, deployers, and other operators)

| Violation | Maximum fine |
|---|---|
| Art. 5 — prohibited AI practices | **EUR 35,000,000** or **7%** of total worldwide annual turnover (preceding financial year), whichever is higher |
| Art. 16 (provider obligations), Art. 22 (AR), Art. 23 (importer), Art. 24 (distributor), **Art. 26 (deployer obligations)**, Art. 31/33/34 (notified bodies), **Art. 50 (transparency)** | **EUR 15,000,000** or **3%** of turnover, whichever is higher |
| Supplying incorrect/incomplete/misleading information to notified bodies or competent authorities | **EUR 7,500,000** or **1%** of turnover, whichever is higher |

For SMEs (including start-ups), each cap applies as **whichever of the percentage or the
fixed amount is *lower*** (para 6) — the inverse of the "whichever is higher" rule for
larger undertakings. Fining decisions must weigh factors including: nature/gravity/duration
of the infringement, number of affected persons, degree of cooperation with authorities,
intentional vs. negligent character, and mitigating action taken (para 7) — several of
which (cooperation, mitigating action, awareness-to-notification timing) are exactly the
things a fast, complete audit trail demonstrates favorably.

### Article 100 — Administrative fines on EU institutions, bodies, offices and agencies

Parallel regime for EU institutions themselves, enforced by the EDPS: up to **EUR
1,500,000** for Art. 5 violations, up to **EUR 750,000** for other violations. Knowledge
only — applies to EU public-sector bodies, not typical PerchGuard customers, though
relevant if the customer *is* an EU institution.

### Article 101 — Fines for providers of general-purpose AI models

Commission-imposed fines on **GPAI model providers** (the customer's model vendor, not the
customer) of up to **3%** of worldwide annual turnover or **EUR 15,000,000**, whichever
higher, for infringing Title V obligations or obstructing Commission evaluations. Knowledge
only — see Title V.

- **Knowledge**: Walk the customer through the Article 99 table mapped to *their own*
  obligations as anchored elsewhere in this document — e.g., "your Article 26 deployer
  obligations (Art. 19/20 logging, Art. 26(2) human-oversight assignment, Art. 26(5)
  fundamental-rights impact assessment) sit in the EUR 15M / 3% tier; your Article 50
  transparency obligations sit in the same tier."
- **Report**: A "fine-exposure map" — one row per article this document has anchored to a
  PerchGuard capability, cross-referenced to its Article 99 tier, framed not as "this is
  your fine" but as "this is the regulatory weight behind the evidence artifact described
  above."
- **Workflow / Code Automation**: None new — this title is the *motivation* for the
  artifacts already mapped under Articles 5, 9, 12, 14, 19, 20, 26, and 50, not a source of
  new deliverables itself.

---

# TITLE XIII — Final Provisions (Articles 102–113)

### Articles 102–110 — Sectoral legislation amendments and collective redress

Articles 102–109 amend eight pieces of sectoral product-safety legislation (aviation
security, agricultural/forestry vehicles, two/three-wheel vehicles, marine equipment, rail
interoperability, motor-vehicle type-approval, civil aviation safety, general motor-vehicle
safety) to fold AI Act conformity assessment into those sectors' existing type-approval
processes. Article 110 adds this Regulation to the list of Union laws covered by Directive
(EU) 2020/1828 (collective redress / representative actions for consumers) — meaning
infringements can be the subject of EU consumer collective-action lawsuits.

No PerchGuard mapping. Listed for completeness — relevant only to providers in those eight
regulated sectors, who would already be working with sector-specific notified bodies.

### Article 111 — Transitional provisions for AI systems already on the market

Three timelines for systems that predate the Act's application:

- High-risk AI systems embedded in the large-scale EU IT systems listed in Annex X
  (e.g., Schengen Information System, Eurodac): comply by **31 December 2030**.
- Other high-risk AI systems already placed on the market/in service before **2 August
  2026**: only need to comply if subject to "significant changes in their design" after
  that date — **except** systems used by public authorities, which must comply by
  **2 August 2030** regardless.
- GPAI models already placed on the market before **2 August 2025**: comply with Title V
  by **2 August 2027**.

- **Knowledge**: For customers with AI systems already in production, this article
  determines whether the rest of this document is "comply now" or "comply if you change
  the system / by 2030 regardless." A "grandfathering assessment" — does the system predate
  the relevant date, and has it had a "significant change in design" since — is a
  prerequisite Knowledge step before scoping any other engagement from this document.

### Article 112 — Evaluation and review

The Commission commits to recurring reviews: annually re-assessing the **Annex III
high-risk list** and the **Article 5 prohibited-practices list**; by 2 August 2028 and
every 4 years, evaluating extensions to Annex III, the **Article 50 transparency-systems
list**, and supervision/governance effectiveness; by 2 August 2029 and every 4 years, a
full Regulation review including whether enforcement needs a dedicated EU agency.

- **Knowledge**: The practical implication is that **Annex III, Article 5, and Article 50
  are living lists** — a customer's classification under this document is a snapshot, not
  a permanent determination. This is the regulatory-side argument for Cap 8.5's
  regulatory-text-ingestion pipeline (§7 of `PHASE8-AGENTIC-COMPLIANCE.md`): the policy
  profile's risk classification should be re-checked against these lists on each periodic
  review cycle, not authored once and forgotten.

### Article 113 — Entry into force and application

**The master compliance timeline.** The Regulation entered into force 1 August 2024 (20
days after OJ publication on 12 July 2024) and applies generally from **2 August 2026**,
with three carve-outs:

| Date | What applies |
|---|---|
| **2 February 2025** | Chapters I–II — general provisions (Art. 1–4) and **prohibited AI practices (Art. 5)** |
| **2 August 2025** | Chapter III Section 4 (notified bodies, Art. 28–39), **Title V (GPAI models, Art. 51–56)**, **Title VII (governance, Art. 64–70)**, **Title XII (penalties, Art. 99–101 — except Art. 101 itself)**, and **Article 78 (confidentiality)** |
| **2 August 2026** | Everything else — including the bulk of Title III (high-risk system requirements, Art. 8–27, 40–49) and **Article 50 (transparency)** |
| **2 August 2027** | **Article 6(1)** and its corresponding obligations (the "Annex I product-safety route" to high-risk classification — narrower than the already-applicable Annex III route) |

- **Knowledge**: This table **is** the consulting engagement's prioritization driver. As of
  the current date, Article 5 (DENY-mapped prohibited practices) has been in force since
  **2 February 2025** — any customer not yet covering Art. 5 prohibited-practice scenarios
  in policy is already out of compliance with an in-force obligation, independent of
  whether their system is "high-risk." The Title III high-risk requirements (Articles
  8–15, 19, 20, 26, 27, 47, 49 anchored throughout this document) become generally
  applicable **2 August 2026** — the date that should anchor any engagement timeline. GPAI
  provider obligations (Title V) and the governance bodies (Title VII) have applied since
  **2 August 2025**.
- **Report**: A "compliance readiness timeline" — for each article anchored in this
  document, map its Art. 113 applicability date against the customer's current policy
  profile coverage, producing a simple "in force now / in force by 2 Aug 2026 / not yet
  applicable" status per obligation.

---

# ANNEXES

### Annex I — List of Union harmonisation legislation

Two lists of sectoral product-safety laws (machinery, toys, lifts, radio equipment, medical
devices, vehicles, aviation, etc.) that define the "Annex I product-safety route" to
high-risk classification under Art. 6(1) — the route that becomes applicable **2 August
2027** (Art. 113(c)). No PerchGuard mapping; relevant only to providers of AI systems that
are themselves safety components of Annex-I-regulated products. Listed for completeness.

### Annex II — List of criminal offences referred to in Article 5(1), point (h)(iii)

The 16-offence list (terrorism, trafficking, sexual exploitation of children, drug
trafficking, murder, etc.) that defines the narrow law-enforcement exception to the
real-time remote biometric identification prohibition in Art. 5(1)(h). **Knowledge only** —
relevant solely to scoping the Art. 5(1)(h) exception discussed under Article 5 above; no
independent deliverable.

---

### Annex III — High-risk AI systems referred to in Article 6(2)

**This is the classification anchor for the entire document.** Annex III lists eight areas;
an AI system intended for any listed use is high-risk under Art. 6(2) (subject to the Art.
6(3) "non-significant-risk" carve-out covered above). Per `PHASE8-AGENTIC-COMPLIANCE.md`
§7.2, Annex III maps to **risk classification tags on admission decisions**.

| # | Area | Examples |
|---|---|---|
| 1 | **Biometrics** (where permitted under Union/national law) | (a) remote biometric identification (excludes pure 1:1 verification); (b) biometric categorisation by sensitive/protected attributes; (c) emotion recognition |
| 2 | **Critical infrastructure** | Safety components managing/operating critical digital infrastructure, road traffic, water/gas/heating/electricity supply |
| 3 | **Education and vocational training** | Admission/access decisions; evaluating learning outcomes; assessing appropriate education level; monitoring prohibited behaviour during tests |
| 4 | **Employment, worker management, self-employment access** | (a) recruitment/selection — targeted job ads, filtering applications, evaluating candidates; (b) decisions on promotion, termination, task allocation, performance/behaviour monitoring |
| 5 | **Access to essential private/public services and benefits** | (a) eligibility for public assistance benefits/healthcare; (b) creditworthiness/credit scoring (excl. fraud detection); (c) life/health insurance risk assessment and pricing; (d) emergency call evaluation/dispatch, patient triage |
| 6 | **Law enforcement** (where permitted) | Victimisation-risk assessment; polygraph-type tools; evidence-reliability evaluation; (re-)offending risk assessment; profiling under Directive (EU) 2016/680 |
| 7 | **Migration, asylum, border control** (where permitted) | Polygraph-type tools; risk assessment (security/irregular-migration/health) of persons entering/in a Member State; assistance examining asylum/visa/residence applications; detection/recognition/identification of persons (excl. travel-document verification) |
| 8 | **Administration of justice and democratic processes** | (a) assisting judicial authorities in researching/interpreting facts and law, or in alternative dispute resolution; (b) influencing election/referendum outcomes or voting behaviour (excludes tools not directly exposed to natural persons, e.g., campaign-logistics tools) |

This is the **first** deliverable in any engagement — everything else in this document is
downstream of "which Annex III area(s), if any, does this agent's intended purpose fall
into?"

- **Knowledge**: An Annex III classification interview, walked through table-row by
  table-row against the agent's `AgentManifest.Mission.Summary` and `Mission.Scope`
  (`pkg/manifest/manifest.go`) — does the agent's intended purpose match any of the eight
  areas? Each "yes" should also be checked against the Art. 6(3) non-significant-risk
  carve-out (covered under Article 6 above) before concluding the system is high-risk.
- **Report**: A short **classification memo** — area(s) matched (or "none matched, system is
  not high-risk under Annex III"), the specific sub-point, and the rationale — which becomes
  the seed of Annex IV §1 ("a general description of the AI system including its intended
  purpose").
- **Workflow**: Classification is not one-and-done — Article 25 (provider/deployer
  role-shift) and Article 111(2) ("significant changes in design") both re-trigger this
  questionnaire. The workflow is: classify at agent registration → re-classify whenever the
  Article 25 purpose-drift report (above) flags a material change to `AgentManifest.Mission`.
- **Code Automation**: Proposed Phase 8 policy-profile field `compliance.euAiActRiskTags`
  (referenced in `PHASE8-AGENTIC-COMPLIANCE.md` as a planned addition, not yet built) —
  e.g., `["annex_iii_4a", "annex_iii_5b"]` — attached to the agent's policy profile and
  carried onto every `SessionAuditRecord` / `ToolCallStep` so admission decisions and audit
  records can be filtered/reported by Annex III classification. Seed value comes from
  `pkg/manifest/manifest.go` `AgentManifest.Mission.Scope`.

---

### Annex IV — Technical documentation referred to in Article 11(1)

**This is the export-schema anchor for the entire document.** Annex IV defines the 9-point
structure of the technical documentation a provider must draw up under Article 11. Per
`PHASE8-AGENTIC-COMPLIANCE.md` §7.2, Annex IV maps to the **compliance report export
schema** — specifically, Phase 8 cap 8.2's "Annex IV technical documentation export from
audit store."

| § | Annex IV requirement | PerchGuard coverage |
|---|---|---|
| 1 | General description: intended purpose, provider/version, system interactions, hardware, deployment forms, UI description, instructions for use | **Customer-authored.** Seed data: `AgentManifest.Metadata` and `Mission.Summary` (name, version, intended purpose). The rest (hardware, deployment form, UI) is outside PerchGuard's runtime scope. |
| 2(a)–(d), (f), (h) | Development process: methods/steps, third-party pre-trained components, design specifications, architecture, training-data datasheets, pre-determined changes, cybersecurity measures | **Customer-authored**, with one exception: 2(a)'s "recourse to pre-trained systems or tools provided by third parties" is where the Title V / Annex XII "request from your model vendor" checklist (Article 53 above) feeds in. |
| 2(e) | **Assessment of Article 14 human-oversight measures**, incl. technical measures to facilitate deployer interpretation of outputs | **PerchGuard-generated.** This is the Article 14 mapping above: REVIEW-decision configuration (`alwaysReview` rules), `pkg/humanreview/webhook.go` Dispatcher records, and the structured `ToolCallStep.Reason`/`PolicyHit` fields *are* the "technical measures to facilitate interpretation." |
| 2(g) | Validation/testing procedures, accuracy/robustness/discrimination metrics, **test logs and reports** | **Partially PerchGuard-generated.** The Phase 5 adversarial-validation findings (`PHASE5-ADVERSARIAL-VALIDATION.md`, 15/18 pass) and `cmd/redteam-agent/` scenario runs are exactly "test logs and reports" for the *governance-control* dimension — they don't substitute for model-accuracy/discrimination metrics, which remain customer/vendor-authored. |
| 3 | Monitoring/functioning/control: capabilities/limitations, accuracy by population, foreseeable unintended outcomes, **Art. 14 oversight measures**, input-data specs | **Partially PerchGuard-generated** — the Art. 14 oversight portion overlaps with 2(e) above; "foreseeable unintended outcomes" can draw on the Phase 5 findings (e.g., F1: semantic firewall gap on SSN exfiltration) as a documented, evidenced limitation. |
| 4 | Appropriateness of performance metrics | **Customer-authored** — model-quality territory, outside PerchGuard's scope. |
| 5 | **Detailed description of the risk management system per Article 9** | **PerchGuard-generated.** This is the Article 9 mapping above — policy profile structure (`policies.admission`, `policies.humanReview`, `policies.audit` in `configs/profiles/<vertical>.yaml`) plus `pkg/agent/risk.go` thresholds (`RiskThresholdHumanReview = 0.7`, `RiskThresholdTerminate = 0.9`) directly document the risk management system's structure and operation. |
| 6 | Description of relevant changes through the system's lifecycle | **Partially PerchGuard-generated** — `PolicyReloadTotal` and policy-file version history document *governance-layer* changes; model/system changes remain customer-authored. |
| 7 | Harmonised standards applied / alternative solutions adopted | **Customer-authored** — standards-conformity territory (Articles 40–46 above), outside PerchGuard's scope. |
| 8 | **Copy of the EU declaration of conformity (Article 47)** | **Customer-authored, requested via the Article 47 mapping above** — PerchGuard doesn't produce this but the export package has a slot for it. |
| 9 | **Description of the post-market monitoring system per Article 72**, including the monitoring plan | **PerchGuard-generated.** This is the Article 72 mapping above — `perchguard_decisions_total`, `perchguard_policy_violations_total`, `perchguard_risk_score`, `perchguard_blocked_tools_total` and the audit-store query surface constitute the monitoring *system*; the recurring export cadence constitutes the monitoring *plan*. |

- **Knowledge**: Walk the customer through this table — three of nine sections (§5, §9, and
  the Article-14 portion of §2(e)/§3) are **directly generated from PerchGuard's existing
  audit/policy/telemetry surface**; the rest require customer- or vendor-authored content
  that PerchGuard doesn't replace, but for which this document identifies the right
  upstream source (Title V/Annex XII for §2(a), Phase 5 for §2(g)/§3).
- **Report**: The flagship deliverable — an **Annex IV technical documentation package**,
  structured per this 9-point schema, with §5/§9/§2(e) auto-populated and the remaining
  sections present as clearly-labeled "customer to complete" placeholders referencing the
  upstream sources identified above.
- **Workflow**: A documentation-assembly workflow — PerchGuard generates its sections on
  demand (or on a recurring cadence per Article 72's monitoring plan), the customer's
  technical/legal team fills the remaining sections, and the combined package is the
  artifact retained for Article 11(1) ("technical documentation shall be drawn up before
  [the system] is placed on the market or put into service and shall be kept up-to date").
- **Code Automation**: Proposed Phase 8 cap 8.2 — `perchguard compliance export
  --regime=eu-ai-act --doc=annex-iv` (not yet built). Composition:
  - §5 ← policy profile (`configs/profiles/<vertical>.yaml` `policies.admission` /
    `policies.humanReview`) + `pkg/agent/risk.go` thresholds.
  - §9 ← `pkg/telemetry/metrics.go` decision/violation/risk-score counters +
    `pkg/api/audit.go` query results over the reporting period.
  - §2(e)/§3 (Art. 14 portion) ← `pkg/review/store.go` `PendingReview`/`Status` records +
    `ToolCallStep.Reason`/`PolicyHit`.
  - §2(g)/§3 (testing evidence) ← Phase 5 red-team findings (`cmd/redteam-agent/` output,
    `PHASE5-ADVERSARIAL-VALIDATION.md`).
  - §1, §2(a)(b)(c)(d)(f)(h), §4, §6 (model-change portion), §7, §8 ← templated
    placeholders with upstream-source pointers.

#### Annex IV export preview (`annex-iv` export)

| § | Annex IV Requirement (Art. 11(1)) | PerchGuard Deliverable | Status |
|---|---|---|---|
| 1 | General description: intended purpose, provider/version, system interactions, hardware, deployment form, UI, instructions for use | Seed from `AgentManifest.Metadata` / `Mission.Summary` (name, version, purpose); hardware/UI/deployment outside runtime scope | **placeholder** (customer-authored, partial seed) |
| 2(a)–(d),(f),(h) | Development process, third-party pre-trained components, design specs, architecture, training-data datasheets, planned changes, cybersecurity measures | Customer-authored; 2(a) third-party model checklist feeds from Title V/Annex XII (Art. 53) | **placeholder** |
| 2(e) | Assessment of Art. 14 human-oversight measures + technical measures for deployer interpretation | `alwaysReview` config + `pkg/humanreview/webhook.go` Dispatcher records + `ToolCallStep.Reason`/`PolicyHit` fields | **generated** |
| 2(g) | Validation/testing procedures, accuracy/robustness/discrimination metrics, test logs & reports | Phase 5 adversarial findings (15/18 pass) + `cmd/redteam-agent/` runs — governance-control dimension only, not model-accuracy metrics | **partial** (governance evidence only) |
| 3 | Monitoring/control: capabilities/limitations, accuracy by population, foreseeable unintended outcomes, Art. 14 oversight, input-data specs | Overlaps 2(e) for oversight; Phase 5 findings (e.g. F1 semantic-firewall SSN gap) as documented limitation | **partial** |
| 4 | Appropriateness of performance metrics | Model-quality territory, outside scope | **placeholder** |
| 5 | Detailed description of risk management system (Art. 9) | `policies.admission`/`policies.humanReview`/`policies.audit` (policy profile) + `pkg/agent/risk.go` thresholds (`RiskThresholdHumanReview=0.7`, `RiskThresholdTerminate=0.9`) | **generated** |
| 6 | Lifecycle change description | `PolicyReloadTotal` + policy-file version history (governance-layer only; model/system changes customer-authored) | **partial** |
| 7 | Harmonised standards applied / alternatives | Standards-conformity territory (Art. 40-46), outside scope | **placeholder** |
| 8 | Copy of EU declaration of conformity (Art. 47) | Customer-authored; export package has a slot for it | **placeholder** |
| 9 | Description of post-market monitoring system (Art. 72) + monitoring plan | `perchguard_decisions_total`, `perchguard_policy_violations_total`, `perchguard_risk_score`, `perchguard_blocked_tools_total` + audit-store queries; recurring export cadence = the plan | **generated** |

3 of 9 sections (§5, §9, Art. 14 portion of §2(e)/§3) ship `generated` from existing
audit/policy/telemetry — the cap 8.2 first increment. §2(g)/§3/§6 ship `partial` (governance
evidence only). §1, §2(a-d,f,h), §4, §7, §8 ship as labeled `placeholder` blocks with
upstream-source pointers.

---

### Annex V — EU declaration of conformity

The required content list for the Article 47 declaration (system identification, provider
details, statement of sole responsibility, confirmation of conformity, references to
harmonised standards/common specifications, notified-body details where applicable,
signature). **No new mapping** — this is the content checklist for the document already
covered under Article 47 (Knowledge: request/retain from provider) and slotted into Annex
IV §8 above.

### Annexes VI & VII — Conformity assessment procedures

- **Annex VI** (assessment based on internal control): the provider self-verifies its QMS
  against Article 17, examines the technical documentation, and confirms the design,
  development, and post-market monitoring system (Article 72) are consistent with that
  documentation.
- **Annex VII** (assessment based on QMS + technical documentation, involving a notified
  body): a notified body audits the QMS (Section A) and assesses the technical
  documentation (Section B), with ongoing surveillance (Section C).

Both are **provider/notified-body procedures** — see TITLE III Chapter 4/5 above ("listed
for completeness; no Knowledge/Report/Workflow/Code Automation entries"). The only
indirect relevance: Annex VI's internal-control check that "the post-market monitoring
system... [is] consistent with the technical documentation" is exactly the kind of
consistency check the Article 72 / Annex IV §9 export (above) is designed to keep true on
an ongoing basis — but the conformity-assessment act itself is outside PerchGuard's scope.

---

### Annex VIII — Registration information (Article 49) — recap and Section C detail

Already mapped in full under **Article 49** and **Article 71** above. Restating the
**Section C** detail here because it's the deployer-facing (not provider-facing) portion,
and it directly cross-references two FULL anchors elsewhere in this document:

Section C (submitted by deployers under Art. 49(3)) requires: deployer name/contact;
submitter name/contact; the URL of the provider's EU-database entry for the system; **a
summary of the Article 27 fundamental-rights impact assessment findings**; and, where
applicable, a summary of the Article 35 GDPR / Article 27 LED data-protection impact
assessment (per Art. 26(8)).

- **Code Automation**: This is the concrete tie-point between the **Article 27 FRIA**
  deliverable (full Knowledge/Report/Workflow/Code Automation breakdown above) and **Article
  49 registration** (also fully mapped above) — the FRIA *summary* produced under Article
  27 is literally Section C item 4 of this Annex. No separate work — it's a re-statement of
  output already designed under Article 27, surfaced here so the cross-reference (deferred
  from the Article 26 table to "see Annex VIII below") is explicit.

### Annex IX — Real-world testing registration (Article 60)

Registration data for systems undergoing real-world testing outside a sandbox (testing
plan, sites, duration, participant numbers, safeguards). **Knowledge only** — cross-refers
to the **Article 60 / Phase 5 red-team lab** parallel discussed under Title VI above. If a
customer pursues formal Art. 60 real-world testing, this Annex is the registration-content
checklist for that testing plan; PerchGuard doesn't generate the registration itself.

### Annex X — Union legislative acts on large-scale IT systems (Freedom, Security and Justice)

Lists the EU large-scale IT systems (Schengen Information System, Visa Information System,
Eurodac, Entry/Exit System, ETIAS, ECRIS-TCN, Interoperability Regulations) referenced by
the Article 111(1) transitional timeline (compliance by 31 Dec 2030). **No PerchGuard
mapping** — relevant only to providers building components of these specific EU systems.
Listed for completeness.

### Annexes XI, XII & XIII — General-purpose AI model documentation and systemic-risk criteria

All three extend **Title V** (Articles 51–56), which is "your model vendor's homework, not
yours" territory established above:

- **Annex XI** (Art. 53(1)(a)): the technical documentation a GPAI provider must maintain
  for the AI Office — model architecture, training data sources, compute, energy
  consumption, evaluation results.
- **Annex XII** (Art. 53(1)(b)): the **transparency information a GPAI provider must give
  downstream providers** integrating the model — capabilities, limitations, intended uses,
  and information sufficient for the downstream provider to meet *their own* Chapter III
  Section 2 obligations (Articles 8–15).
- **Annex XIII**: the criteria (compute, user reach, training methods, capability
  benchmarks) the Commission uses to designate a GPAI model as carrying "systemic risk"
  under Article 51.

- **Knowledge**: **Annex XII is the actionable one** — it is precisely the "request this
  from your model vendor" checklist flagged under Article 53 above. When assembling Annex
  IV §2(a) ("recourse to pre-trained systems or tools provided by third parties"), Annex
  XII tells the customer exactly what their model vendor (OpenAI, Anthropic, etc.) is
  obligated to have made available to them — useful for identifying gaps in vendor
  documentation early in an engagement.

---

# Consulting Engagement Map — Pulling It Together

The article-by-article structure above is the reference. In an actual engagement, the
deliverables group into five phases. This table is the "menu" — cross-referencing back to
the anchor articles (★ = full Knowledge/Report/Workflow/Code Automation breakdown above)
and to `PHASE8-AGENTIC-COMPLIANCE.md` / `SALES-MARKETING-AGENTIC-COMPLIANCE.md` for the
broader product/GTM framing.

| Phase | Question being answered | Anchor articles | Primary deliverable types |
|---|---|---|---|
| **1. Scoping & Classification** | Is this agent in scope, and for what? | Art. 5 (prohibited practices) ★, Art. 6 / **Annex III** (high-risk classification) ★, Art. 25 (role-shift), Art. 50 (disclosure scope) ★ | Knowledge, Report (classification memo) |
| **2. Controls Design** | What does the policy profile need to enforce? | Art. 9 (risk management) ★, Art. 14 (human oversight) ★ — *the* strongest mapping per `PHASE8-AGENTIC-COMPLIANCE.md` §7.2, Art. 10 (data governance), Art. 15 (accuracy/robustness/cybersecurity) | Workflow (policy profile design), Code Automation (`configs/profiles/<vertical>.yaml`, `pkg/agent/risk.go` thresholds) |
| **3. Operating & Evidence** | What's the running record? | Art. 12 (record-keeping/logs) ★, Art. 19/20 (logging & corrective action), **Art. 26 (deployer obligations)** ★ FULL, **Art. 27 (FRIA)** ★ FULL, Art. 72 (post-market monitoring), Art. 73 (serious incidents), Art. 86 (right to explanation) | Code Automation (`pkg/audit/sink.go`, `/api/audit`, telemetry), Report (recurring exports) |
| **4. Documentation Package** | What do we hand to an auditor/regulator? | **Annex IV** (technical documentation) ★ — the export-schema anchor, Art. 47 (EU declaration of conformity), Art. 49 / **Annex VIII** (registration) | Report (flagship Annex IV export), Code Automation (Phase 8 cap 8.2/8.4, not yet built) |
| **5. Ongoing Governance** | How does this stay current? | Art. 99–101 (fine-exposure framing), Art. 112/113 (review cycles & timeline) ★, Title V/Annex XII (model-vendor checklist) | Knowledge (re-classification triggers), Report (compliance readiness timeline) |

**Reminder of the framing this entire document operates under** (from
`PHASE8-AGENTIC-COMPLIANCE.md` §4 / `SALES-MARKETING-AGENTIC-COMPLIANCE.md` §7):

> PerchGuard produces **evidence that maps onto** a regime's requirements. It does not
> certify compliance, replace legal review, or guarantee a regulator's acceptance.
> Say "here is the evidence trail," never "this makes you compliant."

**What this document is not**: a substitute for `pkg/compliance/{ingestion,cache,euaiact}/`
(Phase 8 cap 8.5, not yet built) — the regulatory-text-ingestion pipeline that would keep
article text and the Annex III/Article 5/Article 50 "living lists" (Article 112) current
automatically. This document is the **manually-authored v1 mapping** that cap 8.5 would
eventually keep in sync with EUR-Lex. Until then, re-validate article numbers/text against
CELEX `32024R1689` before using this document in a customer-facing engagement, and re-check
Article 112's annual review outputs for changes to Annex III, Article 5, or Article 50.
