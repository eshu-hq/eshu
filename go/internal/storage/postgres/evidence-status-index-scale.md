# Status index snapshot scale evidence

Tracks the read-path performance fix for `GET /api/v0/status/index` (issue
[#3368](https://github.com/eshu-hq/eshu/issues/3368)).

## Problem

`StatusStore.ReadStatusSnapshot` unconditionally ran every optional section,
including two aggregates over the active `fact_records` table:
`readCollectorFactEvidence` (full GROUP BY over all active facts) and
`readRegistryCollectorSnapshots`. The `getIndexStatus` handler renders neither
section, so the index surface paid ~20s of work it discarded.

## Change

`ReadStatusSnapshotFiltered(ctx, asOf, SnapshotSelection)` gathers only the
requested optional sections. `getIndexStatus` excludes collector fact evidence
and registry collectors; every full-report surface (CLI/admin status, collector
readiness, pipeline, freshness, hosted readiness) keeps the back-compatible full
selection via `ReadStatusSnapshot`.

## Performance Evidence

Backend NornicDB + Postgres 18, local Docker Compose, default stack indexing
906 repositories: 4,547,692 `fact_records` rows (9.4 GB table), 481,728 graph
nodes, reducer drained to ~210 pending.

- Before: `GET /api/v0/status/index` measured 24.1s on a warm call and timed out
  past 70s on a cold call (`curl` exit 28, 0 bytes). A scope-grouped aggregate
  over `fact_records` measured 20.7s in isolation.
- After: three consecutive calls returned HTTP 200 in 1.06s, 1.12s, and 1.70s
  with the core payload intact (`status`, `repository_count=906`,
  `queue.succeeded=10279`, coordinator instances). ~20-40x faster; the endpoint
  no longer exceeds the client timeout.

## No-Regression Evidence

The full status-report path is unchanged: `ReadStatusSnapshot` delegates to
`ReadStatusSnapshotFiltered(FullSnapshotSelection())`, so CLI status, admin
status, and collector readiness still include registry collectors and collector
fact evidence. Covered by
`go test ./internal/storage/postgres -run Status`,
`go test ./internal/status`, and `go test ./internal/query -run 'Status|Index'`.

## No-Observability-Change

No metric, span, log, status row, graph write, or queue consumer is added or
altered. The change only gates which existing read queries run for the index
surface; the omitted sections were already absent from the index payload.
