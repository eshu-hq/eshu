# Collector Evidence Summary Maintainer

## Purpose

Keeps the collector-readiness evidence summary read model reconciled with
the current active fact set via a lease-guarded periodic atomic resweep
(#3466). Moved here from the flat `maintenance` package under #7648.

## Ownership boundary

Owns the resweep loop, the single-owner lease discipline, and the
freshness-guard skip. Does not own the summary table or its resweep SQL
(`storage/postgres`), the collector promotion verdict that reads the
summary, or the `Service` side-runner startup loop in the reducer root.

## Exported surface

- `Maintainer` — the periodic resweep loop
- `Rebuilder` — full-reconcile port
  (`RebuildAllCollectorEvidence`)
- `LeaseManager` — single-owner lease seam
- `FreshnessLookup` — last-materialized watermark guard
- `Domain` — the lease partition name

See `doc.go` for the full contract.

## Dependencies

Standard library only (`context`, `fmt`, `log/slog`, `time`).

Never `internal/reducer`.

## Telemetry

None of its own. The maintainer records a Debug log on each committed
resweep (`collector evidence summary resweep committed` with
`duration_ms`) and an Error log on failure; resweep duration is the
operator's signal for full-reconcile cost against live fact volume.
Inherit the caller's span; no new metric or span.

## Gotchas / invariants

- The maintainer MUST always release its claimed lease (the `defer` in
  `RunOnce`), even on a rebuild error, so a crashed instance never
  blocks takeover beyond the lease TTL.
- Reconciliation is a full idempotent resweep, not incremental
  per-scope dirty tracking, so it cannot miss a change class.
- The default 60s cadence is ~1440x smaller than the 24h collector
  promotion stale verdict, so a one-cadence lag can never flip it.

## Related docs

- `docs/internal/design/collector-readiness-evidence-summary-materialization-design.md`
- `docs/public/observability/telemetry-coverage.md`
