# Agentic Framework Landscape — PerchGuard Integration Strategy

**Date:** 2026-05  
**Context:** PerchGuard as enterprise admission controller for autonomous agents

---

## The core positioning question

PerchGuard intercepts tool calls. Every autonomous agent framework eventually makes tool calls. That is the seam. The question is not *whether* PerchGuard fits — it fits everywhere — but which frameworks enterprises are actually deploying, and what the integration surface looks like for each.

---

## Tier 1 — You must be here

These are the frameworks that a credible enterprise customer will arrive with on day one.

### MCP (Model Context Protocol) — Anthropic

**Why it wins:** MCP is becoming the HTTP of agent tool interop. It is a standard, not a framework. Agents speak MCP to tools; tools expose MCP servers. The adoption curve is fast because Anthropic, OpenAI, Google, and most IDE vendors have all converged on it.

**PerchGuard integration surface:** This is the highest-leverage integration. PerchGuard can run as an **MCP proxy gateway** — it sits between any MCP client (the agent) and any MCP server (the tool), intercepts every tool call at the protocol level, and applies the full admission pipeline before the call reaches the tool server. One integration point covers every MCP-native agent regardless of which LLM or orchestrator is behind it.

```
Agent (any LLM) → MCP Client → [PerchGuard MCP Proxy] → MCP Server (tool)
```

**Effort:** High (protocol-level proxy), but this is the most defensible moat.

---

### LangGraph (LangChain)

**Why it wins:** LangGraph is the stateful, graph-based orchestration layer of LangChain. It is the most mature enterprise story in the LangChain ecosystem because it handles the hard part: durable state, human-in-the-loop checkpoints, and multi-agent coordination. Fortune 500 pilots overwhelmingly run LangGraph when the workflow is complex.

**PerchGuard integration surface:** Two options, both clean.

1. **HTTP tool wrapper:** LangGraph tools are Python callables. A thin wrapper routes every tool call through `POST /intercept` before it executes. One import, zero framework changes.
2. **LangSmith-compatible audit hook:** LangGraph already has a callback/tracing layer. PerchGuard can attach as a callback that intercepts *before* execution rather than after.

The wrapper approach ships fastest; the callback approach is more idiomatic for LangGraph users.

---

### OpenAI Agents SDK (formerly Swarm)

**Why it wins:** OpenAI's official multi-agent framework. Enterprises that start with GPT-4o almost always reach for this when they need agent handoffs and tool orchestration. It has function-calling baked in as the tool mechanism.

**PerchGuard integration surface:** OpenAI's function-calling is HTTP under the hood. PerchGuard wraps tool definitions — tools are re-declared as PerchGuard-proxied endpoints. When the agent calls a tool, it actually calls PerchGuard's `/intercept`, which validates and then forwards to the real tool. The agent sees no difference. This is the same pattern as PerchGuard's declared-intent model.

---

### Microsoft Semantic Kernel

**Why it wins:** This is Microsoft's enterprise AI SDK — `.NET` and `Python`, with deep Azure integration. It is the default choice for enterprises already running Microsoft stacks, which is a large portion of the Fortune 1000. Semantic Kernel has a formal **filter pipeline** (pre- and post-function-call hooks), which is the cleanest integration point PerchGuard has seen in any framework.

**PerchGuard integration surface:** Implement a Semantic Kernel `IFunctionInvocationFilter`. This is a first-class SDK concept: it intercepts every function (tool) call before execution. The filter calls PerchGuard's `/intercept` and either continues or blocks based on the decision. This is architecturally identical to a Kubernetes admission webhook — which is exactly PerchGuard's mental model.

```csharp
// Semantic Kernel filter — one class, plugs into the kernel
public class PerchGuardFilter : IFunctionInvocationFilter {
    public async Task OnFunctionInvocationAsync(
        FunctionInvocationContext context, Func<FunctionInvocationContext, Task> next) {
        await perchguard.Intercept(context); // blocks or passes
        await next(context);
    }
}
```

---

### Microsoft AutoGen / AG2

**Why it wins:** AutoGen pioneered the multi-agent conversation model — agents talk to each other, delegate tasks, and call tools as part of the conversation. Microsoft Research backs it; enterprise AI teams know the name. AG2 is the community-maintained fork with broader adoption momentum.

**PerchGuard integration surface:** AutoGen tools are registered as Python functions. The same wrapper approach as LangGraph applies. The subtlety with AutoGen is that tool calls happen inside agent-to-agent conversations — the call context is richer (which agent is calling which tool on behalf of which orchestrator). PerchGuard's session-stateful FleetManager is the right piece to handle this: each AutoGen agent maps to a registered PerchGuard session, so lineage is tracked across the conversation graph.

---

## Tier 2 — Fast-growing, real enterprise deployments

### CrewAI

**Why it wins:** CrewAI popularized role-based multi-agent composition — you define agents with roles (Researcher, Writer, Analyst) and let them collaborate on tasks. It is simple enough that non-ML engineers can build with it, which accelerates enterprise adoption. Very active GitHub, strong SMB-to-mid-market pull, increasingly in enterprise pilots.

**PerchGuard integration surface:** CrewAI tools are Python classes inheriting from `BaseTool`. A `PerchGuardTool` base class that wraps the intercept call is a one-file integration. Every custom tool in a CrewAI deployment inherits governance without changing existing tool code.

---

### AWS Bedrock Agents

**Why it wins:** AWS-managed agents. Enterprises with significant AWS spend will reach for Bedrock Agents because the IAM, VPC, and CloudWatch integration is already there. The action group model (Lambda functions as tools) is the tool mechanism.

**PerchGuard integration surface:** PerchGuard runs in the same VPC. The Lambda functions that back Bedrock action groups call PerchGuard's `/intercept` as their first action before executing business logic. Alternatively, an API Gateway policy can route all action group calls through PerchGuard. The Kubernetes deployment (via EKS Fargate) already fits this topology.

---

### Dify

**Why it wins:** Dify is an open-source LLM application development platform with a visual workflow builder and a strong enterprise self-hosted story. It has gained significant enterprise traction in Asia-Pacific and is growing in EU/US. Its agent workflow mode competes directly with LangGraph at the product layer.

**PerchGuard integration surface:** Dify supports custom tool endpoints (HTTP). A PerchGuard integration is a Dify tool node that proxies through `/intercept`. This can be packaged as a Dify plugin, which fits the open-source distribution story well.

---

### Haystack (deepset)

**Why it wins:** Haystack is the enterprise-grade framework for search + RAG + agent pipelines. It has deep integration with vector databases (Weaviate, Pinecone, Qdrant) and is the default choice when the agent's primary job is retrieval. Enterprise search vendors and knowledge management products build on Haystack.

**PerchGuard integration surface:** Haystack components are pipeline nodes. A `PerchGuardComponent` node wraps any tool-calling node in the pipeline and routes through `/intercept`. Haystack's declarative pipeline YAML means this can be injected without modifying Python code — just insert the component in the pipeline definition.

---

## Tier 3 — Worth watching, not worth building for today

| Framework | Signal | Notes |
|-----------|--------|-------|
| **BeeAI (IBM)** | IBM enterprise backing | Agent framework with strong TypeScript story; IBM's cloud customers are the target; integration would follow the HTTP wrapper pattern |
| **Google ADK (Agent Development Kit)** | Vertex AI integration | Google's answer to Semantic Kernel; Vertex AI Agents use this; relevant for GCP-heavy enterprises |
| **n8n** | Workflow automation + AI | Strong no-code/low-code segment; AI nodes call LLMs; PerchGuard as an n8n node type would reach non-developer enterprise users |
| **Dapr** | Sidecar runtime | Not an agent framework, but Dapr is used alongside agents in microservice architectures; PerchGuard as a Dapr component fits naturally |

---

## The integration matrix

| Framework | Integration mechanism | PerchGuard component | Effort |
|-----------|----------------------|---------------------|--------|
| MCP | Protocol-level proxy | MCP proxy gateway | High |
| LangGraph | Tool wrapper / callback | Python SDK wrapper | Low |
| OpenAI Agents SDK | Tool endpoint proxy | HTTP redirect | Low |
| Semantic Kernel | `IFunctionInvocationFilter` | .NET + Python filter | Medium |
| AutoGen / AG2 | Tool wrapper + session mapping | Python wrapper + FleetManager | Medium |
| CrewAI | `BaseTool` subclass | Python base class | Low |
| Bedrock Agents | Lambda intercept / API GW policy | Lambda middleware | Medium |
| Dify | Custom tool node / plugin | Dify plugin | Medium |
| Haystack | Pipeline component | Python component | Low |

---

## What this means for PerchGuard's roadmap

Three principles from this landscape:

**1. MCP first.** The MCP proxy is the highest-leverage single integration. Any agent that speaks MCP gets governance for free. This is the moat. It requires the most engineering but it is the one integration that makes PerchGuard framework-agnostic at the protocol level.

**2. Python SDK wrappers are table stakes.** LangGraph, CrewAI, AutoGen, and Haystack all have clean Python wrapper integration points with low effort. A `perchguard-python` SDK that provides `PerchGuardTool`, `PerchGuardBaseTool`, and `PerchGuardFilter` covers four frameworks in one package.

**3. Microsoft is an enterprise distribution channel.** Semantic Kernel + AutoGen together cover the Microsoft enterprise stack. A `perchguard-semantickernel` NuGet package (and the Python equivalent) gets PerchGuard into Azure AI deployments without requiring enterprises to change their orchestration choice.

---

## What PerchGuard does not need to build

PerchGuard is not an orchestration framework. It does not need to compete with LangGraph, CrewAI, or AutoGen. It needs to be the governance layer that drops in front of whichever of these a customer already chose. The integration is always: **before the tool executes, call PerchGuard**. That is the whole surface area. Everything else is distribution.
