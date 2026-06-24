# PerchGuard Security Checklist

Run this after every feature delivery, before closing a branch or opening a PR.
PerchGuard is the trust anchor for every agent it governs — its own security is non-negotiable.

---

## 1. Cryptographic integrity

- [ ] All token/key comparisons use `crypto/subtle.ConstantTimeCompare` — never `==` or `!=`
- [ ] All random generation uses `crypto/rand` — never `math/rand`
- [ ] No hardcoded secrets, tokens, API keys, or passwords anywhere in code or config
- [ ] `grep -r "math/rand" pkg/` returns nothing security-relevant

## 2. Authentication coverage

- [ ] Every `/api/*` endpoint is wrapped with `requireAPIKey` middleware in `RegisterRoutes`
- [ ] Agent-facing endpoints (`/intercept`, `/validate/output`, `/agents/register`) are deliberately unauthenticated and the reason is documented
- [ ] No new endpoint was added without an explicit auth decision — unauthenticated endpoints must justify why

## 3. Input handling

- [ ] All HTTP request bodies are size-bounded (`io.LimitReader` or equivalent)
- [ ] No user-controlled input is passed unsanitized to shell commands, SQL, or file paths
- [ ] Path traversal is blocked on all file operation tools (LeastPrivilegeMutator covers this — verify it still runs)

## 4. Audit integrity

- [ ] Every admission decision (ALLOW, DENY, MUTATE, HUMAN_REVIEW, TERMINATE) reaches `audit()` — no silent drops
- [ ] Error paths in `interceptor.ServeHTTP` that return early without calling `Intercept()` log the rejection (invalid token → logged via the 403 response path is acceptable; verify)
- [ ] The governance snapshot emission path cannot be bypassed by a caller manipulating session state

## 5. Information disclosure

- [ ] Error responses (`writeError`) return only the message string — no stack traces, internal paths, or other session data
- [ ] Audit redaction rules in `configs/policies.yaml` still cover: `password`, `secret`, `api_key`, `token`, `authorization`
- [ ] The auto-generated `pgmk-...` API key is logged at startup and nowhere else — it must not appear in audit records or governance snapshots

## 6. Concurrency

- [ ] Any new shared state introduced in this delivery is protected by an appropriate mutex or atomic
- [ ] `go test -race ./...` passes cleanly

## 7. Dependency hygiene

- [ ] No new external dependencies were introduced without review
- [ ] If a dependency was added: `go mod tidy` was run and `go.sum` is committed
- [ ] `go.sum` was not manually edited

---

## How to run

```bash
# Race detector
go test -race ./...

# Check for math/rand usage
grep -r "math/rand" pkg/ cmd/

# Verify no hardcoded secrets (adjust patterns as needed)
grep -rn "pgat-\|pgmk-\|sk-ant-\|password\s*=" pkg/ cmd/ --include="*.go" | grep -v "_test.go"

# Confirm all /api/* routes go through requireAPIKey
grep -n "mux.Handle\|mux.HandleFunc" pkg/api/server.go
```

---

## Sign-off

After completing the checklist, note it in the PR description or commit message.
If any item cannot be checked (e.g. a deliberate trade-off), document the reason explicitly.
