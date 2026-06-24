# PerchGuard Phase 8 — PSD2 / Open Banking Vertical

> **Note**: This document is the detailed spec for the **PSD2 / Open Banking vertical**
> under the broader Phase 8 umbrella. The umbrella thesis — PerchGuard as a generic
> agentic-compliance engine, with PSD2, EU AI Act, and SOC2 as parallel verticals sharing
> one pipeline — is in `PHASE8-AGENTIC-COMPLIANCE.md`. Read that first; everything below
> remains the working spec for the first (and richest) vertical.

**Date**: 2026-06-05
**Branch**: `phase8-open-banking` (not yet cut)
**Status**: Vision / Pre-planning — first vertical under `PHASE8-AGENTIC-COMPLIANCE.md`
**Depends on**: Phase 7 complete (claude-hook, init, SQLite audit, HITL review)

---

## 1. Where This Comes From

PerchGuard did not start as a generic tool. It started as BankingKube — an admission controller
for a regulated financial workload running on Kubernetes. The core insight was borrowed from
that context: before any action executes against a sensitive system, something trusted must
intercept it, evaluate it against policy, and decide allow / deny / mutate / escalate.

Phases 1–7 generalised that insight so it could govern any AI agent, in any domain, with no
financial knowledge baked in. That was the right move — a generic trust anchor is more defensible
than a domain-specific one, and it gave PerchGuard a story outside fintech.

But the original domain is also the richest one. Every design decision in the pipeline —
DENY / ALLOW / MUTATE / HUMAN_REVIEW, risk scoring, session-stateful drift detection, tamper-evident
audit — was made with financial transaction semantics in mind, even when the examples used healthcare
or generic developer agents. Phase 8 is the return to source.

The architecture is: **PerchGuard v7 = the generic trust layer. v8+ = the open banking domain
sitting on top of it.** The domain does not replace or fork the admission controller. It is a
vertical that uses the controller's full surface: policy profiles, the REVIEW outcome, the audit
store, the agent fleet, the semantic firewall. What the domain adds is financial context: regulated
roles, PSD2-aware thresholds, financial data redaction, and the agent archetypes that open banking
actually needs.

---

## 2. What Open Banking Is — and Why It Needs This

Open banking is the regulatory-mandated practice of making financial data and payment initiation
accessible to licensed third parties via APIs. In Europe, PSD2 (Payment Services Directive 2) is the
law. PSD3 is in progress. Open Finance extends the same model to investments, pensions, insurance.

The two core API roles under PSD2:

| Role | What it does | Regulated as |
|------|-------------|-------------|
| **AISP** — Account Information Service Provider | Reads account data across multiple banks on behalf of a customer | Licensed by national regulator (FCA, DNB, BaFin, etc.) |
| **PISP** — Payment Initiation Service Provider | Initiates payments from a customer's bank account | Licensed; must use Strong Customer Authentication |

The bank holding the account is the **ASPSP** (Account Servicing Payment Service Provider). It
exposes the open banking API. The TPP (Third Party Provider) — whether AISP or PISP — calls it.

**The problem that AI agents create in this context:**

Traditional open banking assumes a human at the keyboard who clicks "approve" before money moves.
SCA (Strong Customer Authentication) is built on that assumption: two factors, explicit consent,
per-transaction authorization. An AI agent breaks that model. An agent can read account balances,
infer spending patterns, and initiate a payment — all without a human in the loop — faster than any
fraud detection system trained on human-speed transactions.

An AISP agent that aggregates accounts across 12 banks is not doing anything a human couldn't do
with a spreadsheet, but it does it in 200ms, at scale, and it exfiltrates all of it in one tool
call if not governed. A PISP agent that "optimises cash flow" can drain a current account before
a human sees the notification.

The threat surface is not the agent misbehaving — it is the agent behaving exactly as instructed,
but with instructions that were never reviewed by a human who understood the financial implications.

**PerchGuard's answer is not a firewall. It is SCA for agentic actions.**

Every tool call from an AI agent in the open banking context passes through the admission pipeline.
High-risk calls — payment initiation above threshold, new beneficiary addition, standing order
creation — do not execute until a human confirms. The REVIEW outcome is the agent-era equivalent
of two-factor authorisation. The audit store is the transaction ledger the regulator asks for.
The semantic firewall is the AML layer.

---

## 3. Problems Worth Solving

These are not hypothetical. Each of these represents a gap in the current open banking ecosystem
that an agentic layer governed by PerchGuard can close.

### 3.1 Payment Initiation with Human-in-the-Loop SCA

**The problem:** A PISP agent that can initiate payments on behalf of a customer needs an
authorisation step that is meaningfully equivalent to SCA under PSD2 Article 97. Existing
open banking SCA flows assume a redirect — user leaves the TPP app, authenticates at their bank,
returns. That flow breaks for unattended or background agents.

**What PerchGuard enables:** The REVIEW outcome from Phase 7 is the authorisation step. When a
payment initiation tool call arrives, the pipeline scores it. Above the review threshold, it
pauses the agent and sends a Slack/push notification to the account holder: "Your agent wants to
pay €2,400 to ACME Corp via SEPA. [Approve] [Deny]". The agent waits. The human decides.
This is not a workaround for SCA — it is SCA, adapted for the agentic context. The audit record
proves it: timestamp, agent session, tool parameters, decision, reviewed_by, reviewed_at.

### 3.2 Account Aggregation with Data Minimisation

**The problem:** An AISP agent aggregating accounts across multiple banks collects more data than
any single query needs. A "what is my current balance?" intent does not require transaction
history, counterparty names, or geolocation data — but a poorly scoped tool call returns all of it.
Under GDPR and PSD2 Article 67 (data minimisation), collecting more than necessary is a violation.

**What PerchGuard enables:** The DataExfiltrationValidator and OutputValidator already enforce
destination and content controls. In the open banking profile, they are extended to:
- Scan tool outputs for IBAN patterns, PANs (card numbers), sort codes, and account numbers
  not belonging to the declared session scope
- Redact or strip fields not required by the stated intent (declared at session registration)
- Block any aggregation tool call that touches a bank not in the authorised institution list for
  the session

The agent gets exactly what the user consented to. Nothing more travels through the pipeline.

### 3.3 Smart Escrow

**The problem:** Escrow — holding funds until a condition is met — requires a trusted neutral party
to verify the condition and release the funds. Today that is a solicitor, a notary, or a bank's
own escrow product. The cost and latency of human verification is the friction. An AI agent that
can verify conditions (e.g., "has the property title deed been filed?", "has the delivery been
confirmed?") and trigger release is a genuine compression of that friction.

**What PerchGuard enables:** An escrow agent with a PerchGuard-governed payment initiation tool.
The agent evaluates the release condition. When it decides to release, that decision is a tool
call — `initiate_payment` with beneficiary, amount, reference. PerchGuard intercepts it, scores
the risk, and routes it to HUMAN_REVIEW regardless of score (policy: `alwaysReview: true` for
`initiate_payment` in the escrow role). Both parties to the escrow — buyer and seller — receive
the notification. Both must approve. Only then does the payment initiate.

The audit record of the condition evaluation + both approvals + payment initiation is the
legal evidence trail. This is not just a product feature — it is a business model: a governed
AI escrow service charges per transaction, has no human overhead in the verification step,
and has a full audit trail for dispute resolution.

### 3.4 Fraud Detection at the Agent Layer

**The problem:** Traditional fraud detection works on transaction data after the fact. It asks:
"does this transaction look like fraud compared to past transactions?" An AI agent introduces
a new attack surface: the agent's behaviour before the transaction. A compromised or manipulated
agent exhibits detectable anomalies — unusual tool call sequences, drift from declared intent,
reconnaissance patterns — before any money moves.

**What PerchGuard enables:** The Phase 3 BehaviorAnalyzer already detects recon→exploit→exfiltrate
sequences. In the open banking context, the sequences to detect are different:

| Sequence | What it indicates |
|----------|-----------------|
| `list_accounts` → `get_balance` (all accounts) → `get_transaction_history` → `initiate_payment` | Reconnaissance before large transfer — classic account takeover pattern |
| Multiple `add_beneficiary` calls → `initiate_payment` | Beneficiary seeding before bulk transfer |
| `get_standing_orders` → `delete_standing_order` → `create_standing_order` | Standing order hijack |
| High-frequency `get_balance` (> N calls/minute) | Timing attack — agent is watching for a deposit to arrive before initiating transfer |

These are defined as named behavior patterns in the open banking policy profile. When detected,
the session risk accumulator fires. Above threshold: REVIEW. The human sees not just the pending
call but the full behavioral sequence that triggered it.

This is pre-transaction fraud detection, not post-transaction fraud detection. It acts before
money moves.

### 3.5 Financial Observability and Operations

**The problem:** Finance teams, treasury operations, and compliance officers need to understand
what AI agents are doing with money in near real-time. Existing tools give them transaction
reports — what happened. They need operational visibility into what is about to happen and why,
and whether it is within policy.

**What PerchGuard enables:** The Grafana fleet dashboard (Phase 7) with open banking labels:

- **Payment flow map** — sessions plotted by institution, direction (inbound/outbound), and
  amount. Makes the agent's financial activity visible as a map, not a log.
- **Beneficiary risk panel** — new beneficiaries flagged, with session context and the risk
  score that was assigned at initiation time.
- **Regulatory headroom gauge** — per-session API call counts vs PSD2 Article 29 access
  frequency limits. Shows when an AISP agent is approaching the threshold where the ASPSP
  is permitted to throttle it.
- **Escrow status board** — pending escrow conditions, time in PENDING state, parties notified.
- **Compliance report export** — one-click DORA Article 11 / PSD2 Article 96 incident report
  from the audit store. Structured JSON that maps directly to the fields the regulator asks for.

This is the observability layer that a treasury team can run next to their Bloomberg terminal.
It does not replace existing banking infrastructure — it governs the AI layer that sits in front
of it.

---

## 4. The Layer Model

```
┌─────────────────────────────────────────────────────────────────┐
│  Phase 9+: Vertical Agents                                      │
│  PaymentInitiationAgent  EscrowAgent  FraudAnalystAgent         │
│  AccountAggregationAgent  ComplianceAgent  TreasuryAgent        │
└───────────────────────────┬─────────────────────────────────────┘
                            │ tool calls
┌───────────────────────────▼─────────────────────────────────────┐
│  Phase 8: Open Banking Domain Layer                             │
│  PerchGuard + open-banking.yaml policy profile                  │
│  Financial data redaction  PSD2-aware HITL  FAPI token adapter  │
│  Regulated agent roles     Open banking behavior patterns       │
└───────────────────────────┬─────────────────────────────────────┘
                            │ admission decisions
┌───────────────────────────▼─────────────────────────────────────┐
│  Phase 7: Generic Admission Controller (PerchGuard v7)          │
│  claude-hook  init  SQLite audit  HITL review  Grafana          │
└───────────────────────────┬─────────────────────────────────────┘
                            │ governs
┌───────────────────────────▼─────────────────────────────────────┐
│  Open Banking APIs                                              │
│  ASPSP (banks)  Payment rails (SEPA, Faster Payments, SWIFT)    │
│  Account aggregation  TPP registry  FCA/DNB/BaFin lookups       │
└─────────────────────────────────────────────────────────────────┘
```

Phase 8 is a configuration and adapter layer, not a fork. It does not modify the admission
pipeline. It provides:

1. `configs/profiles/open-banking.yaml` — the policy profile (written below in Phase 8.1)
2. Financial data redaction patterns (IBAN, PAN, sort code, BIC) added to output validation
3. PSD2 SCA adapter — maps the REVIEW outcome to a consent record that satisfies regulatory audit
4. Open banking agent roles in the toolAuthorization policy
5. Open banking behavior patterns in the agentFleet policy
6. FAPI-compliant token verification adapter for ASPSP-issued access tokens

---

## 5. Phase 8 Capabilities

| # | Capability | Key deliverables | Priority |
|---|-----------|-----------------|----------|
| 8.1 | Open banking policy profile | `configs/profiles/open-banking.yaml` | P0 |
| 8.2 | Financial data redaction | IBAN, PAN, sort code, BIC patterns in OutputValidator | P0 |
| 8.3 | PSD2 SCA adapter | REVIEW → consent record with regulatory audit fields | P0 |
| 8.4 | Open banking agent roles | payment_initiation, account_aggregation, fraud_analyst, escrow, compliance | P1 |
| 8.5 | Open banking behavior patterns | 4 fraud sequences, configurable in policy profile | P1 |
| 8.6 | FAPI token adapter | Verify ASPSP-issued FAPI access tokens in X-PerchGuard-Agent-Token position | P1 |
| 8.7 | Regulatory snapshot report | DORA Article 11 / PSD2 Article 96 compliance export from SQLite | P1 |
| 8.8 | Open banking Grafana panels | Payment flow map, beneficiary risk, regulatory headroom, escrow board | P2 |
| 8.9 | Escrow policy primitive | `alwaysReview: true` + dual-approval (both parties must POST approve) | P2 |

---

## 6. What Phase 8 Deliberately Excludes

- **ASPSP / bank API integration** — PerchGuard governs agents that call bank APIs; it does not
  implement the bank APIs themselves. Integration with specific bank APIs (Monzo, Starling, TrueLayer,
  Plaid, Nordigen/GoCardless) belongs in the vertical agent layer (Phase 9+).
- **Payment processing** — PerchGuard intercepts and authorises payment initiation tool calls; it
  does not process payments. The payment execution remains with the regulated PISP/ASPSP.
- **KYC / identity verification** — out of scope; assume the customer identity layer sits upstream.
- **Multi-currency FX** — a product feature for vertical agents, not a governance concern.
- **Consumer-facing UI** — the Grafana dashboard is operator-facing. Customer-facing consent UI
  (the SCA redirect experience) is a Phase 9 product decision.

---

## 7. Phase 8 Success Criteria

1. A governed payment initiation agent runs against a sandbox open banking API (TrueLayer sandbox
   or Nordigen). Every `initiate_payment` call above €500 pauses and sends a REVIEW notification.
   Payment executes only after operator approval. The audit record contains all PSD2 Article 96
   fields: timestamp, agent session, amount, beneficiary IBAN, risk score, reviewed_by, reviewed_at.

2. An account aggregation agent is registered with role `account_aggregation`. It requests
   transaction history for an account not in its declared scope. PerchGuard blocks it.
   The audit record shows DENY with reason `out_of_scope_account`.

3. A fraud simulation runs the `recon_balance_sweep_transfer` behavior pattern. Risk accumulator
   fires at the third call in the sequence. The fourth call (payment initiation) is routed to
   REVIEW before the agent sees a response.

4. `perchguard init --profile=open-banking` loads the open banking policy profile, registers
   the fintech agent roles, and prints the regulatory headroom panel URL.

---

## 8. The Consulting Angle

The Phase 7 consulting delivery package — one customer, one workflow, one invoice — has its
most credible vertical here. The fintech sector has a compliance officer, a legal team, and a
regulator who all need to see the same thing: proof that the AI agent cannot act without
authorisation, that every action is audited, and that the audit trail meets the regulatory standard.

PerchGuard with the open banking profile is not a bolt-on. It is the answer to the question
every bank CISO asks when a TPP or internal team proposes an AI agent on a payment workflow:
"what stops it from doing something it shouldn't?"

The red-team adversarial snapshot (Phase 5) applied to the open banking threat model — the four
fraud behavior sequences, the beneficiary seeding pattern, the standing order hijack — is the
deliverable that gets compliance sign-off. The REVIEW audit trail is the deliverable that satisfies
the regulator. The Grafana dashboard is the deliverable that keeps the treasury team calm.

Three of those already exist in the generic layer. Phase 8 gives them fintech vocabulary.
