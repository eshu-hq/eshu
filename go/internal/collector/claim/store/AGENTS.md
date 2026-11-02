# AGENTS.md — internal/collector/claim/store guidance

Read first: `README.md`, `select.go`, `url.go`, and
`docs/internal/design/8875-claim-plane-collector-wiring.md`.

## Invariants

- Unset or blank `ESHU_WORKFLOW_CLAIM_PLANE_URL` returns the injected Postgres
  store unchanged. Do not wrap it, decorate it, or build a second one: every
  existing deployment depends on that path being identical.
- Never fall back from the claim plane to Postgres, on any error. A silent
  fallback would split claim authority across two databases.
- Never echo the raw URL in an error or log. It may carry a credential the
  operator pasted by mistake; log only scheme and host.
- Keep the request timeout below the default heartbeat interval
  (`TestNewClaimPlaneClientHasBoundedTimeout`).
- Do not add a client retry. `ClaimNext` is not idempotent, and the runner's
  restart path already handles an outage.
- Keep labels closed: `backend_kind`, `operation`, `outcome` only. No scope,
  item, claim, or instance id becomes a label.

## Out of scope

The ingestion commit fence (`ValidateAndPinClaimForCommit`, #8875 P2) lives in
`go/internal/storage/postgres`. Do not add commit-path logic here.

## Verification

```bash
cd go && go test -race ./internal/collector/claim/store/ ./internal/workflow/ \
  ./cmd/collector-grafana/ ./cmd/collector-pagerduty/ ./cmd/collector-package-registry/
```
