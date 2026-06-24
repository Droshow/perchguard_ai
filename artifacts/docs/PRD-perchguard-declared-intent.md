---
prd_id: prd-20260420-120000
version: v1
owner: droshow@perchguard.io
stakeholders:
  - engineering: droshow@perchguard.io
  - product: droshow@perchguard.io
created_at: 2026-04-20T00:00:00Z
project_name: "PerchGuard: Declared-Intent Governed AI Sessions"
---

# PRD: PerchGuard — Declared-Intent Governed AI Sessions

## 1. Problem Statement

**The Problem:**
Enterprises are deploying AI agents to serve business users — insurance brokers, procurement officers, CRM teams — who have no awareness of, or interaction with, the governance layer running underneath them. Those agents start every session with no declared mission: the governance layer infers intent from observed behaviour after the fact, the project record contains no trace of safety decisions made on the agent's behalf, and there is no single auditable answer to the question "what was this agent authorized to do, and did it stay within those bounds?"

**Supporting Evidence:**
- PerchGuard's SessionAgent bootstraps its intent baseline from the first tool call it observes. The first 5–10 calls of every session are the period of highest uncertainty and highest false-positive risk — because governance has no prior context about what business workflow the agent is serving.
- Project context snapshots capture what was built and decided but not whether the AI agent acting in that session was flagged, modified, or blocked. A HUMAN_REVIEW or DENY event is currently invisible to the project record.
- Security and compliance teams have no single place to answer: "What was this agent's declared mission, who authorized it, and did it behave within those bounds?" — a question that will be asked by regulators, insurers, and enterprise security functions within the next 12–18 months.

**Why Now:**
Enterprises are moving AI agents from pilot to production in 2026 — not in developer toolchains but in core business workflows. The business users those agents serve (brokers, analysts, procurement officers) have no visibility into agent behaviour and no expectation of managing it. The enterprises deploying those agents are about to discover they have no auditable answer to basic compliance questions. The window to establish intent-declared, mission-governed agents as the standard — before ad-hoc point solutions calcify — is now.

---

## 2. Target Users

**Primary — Agent System Architect (the factory setter):**
When I am configuring and deploying an AI agent for a business workflow, I want to declare the agent's mission, constraints, and authorized scope in a single structured artefact, so that the governance layer enforces exactly what I intended — without requiring the business users the agent serves to understand or interact with governance at all.

**Secondary — Security & Compliance Reviewer:**
When I am auditing an enterprise AI deployment, I want a single record that shows what each agent was authorized to do, who declared that mission, and whether runtime behaviour stayed within those bounds — so I can satisfy compliance requirements without reconstructing context from scattered logs across separate systems.

**Implicit / Protected — End User (insurance broker, CRM analyst, procurement officer):**
The end user is not a buyer or operator of governance tooling. They interact only with the agent built for their workflow. Their context — their role, their domain, their typical actions — shapes what "normal" looks like for that agent's mission, and therefore what constitutes drift. They are protected by the governance layer, not responsible for configuring it.

---

## 3. Desired Outcome

**BEFORE:**
A developer starts a session. PerchGuard has no idea what the session is for — it watches the first tool call and begins inferring intent from scratch. Early calls trigger false positives, interrupting flow. When the session ends, the governance layer's decisions — every DENY, MUTATE, HUMAN_REVIEW — evaporate silently. The project record and the safety record are separate universes.

**AFTER:**
A developer starts a session. PerchGuard reads the project's current PRD and recent snapshots from the local context store before the first tool call lands — it already knows the session's purpose, the constraints in play, and what the last sessions accomplished. Intent drift detection is accurate from call one. When governance events occur, they are written into the project's snapshot store as first-class audit entries. When the session closes, the project record contains both what was built and a full account of how the agent behaved. Intent and governance are one coherent story.

**Success Metrics:**
- False-positive governance interruptions in the first 10 calls of a session drop by ≥ 60% compared to cold-start baseline.
- 100% of HUMAN_REVIEW and TERMINATE events appear in the project's governance snapshot within the session window.
- A security reviewer can reconstruct the full intent-and-behaviour history of any past AI session from the audit sink alone, without accessing PerchGuard logs directly.

---

## 4. User Stories

_Prioritized. Must-haves ship in v1._

**Must-Have (v1):**
1. As an agent system architect, I want to declare an agent's mission, authorized scope, and domain constraints before deployment, so that PerchGuard enforces governance against that declaration from the first action the agent takes.
2. As an agent system architect, I want governance events (DENY, MUTATE, HUMAN_REVIEW, TERMINATE) to be recorded in the project snapshot automatically, so that every deployed agent has an auditable runtime record tied to its declared mission — without any action required from the business users the agent serves.
3. As a security reviewer, I want to open any past governance snapshot and see both the agent's declared mission and its full governance record for that session, so I can verify compliance without accessing separate systems or reconstructing context manually.

**Should-Have:**
1. As an agent system architect, I want PerchGuard's risk thresholds for an agent to be derived from the ADR constraints I declared for its domain, so that an agent configured for financial transactions is held to tighter standards than one configured for internal knowledge retrieval — without manual policy tuning.
2. As a security reviewer, I want to query all sessions across a deployed agent's history in which TERMINATE or HUMAN_REVIEW events occurred, so I can identify behavioural patterns and refine the agent's mission declaration accordingly.
3. As an agent system architect, I want PerchGuard to flag when an agent's tool calls are drifting from the active ADR's declared scope — not just from abstract intent — so that governance is grounded in the actual business constraints I specified.

**Nice-to-Have:**
1. As an agent system architect, I want PerchGuard to auto-generate a draft policy from a new PRD/ADR, so that governance policy stays in sync with mission declarations without manual maintenance.
2. As a security reviewer, I want governance events exportable from the audit sink as a structured compliance report (PCI-DSS, SOC2, HIPAA), so I can satisfy audit requirements without custom tooling.
3. As an agent system architect, I want a single dashboard view showing each deployed agent's current mission declaration, live risk score, and recent governance events, so I can monitor the fleet I have configured without switching between systems.

---

## 5. Non-Goals

- **Replacing PerchGuard's stateless validation pipeline.** The integration enriches the session-aware layer (Phase 3); it does not change how quota checks or pattern-matching validators work. Rationale: stateless validators are fast, independent, and should stay that way.
- **Making the project context store a security tool.** The project context store's purpose is developer context and project memory. PerchGuard owns enforcement decisions. The context store records them; it does not make them. Rationale: mixing governance authority into a project management tool creates confused ownership and undermines both.
- **Cloud-syncing governance events.** All integration operates local-first. Rationale: introducing a cloud dependency for safety data would block adoption in air-gapped or compliance-constrained environments.
- **Governing project context tool AI operations.** Project context tooling may use AI to assist PRD/ADR creation. PerchGuard will not intercept those calls in v1. Rationale: circular governance adds complexity with minimal marginal safety benefit at this stage.
- **Multi-tenant or multi-developer session merging.** This integration targets solo and small-team workflows. Rationale: multi-developer session attribution is a separate, harder problem that should not block v1.

---

## 6. Constraints

- **Solo builder:** Both systems are maintained by a single developer. Integration scope must be achievable without a dedicated team.
- **Local-first:** No new cloud dependencies may be introduced. All integration data must be storable and queryable without network access.
- **Zero-disruption to existing workflows:** A developer not using the integration must experience no change in PerchGuard behaviour. Opt-in at the session level.
- **Token budget:** Context injected into PerchGuard must stay within a budget that does not meaningfully increase per-call latency. The existing 8k token constraint on context file budget applies.
- **Append-only audit semantics:** Governance events written to project snapshots must follow the same append-only model as all other snapshot data. Past records may not be modified.

---

## 7. Open Questions

- Is the PRD the right source of truth for PerchGuard's intent baseline, or should it be the most recent ADR for the active domain? The ADR is more specific but less stable; the PRD is stable but may be too broad for accurate drift detection.
- Should governance events be embedded directly in the project snapshot, or stored as a separate linked artefact? Embedding keeps context co-located; separation keeps the snapshot format clean.
- At what cadence should PerchGuard re-read project context during a long session? Once at session start is simple; periodic refresh would catch PRD updates mid-session but adds complexity.
- Can the same project context serve both the AI assistant (Copilot/Claude) and PerchGuard, or do they need different views of the same data? A shared format reduces duplication; separate views reduce coupling.
- Should Auto-generated PerchGuard policies (Nice-to-Have story 1) be versioned as ADRs, as policy files, or as both?

---

## 8. Release Criteria

_What "done" looks like for v1_

- [ ] **Functionality:** PerchGuard reads project context (PRD + recent snapshots) at session start and uses it to seed the SessionAgent intent baseline before the first tool call is evaluated.
- [ ] **Functionality:** DENY, MUTATE, HUMAN_REVIEW, and TERMINATE governance events are written into the active project snapshot automatically, with tool call reference and decision reason.
- [ ] **Usability:** A developer using both tools sees zero new required steps — context seeding and event recording happen without any manual command.
- [ ] **Usability:** A reviewer can open any governance snapshot from an integrated session and find governance events in a clearly labelled section without reading PerchGuard logs.
- [ ] **Reliability:** If project context is unavailable at session start (no PRD, no snapshots, offline), PerchGuard falls back to cold-start behaviour without error. The integration degrades gracefully.
- [ ] **Reliability:** Governance event writes to project snapshots do not block the admission decision. A failure to write must not delay or alter the governance outcome.
- [ ] **Performance:** Context seeding at session start adds no more than 50ms to the first tool call's end-to-end latency.
- [ ] **Security:** project context read by PerchGuard is treated as advisory input only. It cannot elevate permissions or override an explicit DENY in the policy file.
- [ ] **Maintenance:** The integration is documented in a single ADR covering the data contract between the two systems, so either system can be updated independently without silent breakage.
