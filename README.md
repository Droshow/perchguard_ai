# PerchGuard

**The missing governance layer for AI agents.**

PerchGuard applies the Kubernetes **admit / deny / mutate** pattern to every tool call an AI agent tries to execute. Just as you wouldn't run a cluster without an admission controller, you shouldn't run autonomous agents without PerchGuard.

```
K8s:         kubectl apply (Pod YAML)  →  AdmissionWebhook  →  etcd
PerchGuard:  agent.execute_tool()      →  PerchGuard         →  Tool Server
```

---

## See it decide in 30 seconds

No Docker, no config — just clone and run:

```bash
git clone https://github.com/perchguard/PerchGuard_AI.git
cd PerchGuard_AI/perchguard
./scripts/demo.sh
```

This builds PerchGuard, registers an agent, and fires three real tool calls
through `/intercept`:

```
read_file    (in scope)               → ALLOW
write_file   (denied for this role)   → DENY
read_policy  (carries an SSN)         → HUMAN_REVIEW
```

A human operator approves the flagged call, the session closes, and the
script reads the resulting governance record straight back out of
`snapshots/governance.json`. Then — from that same audit trail, no extra
setup — it generates an **EU AI Act Art. 26 / Annex IV compliance report**.

That's the whole pitch in one script: every tool call gets a real-time
admit/deny/mutate decision, and every decision is durable evidence you can
hand to an auditor. Walkthrough with full sample output: [demo.md](artifacts/docs/demo.md).

---

## The Problem

AI agents are fundamentally different from traditional software:

- They execute tool calls **autonomously**, without human review of each action
- They read untrusted content (web pages, files, emails) and **act on it**
- They can be manipulated via **indirect prompt injection** — malicious instructions embedded in content, not the user prompt
- They can loop indefinitely, incurring **unbounded cost**

Without PerchGuard, an agent is a powerful but ungoverned actor.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   AGENT FRAMEWORK                           │
│   (Claude Code / LangChain / AutoGen / Custom)              │
└───────────────────────────┬─────────────────────────────────┘
                            │  HTTP POST /intercept
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      PERCHGUARD                             │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  QUOTA   (stateful, fast-fail)                      │   │
│  │  ├── DepthLimiter    → max agent-call-agent depth    │   │
│  │  └── SessionBudget   → token / cost / call-rate cap  │   │
│  └──────────────────────┬──────────────────────────────┘   │
│                         │ pass                              │
│  ┌──────────────────────▼──────────────────────────────┐   │
│  │  VALIDATION  (stateless, policy-driven)              │   │
│  │  ├── PromptInjection   → scan for hijack patterns    │   │
│  │  ├── ToolAuthorization → role-based allow/deny list  │   │
│  │  ├── DataExfiltration  → egress URL check            │   │
│  │  └── SemanticFirewall  → LLM intent alignment        │   │
│  └──────────────────────┬──────────────────────────────┘   │
│                         │ pass                              │
│  ┌──────────────────────▼──────────────────────────────┐   │
│  │  MUTATION  (transform, not block)                    │   │
│  │  ├── ParameterSanitizer → strip -rf, inject --dry-run│   │
│  │  └── LeastPrivilege    → scope SQL, restrict paths   │   │
│  └──────────────────────┬──────────────────────────────┘   │
│                         │ ALLOW / DENY / MUTATE             │
│  ┌──────────────────────▼──────────────────────────────┐   │
│  │  AUDIT  (every call, every decision)                 │   │
│  │  └── OpenTelemetry spans + JSONL audit trail         │   │
│  └─────────────────────────────────────────────────────┘   │
└───────────────────────────┬─────────────────────────────────┘
                            │ (if ALLOW or MUTATE)
                            ▼
┌─────────────────────────────────────────────────────────────┐
│              OUTBOUND ADMISSION  (return path)              │
│  POST /validate/output  →  OutputValidator                  │
│  Scans tool results before they reach the agent             │
└─────────────────────────────────────────────────────────────┘
```

| Decision | Meaning |
|---|---|
| `ALLOW` | All checks passed |
| `DENY` | Hard block — policy violation |
| `MUTATE` | Allowed, but parameters sanitized |
| `HUMAN_REVIEW` | Paused — human notified via webhook |
| `TERMINATE` | Session quota exceeded — session killed |

---

## What It Guards Against

| Threat | Defence |
|---|---|
| Indirect prompt injection | `PromptInjectionValidator` scans every tool parameter and tool output |
| Semantic hijack | `SemanticFirewallValidator` uses an LLM to verify the call serves the user's intent |
| Unauthorized tool use | `ToolAuthorizationValidator` enforces role-scoped allow/deny lists |
| Data exfiltration | `DataExfiltrationValidator` blocks calls to non-allowlisted destinations |
| Accidental destruction | `ParameterSanitizer` injects `--dry-run`, strips `--force` / `-rf` |
| Denial-of-wallet | `SessionBudgetChecker` caps tokens, cost, and call rate per session |
| Agent-calling-agent spirals | `DepthLimiter` enforces max recursion depth |
| Injected instructions in tool output | `OutputValidator` scans results before they reach the agent |
| High-risk borderline calls | `HumanReviewDispatcher` blocks and POSTs to a webhook for human approval |

---

## Run It For Real

Past the demo and want to govern an actual agent? Start the server:

```bash
docker compose -f deployments/docker-compose/docker-compose.yaml up perchguard --build
```

PerchGuard starts in `observe` mode — the full pipeline runs and every decision is audited, but nothing is blocked. Read `snapshots/audit.jsonl` to see what would have fired, then flip to `enforce` when you're ready.

```bash
# First intercept call
curl -s -X POST http://localhost:8080/intercept \
  -H "Content-Type: application/json" \
  -d '{
    "uid": "1", "session_id": "s1", "agent_id": "a1",
    "agent_role": "developer_agent",
    "user_intent": "read config",
    "tool_call": {"name": "read_file", "parameters": {"path": "/workspace/config.yaml"}}
  }' | jq .decision
```

For local IDE agents (Copilot, Claude Code):

```bash
PERCHGUARD_MCP_UPSTREAM=http://localhost:3000 go run ./cmd --mode=copilot
# Opens browser dashboard at http://localhost:8081
```

Already have an audit trail? Generate an EU AI Act compliance report from it directly:

```bash
go run ./cmd --mode=compliance --doc=annex-iv --format=markdown
```

See [compliance-export.md](artifacts/docs/compliance-export.md) for the full Art.26 / Annex IV walkthrough.

---

## Documentation

→ **[artifacts/docs/](artifacts/docs/README.md)** — getting started, configuration reference, deployment, agent integration, red team guide

---

## Self-hosted

PerchGuard runs entirely on your infrastructure. No cloud dependency, no telemetry home. The audit trail (`snapshots/audit.jsonl`) stays on your machine.

The semantic firewall requires an Anthropic API key. Everything else works without one.
