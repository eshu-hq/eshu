# AGENTS.md — internal/collector/cicdrun guidance

## Read First

1. `README.md` — package purpose, exported surface, and invariants.
2. `github_actions_fixture.go` — GitHub Actions fixture normalization.
3. `envelope.go` — fact identity and envelope construction.
4. `docs/docs/adrs/2026-05-15-ci-cd-run-collector.md` — source-truth boundary
   and implementation gates.

## Invariants

- Keep this package fixture-backed until the hosted runtime slice is explicitly
  opened.
- Do not add HTTP clients, workflow claims, credential loading, graph writes,
  reducer imports, query imports, or runtime status here.
- Preserve provider-native IDs and `run_attempt` in fact identity.
- Emit warnings for partial provider metadata instead of silently publishing
  complete-looking facts.
- Strip token-bearing URLs before payload or source-reference emission.
- Do not infer deployment truth from CI success, job names, shell text, or
  environment names.

## Common Changes

- Add a provider by creating provider-specific fixture parsing and envelope
  tests in this package.
- Add live API collection only in a future runtime package with credentials,
  request budgets, redaction proof, health/readiness, metrics, and status.
- If payload shape changes, check `go/internal/reducer/ci_cd_run_correlation.go`
  so reducer anchors stay aligned.
