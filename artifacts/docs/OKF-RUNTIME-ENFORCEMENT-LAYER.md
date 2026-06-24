# PerchGuard as the Runtime Enforcement Layer for OKF-Governed Agents

## Core Idea

Open Knowledge Format (OKF) can define what an AI agent is allowed to know, cite, depend on, and do.

PerchGuard can enforce those declarations at runtime.

The positioning:

> OKF describes governed knowledge. PerchGuard enforces governed action.

This turns PerchGuard from "agent drift detection" into a runtime trust layer for AI systems that consume structured, source-linked company knowledge.

---

## Why This Is Interesting

OKF is a portable file format for representing knowledge as markdown plus YAML frontmatter. It is human-readable, agent-readable, version-controlled, and source-linked.

That makes it useful for AI context management, but by itself OKF does not enforce behavior.

A company can publish an OKF bundle that says:

- this policy is approved
- this dataset is sensitive
- this runbook applies to production incidents
- this metric definition is current
- this document is deprecated
- this agent may use this knowledge source

But something still has to make sure agents respect those declarations when they act.

That is the PerchGuard opportunity.

---

## Product Framing

OKF answers:

- What knowledge exists?
- Where did it come from?
- Who owns it?
- Is it current?
- How does it relate to other knowledge?
- What type of concept is this?

PerchGuard answers:

- Is this agent allowed to use that knowledge?
- Is this tool call consistent with the declared mission?
- Is the agent drifting outside its approved scope?
- Is it sending sensitive knowledge to the wrong place?
- Can we prove what happened later?

Together:

> OKF gives agents structured context. PerchGuard makes that context enforceable.

---

## Example: OKF-Governed Agent Session

An AI agent registers with PerchGuard and declares:

- mission baseline
- allowed phases
- out-of-scope actions
- permitted tools
- allowed OKF bundles
- sensitive OKF concepts it must not exfiltrate
- policy documents it must follow

PerchGuard stores this declaration immutably at session start.

During execution, every tool call can be checked against:

- the registered mission
- the OKF knowledge packet metadata
- PII / biometric policy
- destination risk
- drift score
- audit requirements

The agent cannot rewrite its own baseline mid-session. If it needs a new mission, it must start a new governed session.

---

## What PerchGuard Could Enforce

### 1. Knowledge Scope

Agents can only use OKF bundles or concepts declared in their session manifest.

Example:

> A support agent can read customer support runbooks, but not payroll policies or biometric identity datasets.

### 2. Source Freshness

Agents can be blocked or reviewed if they rely on stale knowledge.

Example:

> A deployment agent cannot execute a production runbook if the OKF timestamp is older than the approved freshness window.

### 3. Owner and Approval Metadata

Agents can be required to act only on knowledge owned or approved by trusted teams.

Example:

> A compliance agent may cite only OKF documents approved by Legal or Security.

### 4. Sensitive Concept Handling

OKF concepts can carry sensitivity metadata. PerchGuard can stop sensitive content from leaving allowed destinations.

Example:

> A customer agent may summarize a regulated policy, but may not send raw biometric fields to an external SaaS tool.

### 5. Mission Drift Against Knowledge Use

Drift detection can include not only tool calls, but also knowledge access patterns.

Example:

> An agent registered to help with "library sorting" suddenly reads "teacher office access policy" and calls an admin tool. That is both knowledge drift and action drift.

### 6. Audit Proof

PerchGuard can produce governance records showing:

- which OKF concepts were used
- which policies applied
- which calls were allowed, reviewed, mutated, or denied
- drift score per action
- redactions performed
- session baseline and immutable scope

This makes compliance evidence machine-readable instead of a pile of screenshots and logs.

---

## OKF-Formatted Proof

PerchGuard could emit audit artifacts as OKF-compatible markdown documents.

Example concept types:

- `Agent Session`
- `Tool Call`
- `Policy Decision`
- `Drift Event`
- `Redaction Event`
- `Governance Summary`

Each artifact could include frontmatter like:

```yaml
---
type: Agent Session
title: Insurance Claims Assistant Session
session_id: sess_123
agent_id: claims-assistant
owner: risk-platform
mission: Triage insurance claims and summarize missing documentation.
allowed_bundles:
  - okf://claims/policies
  - okf://claims/runbooks
out_of_scope:
  - approve claim payouts
  - alter customer identity records
  - access biometric data
drift_threshold: 0.72
timestamp: 2026-06-16T14:30:00Z
---
```

The body could describe what happened in human-readable form, while the frontmatter remains queryable by downstream audit tools.

---

## Why This Is Stronger Than Generic RAG Governance

Generic RAG governance usually controls document retrieval loosely:

- which index can be queried
- which documents are tagged private
- what the prompt says the agent should avoid

PerchGuard plus OKF can be stricter:

- knowledge has structured identity
- session scope is immutable
- tool calls are intercepted at runtime
- sensitive content can be redacted before logging or egress
- drift is measured continuously
- evidence is generated as structured audit output

This creates a closed loop:

1. OKF declares trusted knowledge.
2. Agent uses that knowledge.
3. PerchGuard enforces scope and policy.
4. PerchGuard emits proof of enforcement.
5. Proof can itself be stored as OKF.

---

## Market Positioning

PerchGuard could be positioned as:

> Runtime governance for AI agents using structured enterprise knowledge.

Or more directly:

> PerchGuard enforces what OKF describes.

This is especially relevant for:

- regulated enterprises
- AI agent platforms
- data catalog vendors
- compliance-heavy SaaS companies
- internal enterprise AI teams
- consulting firms deploying agents into customer environments

---

## Potential Product Features

- OKF bundle allowlists in agent manifests
- OKF concept sensitivity checks
- OKF freshness validation before tool execution
- OKF owner / approval validation
- drift scoring that includes knowledge access
- governance.json export mapped into OKF markdown artifacts
- visual audit graph: sessions, policies, concepts, tool calls, drift events
- policy templates for common regulated workflows

---

## Strategic Takeaway

OKF is a format for portable AI knowledge.

PerchGuard can become the runtime layer that makes portable AI knowledge safe to act on.

That is the interesting product story:

> Enterprises do not only need agents that know more. They need agents whose knowledge use and actions can be governed, enforced, and proven.
