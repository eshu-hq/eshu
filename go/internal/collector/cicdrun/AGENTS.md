# cicdrun Agent Guidance

## Read First

1. `README.md` and `doc.go` for the fixture-backed evidence boundary.
2. `github_actions_fixture.go` for provider fixture normalization.
3. `envelope.go` for fact identity, fencing, and payload construction.
4. `docs/public/guides/collector-authoring.md` for the general collector fact
   contract.

## Local Rules

- Keep this package fixture-backed until the hosted runtime slice is explicitly
  opened.
- Do not add HTTP clients, workflow claims, credential loading, graph writes,
  reducer imports, query imports, logs, metrics, or runtime status here.
- Preserve provider-native IDs and `run_attempt` in fact identity.
- Emit warning facts for partial provider metadata instead of publishing
  complete-looking facts.
- Strip token-bearing URLs before payload, warning text, or source-reference
  emission.
- Do not infer deployment truth from CI success, job names, shell text, or
  environment names.

## Change Rules

- Add providers through fixture parsing and envelope tests in this package.
- Add live API collection only in a future runtime package with credentials,
  request budgets, redaction proof, health/readiness, metrics, and status.
- Check `go/internal/reducer/ci_cd_run_correlation.go` when payload shape
  changes so reducer anchors stay aligned.
