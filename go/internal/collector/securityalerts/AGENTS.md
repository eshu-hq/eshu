# AGENTS.md — internal/collector/securityalerts guidance

## Read First

1. `README.md` — package purpose, exported surface, and invariants.
2. `envelope.go` — fact identity, payload shaping, and URL sanitization.
3. `github_client.go` — GitHub request guardrails and allowlist behavior.
4. `docs/public/reference/collector-reducer-readiness.md` — source-truth
   boundary and runtime gate.
5. `go/internal/reducer/security_alert_reconciliation.go` — reducer consumer
   for emitted provider alert facts.

## Invariants

- Keep provider alerts as source facts. Do not emit canonical Eshu impact truth
  from this package.
- Do not add workflow claims, database commits, graph writes, query imports, or
  runtime status here.
- Preserve provider-native alert ID/number, state, dependency metadata,
  advisory IDs, vulnerable range, patched version, severity, CVSS, EPSS, CWE,
  timestamps, and source URL.
- Strip token-bearing query parameters from payload and source-reference URLs.
- Require an explicit repository allowlist before any hosted provider request.
- Keep synthetic fixture coverage ahead of payload shape changes.

## Common Changes

- Add another Dependabot field by first extending fixture coverage in
  `dependabot_envelope_test.go`, then extending payload construction.
- Add another provider in a provider-specific file with source-native DTOs and
  envelope tests; do not mix provider schemas.
- Add hosted runtime work only in a future package or slice with request
  budgets, rate-limit handling, metrics, status, workflow claims, and
  deployment docs.
- If package identity changes, check `internal/packageidentity` and the
  security alert reconciliation reducer before editing.
