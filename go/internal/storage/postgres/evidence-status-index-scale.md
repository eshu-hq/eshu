# Status index and ingester snapshot scale evidence

Tracks the read-path performance fix for `GET /api/v0/status/index` (#3373,
closes #3368) and the ingester status surface (`GET /api/v0/status/ingesters`,
`GET /api/v0/status/ingesters/{ingester}`) (#3377, follow-up to #3368).

## Problem

`StatusStore.ReadStatusSnapshot` unconditionally ran every optional section,
including two aggregates over the active `fact_records` table:
`readCollectorFactEvidence` (full GROUP BY over all active facts) and
`readRegistryCollectorSnapshots`. The `getIndexStatus`, `listIngesters`, and
`getIngesterStatus` handlers render neither section, so those surfaces paid
~20s of work they discarded. At ~900-repo scale the index and ingester
endpoints aggregated ~4.5M `fact_records` (~9.4 GB), taking 24-70s or timing
out.

## Change

`ReadStatusSnapshotFiltered(ctx, asOf, SnapshotSelection)` gathers only the
requested optional sections. `getIndexStatus`, `listIngesters`, and
`getIngesterStatus` exclude collector fact evidence and registry collectors;
they render only health, queue, coordinator-instance, stage, and backlog
fields, none of which derive from the excluded `fact_records` sections. Every
full-report surface (CLI/admin status, collector readiness, pipeline,
freshness, hosted readiness) keeps the back-compatible full selection via
`ReadStatusSnapshot`.

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
fact evidence. The ingester handlers request the filtered selection and still
render correct health/queue/coordinator output, proven by
`TestListIngestersRequestsFilteredSelection` and
`TestGetIngesterStatusRequestsFilteredSelection`. Covered by
`go test ./internal/storage/postgres -run Status`,
`go test ./internal/status`, and
`go test ./internal/query -run 'Status|Index|Ingester'`.

## No-Observability-Change

No metric, span, log, status row, graph write, or queue consumer is added or
altered. The change only gates which existing read queries run for the index
and ingester surfaces; the omitted sections were already absent from those
payloads.
