# AGENTS.md — internal/collector/cicdrun guidance

## Read First

1. `README.md` — package purpose, exported surface, runtime boundary, and
   invariants.
2. `github_actions_fixture.go` — GitHub Actions fixture normalization.
3. `envelope.go` — fact identity and envelope construction.
4. `docs/public/reference/collector-reducer-readiness.md` — source-truth boundary
   and implementation gates.

## Invariants

- Keep hosted provider polling in `ghactionsruntime`; do not move HTTP clients,
  workflow claims, credential loading, or runtime status into the parent
  normalizer package.
- Do not add graph writes, reducer imports, or query imports here.
- Preserve provider-native IDs and `run_attempt` in fact identity.
- Emit warnings for partial provider metadata instead of silently publishing
  complete-looking facts.
- Strip token-bearing URLs before payload or source-reference emission.
- Do not infer deployment truth from CI success, job names, shell text, or
  environment names.

## Common Changes

- Add a provider by creating provider-specific fixture parsing and envelope
  tests in this package.
- Add or change live API collection only in `ghactionsruntime` or another
  runtime subpackage with credentials, request budgets, redaction proof,
  health/readiness, metrics, and status.
- If payload shape changes, check `go/internal/reducer/ci_cd_run_correlation.go`
  so reducer anchors stay aligned.
