# Changelog

All notable changes to PerchGuard are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project has not yet tagged a release; `v0.1.0` below describes the
state of `main` at the point of public release.

## [Unreleased]

## [v0.1.0] — initial public release

### Added

- **Admission pipeline**: per-call interception of agent tool calls
  (`POST /intercept`) with token verification, session quotas, validators,
  and mutators ahead of execution.
- **Validators**: prompt injection, tool authorization, data exfiltration,
  PII/biometric content matching, and an optional LLM-backed semantic
  firewall (`PERCHGUARD_LLM_API_KEY`).
- **Agent fleet management**: session-stateful tracking of registered
  agents, declared intent baselines, and multi-phase drift scoring against
  those baselines.
- **Human-in-the-loop review**: pending-review queue for `HUMAN_REVIEW`
  decisions, with API-key-guarded approve/deny endpoints that write the
  outcome back onto the originating audit record.
- **Audit trail**: durable SQLite-backed audit sink alongside an in-memory
  ring buffer, queryable via the management API and exportable as a
  governance snapshot (`governance.json`).
- **Management API** (`/api/*`): session inspection, audit querying,
  fleet summaries, API key rotation — all behind `Authorization: Bearer
  <PERCHGUARD_API_KEY>`.
- **Compliance export** (`--mode=compliance`): generates EU AI Act Art.26
  deployer-obligations and Annex IV technical-documentation reports
  (Markdown/JSON) directly from the audit trail and policy configuration.
- **Deployment**: Docker Compose, k3s manifests, and an EKS Fargate path
  (Terraform + Helm); CI pipeline for build/test.
- **Documentation**: getting started, configuration reference, deployment,
  agent integration, and red-team guides under `artifacts/docs/`.

### Security

- Token comparisons use `crypto/subtle.ConstantTimeCompare`.
- All random generation (API keys, session tokens) uses `crypto/rand`.
- See [`SECURITY.md`](SECURITY.md) for the vulnerability disclosure policy.
