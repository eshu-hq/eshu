# CI/CD Run Watermark

## Purpose

`runwatermark` defines the claim-fenced watermark contract the GitHub Actions
runs poller (`ghactionsruntime`) uses to detect a cross-cycle collection gap
(#5429). Every claim fetches a bounded, stateless window of the most recent
runs (`max_runs`); when more than `max_runs` runs land between two claim
cycles, the fetched window's oldest run can be newer than the previous
cycle's newest observed run, meaning every run between them was never
fetched by either cycle. A watermark stores that previous cycle's newest
observed run ID so the poller can compare against it and detect the gap
instead of silently losing the runs in between.

## Ownership boundary

This package owns the watermark data contract (`Key`, `Watermark`, `Store`,
fencing semantics) and one in-process implementation (`InMemoryStore`). It
does not own gap-detection logic, GitHub Actions polling, fact construction,
or telemetry -- those live in
`go/internal/collector/cicdrun/ghactionsruntime` (see
`run_watermark.go` there). It does not own durable Postgres persistence
either; a durable `Store` implementation lives in
`go/internal/storage/postgres` (`CICDRunWatermarkStore`) and is wired
through `SourceConfig.Watermarks` by the hosted command.

## Exported surface

See `doc.go` for the godoc contract. Callers use:

- `Key` to identify one polled `(scope_id, repository)` target.
- `Watermark` to carry the newest observed run ID, the claim that wrote it,
  and the fencing token that protects it from regression.
- `Store` to Load and Save watermarks with fencing.
- `ErrStaleFence` to detect a rejected, superseded write.
- `InMemoryStore` for tests and for interim single-process gap detection.

## Dependencies

The package has no dependency on `ghactionsruntime`, `workflow`, or any
provider client -- it depends only on the standard library. This keeps the
watermark contract usable by both the runtime poller and any future
Postgres-backed store without an import cycle.

## Telemetry

This package emits no metrics or spans directly. Gap-detection outcomes are
recorded by `ghactionsruntime` (`eshu_dp_ci_cd_run_partial_generations_total{reason="runs_backfill_gap"}`);
a durable Postgres-backed `Store` records its own load/save telemetry the
same way `AWSPaginationCheckpointStore` does.

## Gotchas / invariants

- One `Key` maps to exactly one watermark row. Unlike
  `awscloud/checkpoint.Key`, there is no `ResourceParent`/`Operation`
  dimension: a GitHub Actions target has exactly one runs listing to track,
  so `Scope` and `Key` are not split into separate types the way the AWS
  pagination checkpoint contract splits them.
- `Save` rejects a fencing token strictly older than the stored row
  (`ErrStaleFence`). A fencing token EQUAL to the stored row's succeeds --
  this is the idempotent-redelivery case (a retried claim that already
  wrote this exact watermark).
- `InMemoryStore` has no durability across process restarts or visibility
  across collector replicas. It narrows, but does not close, the gap the
  Postgres-backed store closes; see `CICDRunWatermarkStore` in
  `go/internal/storage/postgres`.
- `LastRunID` is a decimal string (GitHub Actions run IDs), not a numeric
  type, to avoid a redundant parse/format round trip at the storage
  boundary; comparison as an integer happens in `ghactionsruntime`, which is
  the only caller that needs ordering semantics.

## Related docs

- `go/internal/collector/awscloud/checkpoint/README.md` -- the pagination
  checkpoint pattern this contract mirrors.
- `go/internal/collector/cicdrun/ghactionsruntime/README.md` -- the poller
  that reads and writes watermarks.
