# #7064 evidence: Postgres-backed gauges off the /metrics scrape path

## Change (Option A, owner-approved)

All five Postgres-backed observable-gauge families now serve from a
background `telemetry/snapshot` refresher instead of querying Postgres inside
the OTEL collection callback:

- reducer (`go/cmd/reducer/postgres_gauge_wiring.go`): queue depth/age +
  source-system variants, shared acceptance rows, workflow family queue depth,
  active generations, poison liveness. `telemetry.Register*` calls unchanged;
  cached adapters implement the same observer contracts.
- ingester (`go/cmd/ingester/queue_gauge_wiring.go`): queue depth/age +
  source-system variants.
- Shared env pair `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL` (5m) /
  `ESHU_POSTGRES_GAUGE_REFRESH_TIMEOUT` (30s), registered in
  `go/internal/envregistry/entries.go` (+ regenerated reference doc).
- `EshuGraphGaugeSnapshotStale` alert generalized to graph + reducer Postgres
  sources (expr excludes `ingester_*` gauges); new
  `EshuIngesterGaugeSnapshotStale` alert (`service: eshu-ingester`) covers the
  ingester sources, so every staleness fire routes to the owning binary.
  Snapshot source names are prefixed per binary (`reducer_*`, `ingester_*`).

Read statements are byte-identical; they run once per interval per source
instead of once per scrape. Worker-pool gauge (in-memory) stays on the
scrape. Series names, labels, and cardinality are identical by construction
(flatten round-trip; NUL separator is collision-free because Postgres text
cannot hold NUL).

## Operator-visible signal proven in test

New regression tests (all in the worktree, run with `-count=1`):

- `TestPostgresQueueGaugesDoNotBlockMetricsCollection` (reducer) and
  `TestIngesterQueueGaugesDoNotBlockMetricsCollection` (ingester): a Postgres
  observer that blocks until ctx-done; `ManualReader.Collect` returns while
  the read is stuck. RED before the fix (`undefined:
  registerPostgresBackedGauges`); GREEN after.
- `TestPostgresBackedGaugesServeRefreshedSnapshot`: scripted readings for all
  five families; scrape serves exact values incl. nested labels and float ages
  (1.5s, 2.5s, 3.25s via millis encoding). Mutation check: breaking the
  flatten separator fails this test (5s deadline), then reverted.
- `TestPostgresScalarGaugesReportNothingBeforeFirstRefresh`: scalar gauges
  observe nothing (not false zero) pre-first-snapshot.
- `TestTwoGaugeRefreshersShareSnapshotInstruments`: graph + Postgres
  refreshers register identical `eshu_dp_gauge_snapshot_*` instruments on one
  meter; both families report.

## No-regression evidence

- `go test ./cmd/reducer/ ./cmd/ingester/ ./internal/envregistry/ -count=1`:
  all ok.
- `go build ./cmd/reducer/ ./cmd/ingester/`, `go vet`: clean.
- `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
  scripts/verify-telemetry-coverage.sh`: agree, no new untracked stages.
- No-Regression Evidence (SQL): no statement text changed; the same store
  methods run on refresher goroutines instead of collection callbacks, so no
  EXPLAIN/plan evidence is owed. No queue/lease/claim semantics touched. No
  throughput or latency improvement is claimed: Postgres read load becomes
  constant per interval instead of scaling with scrape frequency, which is a
  load-shape change, not a measured speedup.
- Observability Evidence: refresh outcome/age signals
  (`eshu_dp_gauge_snapshot_refreshes_total{outcome=success|error|timeout}`,
  `eshu_dp_gauge_snapshot_age_seconds`) plus WARN logs cover every refresh;
  `EshuGraphGaugeSnapshotStale` covers staleness for the new sources.
