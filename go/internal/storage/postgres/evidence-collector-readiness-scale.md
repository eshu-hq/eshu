# Collector readiness fact-evidence scale evidence

Tracks the read-path performance fix for `GET /api/v0/status/collector-readiness`
(issue [#3375](https://github.com/eshu-hq/eshu/issues/3375)).

## Problem

`readCollectorFactEvidence` (`status_collector_evidence.go`) builds collector
readiness evidence from a `fact_summary` CTE. The pre-fix CTE joined every active
`fact_records` row for every active scope and then ran one global
`GROUP BY scope_id, generation_id, evidence_source, source_system`. The outer
`LIMIT 200` does not bound that inner aggregate, so the query had to sort all
active facts at once. Unlike `status/index` ([#3368](https://github.com/eshu-hq/eshu/issues/3368)/[#3373](https://github.com/eshu-hq/eshu/issues/3373)),
collector readiness genuinely needs the evidence, so it cannot skip the section;
it needs an evidence-preserving optimization.

## Root cause (EXPLAIN ANALYZE)

Measured against the live e2e Postgres (`eshu-remote-e2e-postgres-1`, Postgres
18.4, `work_mem=16MB`) holding 249,827 `fact_records` rows; 140 active scopes;
111,609 active facts scanned to produce 266 evidence groups (a ~420:1
scan-to-output ratio). The active-fact scan already used the existing partial
index `fact_records_collector_status_active_idx`, so the index was not missing.

The dominant cost was the global aggregate, not the scan: the GROUP BY sorted all
111,609 rows with `Sort Method: external merge  Disk: 18728kB`
(`temp read=2341 written=2342` blocks of disk I/O). The work is `O(active_facts)`
and spills once the row set exceeds `work_mem`. On the issue's 4.5M-row stack the
same sort spills roughly 18x more (~340MB), which is the reported ~20s.

A missing index is not the cause and an added index cannot fix it: an aggregate
that must visit every active fact row to count it cannot be index-bounded. A
reshape that grouped in index-column order also still spilled, because the outer
scope iteration is not globally sorted.

## Change

The `fact_summary` CTE now aggregates each active scope's facts inside a
per-scope `JOIN LATERAL (... GROUP BY evidence_source, source_system) ON TRUE`
subquery. Each per-scope aggregate is bounded to one scope's facts (avg ~800
rows), so it stays in memory and never spills. The surrounding
`active_scopes`/`workflow_instances` CTEs and the final
`SUM`/`MAX`/`ARRAY_AGG` aggregate are unchanged, so the emitted rows are
byte-identical to the previous shape.

## Accuracy / evidence preservation

Original and LATERAL queries were executed in one `REPEATABLE READ` transaction
(frozen snapshot) and their full ordered output (`\copy ... FORMAT csv`) was
diffed: 27 rows each, **byte-identical** (`diff` empty). The live stack ingests
continuously, so a non-transactional diff drifts only by fresh rows/timestamps,
not by query shape. Unit equivalence is covered by
`TestReadCollectorFactEvidenceUsesBoundedActiveFactMetadata`; the per-scope
LATERAL shape is pinned by `TestCollectorFactEvidenceQueryAggregatesFactsPerScope`.

## Performance Evidence

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of the full collector-readiness query against
`eshu-remote-e2e-postgres-1`, three consecutive runs each:

| Run | Before (global GROUP BY) | After (per-scope LATERAL) |
| --- | --- | --- |
| 1 | 187.1 ms | 60.5 ms |
| 2 | 124.5 ms | 71.1 ms |
| 3 | 161.2 ms | 49.3 ms |

- Before: every run reports `Sort Method: external merge  Disk: 18728kB` with
  `temp read=2341 written=2342`.
- After: **no external merge sort and zero temp blocks** — the per-scope
  HashAggregate fits in memory (~32kB per scope).

This local stack holds 249,827 rows, so it shows ~2.5-3.3x at 18MB of spill. The
load-bearing result is the elimination of the on-disk sort: the after-plan never
spills regardless of active-fact count, whereas the before-plan's spill grows
linearly with active facts (~340MB / ~20s on the 4.5M-row stack in the issue).
Classification: wall-clock win that removes an unbounded-with-scale cost.

Scope caveat: the eliminated spill was the dominant `O(active_facts)` cost
(scale-linear in total fact count). The residual outer `GROUP BY` + `ORDER BY`
over `fact_summary` rows scales instead with active-scope x source_system
cardinality — on the order of hundreds to low-thousands of rows (266 here), far
below `work_mem` — and was not EXPLAIN-measured at the issue's 4.5M-fact /
906-scope target. It is algebraically bounded well under the spill threshold,
not measured at that scale; the load-bearing, fact-count-linear spill is what
the proof above definitively removes.

## No-Observability-Change

No-Observability-Change: no metric, span, log, status row, graph write, or queue consumer is added or
altered. Only the `fact_summary` CTE shape changed; the read still flows through
the shared `s.queryer` and the existing HTTP request metrics on
`/api/v0/status/collector-readiness` continue to expose endpoint latency to an
operator. The omitted on-disk sort is visible to operators via the same endpoint
latency and Postgres `temp_bytes` / `pg_stat_database` counters.

## Wire contract

Unchanged. The response shape, ordering (`LIMIT 200`), and evidence values are
identical, so no OpenAPI or HTTP API reference change is required.
