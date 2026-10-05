# Status active work: grouped history before the generation join (#7009)

## Scope and cause

`StatusStore.ReadStatusSnapshotFiltered` reads stage counts, domain backlog,
queue state, conflict blockage, and the latest failure from one statement,
`activeWorkSummaryQuery` (`read="active_work_summary"`). Before this change, the
statement joined every `fact_work_items` row (about 254,000 on the ops-qa
capture of 2026-10-05) to its scope state and generation. It then materialized
the joined set and counted `fact_work_items` a second time for `total_count`.
Most rows are succeeded history, and the summary reads them only as
`(stage, status)` counts and `succeeded_count`. The C3 shim
(`7009-c3-shim-report-20261005.md`) attributed most of the cost to per-row CPU:
two wide hash joins, a 254,000-row CTE, rescans of that CTE, and the second
count. I/O was not the main cost.

## Change and semantics

- `active_fact_work_items` now reads only the six statuses a section reads in
  detail (`activeWorkDetailStatuses`: pending, claimed, running, retrying,
  failed, dead_letter). The fence, joins, stale-generation predicate, and
  conditional projection are unchanged.
- `fact_work_status_groups` counts every work row once, grouped by
  `(scope_id, generation_id, stage, status)`.
- `fact_work_history_counts` applies the same scope-state and
  `(scope_id, generation_id)` generation join to each group of the other
  statuses. Each group is keyed on both join columns, so the join keeps or
  drops the same rows as the per-row join. The stale-generation predicate
  applies only to four of the six detail statuses, so history rows need only
  the joins.
- Stage counts add the two disjoint halves. `succeeded_count` comes from
  grouped history. `total_count` is the sum of the grouped pass. Like the old
  `COUNT(*)`, it counts every row with no join, in the same statement and
  snapshot.
- The backlog, blockage, and failure sections are unchanged. No schema, index,
  lock, lease, claim, or write change. No migration.

The Go render of the new query is byte-identical to the measured shim candidate
`cand_a2.sql` (SHA-256 `bcd5846afddbe540f09b61ad50911a19fe9baf3360c5e0d314c33c8ab17aae40`).

## Proof

- Oracle: `activeWorkSummaryPreHistoryGroupsOracle` is the old query, rendered
  from origin/main `9bcca588f` by a temporary package test. It is pinned to the
  shim baseline SHA-256 `bef1078e2f5b971f6f35f576403b5c7197b59b6e37ccf13974d3240e4d432e2e`.
  `TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups` undoes exactly
  the grouped-history pieces and gets the oracle back, byte for byte.
- Differential: `TestActiveWorkSummaryMatchesPreHistoryGroupsOracle` runs the
  oracle and the shipped query as two CTEs of one statement on PostgreSQL 18.3.
  It compares the section rows with `EXCEPT ALL` in both directions.
  - Edge cases: the ported shim edge cases (empty; stale and equal-time
    generations with a composite mismatch; null and dangling active pointers;
    queue, failure, and provenance; backlog order and a lease-only domain;
    blockage leases and readiness). It also adds a history fence case for
    succeeded, superseded, and quarantined rows on stale and mismatched
    generations.
  - Fixtures: a busy shape with about 20% live rows (3,000 rows), and the
    #6794 semantics fixture.
  - Every case is equal, and the edge cases also match hand-derived values.
- Seeded faults: `TestActiveWorkSummaryOracleCatchesSeededHistoryMutations`
  requires each of these to differ from the oracle on at least one case:
  dropping `failed` or `claimed` from the status partition; dropping
  `retrying` from the detail filter only; dropping the stale-generation
  predicate; joining history generations without the scope key; counting
  `total_count` from the joined rows; counting `succeeded_count` from the
  unjoined groups.

## Generation scan guard

`TestStatusActiveFactWorkItemsCTEUsesGenerationIndex` (#4446) used to reject
any `Seq Scan on scope_generations` in the summary plan. On its fixture
(2,000 scopes, 100,000 generations), the grouped history join hashes
`fact_work_history_counts` against one full pass over `scope_generations`.
That pass is about 4 ms, and it runs once per statement, not once per work row.
The shim's baseline arm already full-scanned `scope_generations` at ops-qa
shape. The guard now uses `checkSummaryGenerationScans`, which enforces three
rules:

- `active_fact_work_items`, the per-work-row join, must never full-scan
  `scope_generations`.
- `fact_work_history_counts` may hash at most one pass.
- No other CTE may full-scan `scope_generations`.

`TestCheckSummaryGenerationScansRejectsDetailFullScan` pins those rules on
fixed plan text. The live guard also re-plans with index scans disabled. It
requires the detail rule to reject that seeded full scan.

## Performance Evidence:

LOCAL FIXTURE evidence only, from the C3 shim. It is not ops-qa, and it makes no
endpoint p95 claim. The setup was a disposable PostgreSQL 18.3 primary and
streaming hot standby, with all 178 migrations applied and seeded to the ops-qa
row counts: 821 scopes, 25,620 generations, and 254,222 work rows. Statements
ran on the standby with `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)`. Medians are of
5 samples, with the first mover rotating each repetition. The host was shared
(load1 6.6 to 21.9), so buffers are the comparable metric and absolute
milliseconds are not.

| fixture state | baseline median (ms) | grouped history median (ms) | buffers baseline / grouped |
| --- | ---: | ---: | --- |
| ops-qa-like, stale VM, `work_mem=64MB` | 687.0 | 201.5 | 191,276 / 141,573 |
| ops-qa-like, stale VM, `work_mem=4MB` | 550.1 | 171.3 | 191,276 / 141,573 |
| vacuumed, no index | 409.0 | 126.7 | 59,728 / 53,352 |
| busy, about 20% live rows, vacuumed | 336.3 | 290.5 | 72,685 / 104,265 |

Equality held on the same snapshot: 47/47 rows in the ops-qa-like state and
48/48 in the busy state.

Busy-shape observation (not a gate): the grouped query read more buffers than
the baseline (104,265 vs 72,685) but was still faster in the median. The
detail branch makes a second heap pass when a fifth of the rows are live. No
tuning was done without a theory. The shim's optional narrow rollup index
removed that pass on the fixture, but it is not part of this change.

## Observability

No-Observability-Change: `eshu_dp_status_snapshot_read_duration_seconds{read="active_work_summary",outcome}`
still times this read. `ReadStatusSnapshotFiltered` wraps `readActiveWorkSummary`
in the same `statusReadActiveWorkSummary` read label. The statement, decoder,
and section names are unchanged. An operator compares the same histogram
before and after deployment.

## Not proven

- The ops-qa plan shape and timing for the new statement, including ops-qa
  `work_mem`.
- Endpoint or bundle p95, and the API/MCP transport time.
- A built-binary or deployed run.
- Planner choice at real heap order and at 6.8 million intents.
- A busy-shape regression bound beyond the one 20%-live fixture.
