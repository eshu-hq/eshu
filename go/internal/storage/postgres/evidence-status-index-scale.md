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

## status/index domain-backlog regression at collector scale (#3389)

After the cloud/SaaS collectors were layered onto the stack (graph grew to
502,865 nodes; package/vuln/sbom/jira facts and their reducer work items
inflated `fact_work_items`), `GET /api/v0/status/index` regressed back past the
client timeout even though #3373 had already excluded the `fact_records`
collector-fact-evidence and registry-collector sections from the index path.

The new bottleneck is `domainBacklogQuery` (`status_queries.go`), the heaviest
of the three `fact_work_items` aggregates the index path still runs (the others
are `stageCountsQuery` and `queueSnapshotQuery`). It is the only one that, on top
of the shared `active_fact_work_items` CTE, adds `shared_projection_intents` /
`shared_projection_partition_leases` joins, a `UNION ALL`, and a `GROUP BY
domain`. Its `fact_domain_backlogs` aggregate previously grouped the entire
`active_fact_work_items` population by domain, but the `DomainBacklog` struct has
no `succeeded` field: it reports only outstanding, in-flight, retrying,
dead-letter, and failed depth, and its `HAVING` clause discards any domain with
zero non-terminal work. At collector scale the active set is dominated by
terminal `succeeded` rows, so the query paid to materialize and group millions
of rows it then discarded.

The fix bounds the `fact_domain_backlogs` source with `WHERE status IN
('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed')` before
the `GROUP BY domain`. Succeeded and superseded rows contribute 0 to every
`COUNT(*) FILTER (...)` and to the `HAVING`, so the output is row-identical.
Because `active_fact_work_items` is referenced exactly once in this statement,
Postgres (18) inlines the non-recursive CTE and pushes the status predicate down
into the `fact_work_items` scan, so the bound restricts the expensive
`fact_work_items` -> `scope_generations` (x2) -> `ingestion_scopes` anti-join to
the small non-terminal population and lets the planner use
`fact_work_items_stage_domain_status_idx` / `fact_work_items_status_idx` instead
of a full table scan. `stageCountsQuery` and `queueSnapshotQuery` are
deliberately left unchanged because both surface `succeeded` counts and so
legitimately require every status.

No-Regression Evidence: the bound is output-identical (terminal rows are inert
in this aggregate). `go test ./internal/storage/postgres -run
'TestDomainBacklogQueryBoundsWorkItemsToNonTerminalStatuses|TestStatusQueriesUseAggregateFilterSyntax|TestStatusStore'
-count=1` and `go test ./internal/status -count=1` cover the bounded query
shape, the shared-projection backlog union, FILTER placement, and snapshot
assembly. A live 500k-row wall-clock capture was not run in this environment;
the load-bearing proof is the predicate push-down into the indexed
`fact_work_items` scan plus the row-identical-output argument above. The earlier
#3373 measurement (24.1s -> ~1.1s) stands for the `fact_records` sections; this
entry addresses the separately-regressed `fact_work_items` domain-backlog
aggregate.

Observability Evidence: No-Observability-Change. No metric, span, log, status
row, graph write, or queue consumer is added or altered. The status/index
payload (`status`, `repository_count`, `queue.*`, `domain_backlogs`, coordinator
instances) is unchanged; only the rows the domain-backlog aggregate scans are
reduced.
