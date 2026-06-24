# Getting Started

PerchGuard is an admission controller for AI agents. Every tool call an agent makes passes through PerchGuard before it executes. PerchGuard checks it against policy, decides ALLOW / DENY / MUTATE, and writes an audit record.

This guide gets you from zero to your first intercepted tool call in under 10 minutes using Docker Compose.

---

## Prerequisites

- Docker + Docker Compose
- An Anthropic API key (optional — only needed if you want the semantic firewall)

---

## 1. Clone and configure

```bash
git clone https://github.com/perchguard/PerchGuard_AI.git
cd PerchGuard_AI/perchguard
cp .env.example .env   # create if missing: touch .env
```

If you have an Anthropic key and want the semantic firewall:

```bash
echo "ANTHROPIC_API_KEY_USED_BY_PERCHGUARD=sk-ant-..." >> .env
```

Leave `.env` empty to run without the semantic firewall — all other validators still work.

---

## 2. Start PerchGuard

```bash
docker compose -f deployments/docker-compose/docker-compose.yaml up perchguard --build
```

PerchGuard is ready when you see:

```
[perchguard] enforcement mode: OBSERVE — decisions logged but not enforced
[perchguard] management API key: pgmk-...
```

> **Note the API key** — you need it for management endpoints. It is printed once at startup.

Health check:

```bash
curl http://localhost:8080/healthz
# {"status":"ok","service":"perchguard"}
```

---

## 3. Send your first tool call

```bash
curl -s -X POST http://localhost:8080/intercept \
  -H "Content-Type: application/json" \
  -d '{
    "uid": "test-001",
    "session_id": "my-session",
    "agent_id": "my-agent",
    "agent_role": "developer_agent",
    "user_intent": "read the config file",
    "tool_call": {
      "name": "read_file",
      "parameters": {"path": "/workspace/config.yaml"}
    }
  }' | jq .
```

Response:

```json
{
  "uid": "test-001",
  "decision": "ALLOW",
  "reason": "all checks passed"
}
```

Now try something the policy blocks:

```bash
curl -s -X POST http://localhost:8080/intercept \
  -H "Content-Type: application/json" \
  -d '{
    "uid": "test-002",
    "session_id": "my-session",
    "agent_id": "my-agent",
    "agent_role": "developer_agent",
    "user_intent": "clean up temp files",
    "tool_call": {
      "name": "bash",
      "parameters": {"command": "rm -rf /tmp/work"}
    }
  }' | jq .
```

Response (in observe mode, decision is ALLOW but `observed_decision` records what would have fired):

```json
{
  "uid": "test-002",
  "decision": "ALLOW",
  "reason": "all checks passed"
}
```

The audit log (`snapshots/audit.jsonl`) shows the real decision:

```bash
tail -1 snapshots/audit.jsonl | jq '{decision, observed_decision, tool, reason}'
```

---

## 4. Watch the dashboard

In a second terminal:

```bash
PERCHGUARD_API_KEY=pgmk-...  # key from startup log
go run cmd/main.go --mode=watch --watch-key=$PERCHGUARD_API_KEY
```

Or if you built the binary:

```bash
./perchguard --mode=watch --watch-key=pgmk-...
```

---

## 5. Switch to enforce mode

When you have reviewed the audit log and are ready to start blocking:

Edit `configs/policies.yaml`, change line 1:

```yaml
enforcement: enforce   # was: observe
```

PerchGuard hot-reloads within 30 seconds — no restart needed.

---

## Next steps

| Goal | Read |
|------|------|
| Understand every policy field | [configuration-reference.md](configuration-reference.md) |
| Deploy to Kubernetes or EKS | [deployment.md](deployment.md) |
| Wire an existing agent | [agent-integration.md](agent-integration.md) |
| Run an adversarial exercise | [red-team-guide.md](red-team-guide.md) |
| Contribute to PerchGuard | [../CONTRIBUTING.md](../CONTRIBUTING.md) |
