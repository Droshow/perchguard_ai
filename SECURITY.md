# Security Policy

PerchGuard is an admission controller for AI agents — it sits in the request
path for every tool call an agent makes. A vulnerability here can mean a
governance bypass, not just a broken feature. We take reports seriously and
will work with you on disclosure timing.

This document covers reporting a vulnerability you've found in PerchGuard
itself. For the checklist contributors run before merging a change, see
[`SECURITY_CHECKLIST.md`](SECURITY_CHECKLIST.md).

## Supported versions

PerchGuard is pre-1.0. Security fixes land on `main` and the latest tagged
release. There is no long-term support branch yet.

| Version | Supported |
|---------|-----------|
| latest `main` | yes |
| tagged releases | latest only |

## Reporting a vulnerability

**Do not open a public GitHub issue for a security report.**

Email **devsbridge1@gmail.com** with:

- A description of the vulnerability and its impact
- Steps to reproduce (a minimal repro or PoC is ideal)
- The affected version/commit

We aim to acknowledge reports within 72 hours and to provide a remediation
timeline within 7 days. If you'd like recognition, let us know how you'd
like to be credited once a fix ships — we default to crediting reporters in
the release notes unless asked not to.

## Scope

In scope:

- The admission pipeline (`pkg/admission/`) — validators, mutators, quota
  checks
- The management API (`pkg/api/`) — authentication, authorization
- Audit storage and integrity (`pkg/store/`)
- Agent fleet / session state (`pkg/agent/`)

Out of scope:

- Vulnerabilities in third-party dependencies (report upstream; we'll track
  the update)
- Issues requiring an already-compromised `PERCHGUARD_API_KEY` or LLM API
  key — those are deployment secrets, not a code-level finding
- Denial-of-service via resource exhaustion (track as a regular issue
  instead)
