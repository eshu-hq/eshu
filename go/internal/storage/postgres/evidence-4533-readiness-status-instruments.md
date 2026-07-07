# #4533 — wire StatusStore.Instruments on the readiness-probe path

Follow-up to #4446/#4530 (PR #4534). That change wired the shared
meter-provider `*telemetry.Instruments` onto the `StatusStore` used by the
API and MCP server's operator status-serving surface (`cmd/api`,
`cmd/mcp-server`), so `eshu_dp_status_stage_counts_cache_total` (recorded in
`status_stage_counts_cache.go`'s `listStageCounts`) emits on the `/status`
snapshot path.

## Gap

Every other binary that constructs a `StatusStore` for its
readiness/liveness surface (`internal/runtime/readiness.go` ->
`ReadRawSnapshot` -> `ReadStatusSnapshotFiltered` -> `listStageCounts`) called
`postgres.NewStatusStore(...)` directly, which deliberately leaves
`Instruments` nil for source-compatibility. Every readiness probe on those
processes exercised the exact same cache code path — but the counter never
emitted there.

## Fix

Added `postgres.NewInstrumentedStatusStore(queryer, instruments)` next to the
existing `NewStatusStore` in `go/internal/storage/postgres/status.go`. It
wraps `NewStatusStore` and assigns `Instruments`, mirroring the pattern
`cmd/api` and `cmd/mcp-server`'s `newStatusStore` helper already used, but as
one shared, testable constructor in the owning package instead of a
per-binary reimplementation. Every readiness-probe call site that already had
`instruments` in scope (each binary builds it via `telemetry.NewInstruments`
before opening Postgres) was switched from `NewStatusStore` to
`NewInstrumentedStatusStore`:

- `cmd/ingester`, `cmd/reducer`, `cmd/projector`, `cmd/scanner-worker`,
  `cmd/workflow-coordinator`, `cmd/webhook-listener` (hand-written binaries).
- The shared `internal/collector/entrypoints/generate.go` `mainTemplate`,
  which is the single source generating `cmd/collector-jira` and
  `cmd/collector-pagerduty`'s `main.go` via
  `scripts/generate-collector-entrypoints.sh`.
- The remaining ~19 `cmd/collector-*` binaries (aws-cloud, azure-cloud,
  cicd-run, component-extension, confluence, gcp-cloud, git, grafana,
  kubernetes-live, loki, oci-registry, package-registry, prometheus-mimir,
  sbom-attestation, security-alerts, tempo, terraform-state, vault-live,
  vulnerability-intelligence), which predate the generator and are
  hand-maintained copies of the same shape.

`cmd/admin-status` and `cmd/eshu` (`local_host_progress.go`) are out of scope:
neither builds a telemetry meter or exposes a readiness/liveness probe — both
are one-shot CLI tools, so there is no probe path for the counter to attach
to.

## Observability

Observability Evidence: No new metric. `eshu_dp_status_stage_counts_cache_total`
  already has its X1 row in `docs/public/observability/telemetry-coverage.md`
  ("status stage-counts cache", pointing at
  `status_stage_counts_cache.go:121`). This change widens which processes
  emit that existing signal — the readiness-probe path on ~25 additional
  `cmd/*` binaries now records cache hit/miss the same way the API/MCP status
  path already did, so an operator reading that counter's `result` label sees
  readiness-probe traffic too, not only `/status` snapshot traffic.

## No-Regression

No-Regression Evidence: `TestNewInstrumentedStatusStoreWiresInstruments` proves the
  wiring (RED without the `store.Instruments = instruments` assignment,
  GREEN with it — verified locally by temporarily reverting the assignment).
  `TestNewInstrumentedStatusStoreAllowsNilInstruments` proves a nil
  `*telemetry.Instruments` stays a no-op. Full build (`go build ./...`) and
  the focused package suites for every touched binary plus
  `internal/storage/postgres`, `internal/collector/entrypoints`,
  `internal/app`, and `internal/runtime` pass locally (see PR description for
  the exact commands and counts).
