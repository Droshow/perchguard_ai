# Compliance Export

`--mode=compliance` turns PerchGuard's audit trail and policy file into a
regulatory report — no running server required. It reads the SQLite audit
database and policy YAML directly, then renders a report for the requested
regime/document pair.

Today one regime is registered: **`eu-ai-act`**, with two documents.

## Article 26 — deployer obligations coverage

Eleven rows, one per Art. 26(1)-(11). Each row carries a status
(`configured` / `evidenced` / `placeholder` / `not_applicable` /
`outside_layer`), the evidence behind that status, and the PerchGuard
surface (policy field, audit table, source file) that backs it.

```bash
go run ./cmd --mode=compliance --doc=art26-coverage --format=markdown
```

Rows pulled live from your policy and audit data:

- **(2) Human oversight** — reads `policies.admission.dualApprovalRoles` /
  `policies.humanReview` from the loaded policy.
- **(3) Input data relevance** — reads `parameterSanitization.enabled` and
  counts `MUTATE` decisions in the audit window.
- **(4) Monitoring / suspension on risk** — counts `HUMAN_REVIEW` and
  `TERMINATE` decisions in the audit window.
- **(5) Log retention** — reads `audit.enabled`.

The remaining rows (workplace notification, EU database registration, GDPR
DPIA feed, biometric ID authorisation, individual notification) are
deliberately marked `outside_layer` or `not_applicable` — they're legal or
organizational processes PerchGuard doesn't (and shouldn't) automate. The
report tells you which is which rather than silently omitting them.

## Annex IV — technical documentation

Nine numbered points per Article 11(1), rendered as eleven sections (§2
splits into three sub-sections). Three sections are generated from live
data, one is partial, the rest ship as labeled placeholders pointing at
who owns that content upstream (you, your model vendor, or a separate
legal artifact).

```bash
go run ./cmd --mode=compliance --doc=annex-iv --manifest=./snapshots/agent-manifest.json --format=json
```

| Section | Status | Source |
|---------|--------|--------|
| §1 General description | generated if `--manifest` supplied, else placeholder | `AgentManifest.Mission.Summary` |
| §2(a)-(d),(f),(h) Development process | placeholder | customer/vendor-authored |
| §2(e) Human-oversight assessment | generated | `policies.humanReview` + `HUMAN_REVIEW` decision count |
| §2(g) Validation/testing logs | partial | Phase 5 adversarial-validation results |
| §3 Monitoring/control, limitations | partial | `HUMAN_REVIEW` count + known findings |
| §4 Performance metric appropriateness | placeholder | model-quality territory, out of scope |
| §5 Risk management system (Art. 9) | generated | `agentFleet` config + `pkg/agent` risk thresholds |
| §6 Lifecycle changes | partial | policy version history |
| §7 Harmonised standards | placeholder | out of scope |
| §8 EU declaration of conformity | placeholder | customer-authored |
| §9 Post-market monitoring (Art. 72) | generated | decision counts + session count |

## Flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--regime` | `eu-ai-act` | Regulatory regime. |
| `--doc` | — | `annex-iv` or `art26-coverage`. |
| `--policy` | `$PERCHGUARD_POLICY` or `./configs/policies.yaml` | Policy file to read. |
| `--db` | `store.DefaultDBPath()` | SQLite audit database. |
| `--manifest` | — | Agent manifest JSON; seeds Annex IV §1. |
| `--since` / `--until` | unbounded | RFC3339 bounds on the audit window. |
| `--format` | `markdown` | `markdown` \| `json`. |
| `--out` | stdout | Write to a file instead of stdout. |

## What this is not

These reports are generated from whatever policy and audit data is
currently configured — they are evidence packages, not legal
certification. `placeholder` and `outside_layer` statuses are the report
being honest about what it can't see: model training data, HR processes,
DPIAs, and standards conformity all live outside PerchGuard's runtime
admission layer.

---

← [Docs index](README.md)
