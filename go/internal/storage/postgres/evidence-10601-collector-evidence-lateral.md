# Evidence: #10601 collector-evidence lateral lookup — issue eshu-hq/eshu#10601

## Symptom

`GET /admin/status` (`?format=text` and `?format=json`) returned 500 on QA
under NVD-backlog load. The runtime snapshot reader bounds each snapshot to
5s (`statusSnapshotLimit` in `go/internal/runtime/postgres/status_reader.go`);
the collector-evidence section alone cost ~13.8s, so the server cancelled its
own section query and the masked 500 (`PostgreSQL reader query failed`) fired.

## Root cause (measured, QA primary `eshu-postgresql-0`, Postgres 18.3)

`collectorFactEvidenceQuery`
(`go/internal/storage/postgres/status_collector_evidence.go`) built the
collector-instance leg as `workflow_instances AS MATERIALIZED (DISTINCT ON
...)` over `workflow_work_items` joined to `active_scopes`, then LEFT JOINed
it to the summary aggregate. The outer join cannot use an index (CTE scan),
so every one of the 9730 summary rows re-scanned all 4567 CTE rows.

Input shape at measurement: 5372 active scopes, 9730
`collector_evidence_summary` rows, 4567 instance groups, 29,229
`workflow_work_items` rows (completed 27,398), 312,097 `fact_work_items`,
140.3M `fact_records` (untouched by this read).

A lateral leg hidden in its own CTE was tried and rejected: the planner
inlines the single-reference CTE and re-scans `active_scopes` per outer row
(28.9M-row plan shape, 22+ min on QA).

## Fix

The lateral lookup (`LEFT JOIN LATERAL ... LIMIT 1` over
`workflow_work_items` in DISTINCT ON key and `ORDER BY`) sits in the main
query before the summary fanout, so each active scope costs exactly one probe
of `workflow_work_items_collector_scope_generation_updated_idx`. WHERE,
key, ORDER BY, GROUP BY, ORDER BY, and LIMIT 200 are unchanged.

## Performance Evidence: section query on QA

`EXPLAIN (ANALYZE, BUFFERS)` of the full section query on QA, same input
shape (5376–5385 active scopes between runs):

| Case | Before | After |
|---|---|---|
| Section execution time | 13,812 ms | 3,540 ms cold / 1,121 ms warm |
| Dominant operator | 44,420,378-row CTE-scan join filter | 9,748 indexed probes (`loops=9748`) |
| Output rows | 43 | 43, byte-identical (`diff` empty) |

Row exactness: the 43-row QA outputs diff empty, and
`TestCollectorFactEvidenceLateralMatchesPreChangeJoinLive` diffs the new
query against the verbatim pre-change join over blank-latest fallback,
`updated_at` tiebreak, empty-instance, and superseded-generation branches.

Steady state (~1.1s warm) plus the remaining sections (~1.2s) fits the 5s
snapshot budget with margin; the collector-readiness route shares this read
and heals with it.

## No-Observability-Change: read label and call site untouched

The `statusReadCollectorFactEvidence` read label and call site
(`StatusStore.ReadStatusSnapshotFiltered`) are untouched; no span, metric, or
log shape changed. `telemetry-coverage` gate passes with no new untracked
stages.
