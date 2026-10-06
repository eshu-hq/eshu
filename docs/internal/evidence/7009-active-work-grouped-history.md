# Status active work: grouped history before the generation join (#7009)

## Scope and cause

`StatusStore.ReadStatusSnapshotFiltered` reads stage counts, domain backlog,
queue state, conflict blockage, and the latest failure from one statement,
`activeWorkSummaryQuery` (`read="active_work_summary"`). Before this change, the
statement joined every `fact_work_items` row (about 254,000 on the ops-qa
capture of 2026-10-05) to its scope state and generation. It then materialized
the joined set and counted `fact_work_items` a second time for `total_count`.
Most rows are succeeded history, and the summary reads them only as
`(stage, status)` counts and `succeeded_count`. The C3 shim attributed most of
the cost to per-row CPU: two wide hash joins, a 254,000-row CTE, rescans of
that CTE, and the second count. I/O was not the main cost. The shim report is
an out-of-tree harness artifact, not a file in this repository. It lives in the
latency kit at
eshu-latency-kit/latency-reconcile-20261002/control-diagnostic/harness-worktree/7009-c3-shim-report-20261005.md.

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
`TestActiveWorkSummaryQueryIsThePinnedRender` pins that SHA-256, so any edit to
the statement must re-pin it and rerun the oracle differential.

## Proof

- Oracle: `activeWorkSummaryPreHistoryGroupsOracle` is the old query, rendered
  from origin/main `9bcca588f` by a temporary package test. It is pinned to the
  shim baseline SHA-256 `bef1078e2f5b971f6f35f576403b5c7197b59b6e37ccf13974d3240e4d432e2e`.
  `TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups` undoes exactly
  the grouped-history pieces and gets the oracle back, byte for byte.
- Oracle lifecycle: the oracle is bound to `9bcca588f` and to this
  transformation only. A later change to a shared fragment (`stageCountsSelect`,
  `domainBacklogCTEs`, `reducerConflictBlockageCTEs`, the scope join, or
  `latestQueueFailureSelect`) fails the reverse-substitution test. The change
  then either re-derives the oracle from the new query by those reverse
  substitutions and re-pins its SHA-256 with review sign-off, or retires the
  oracle tests. `TestActiveWorkSummaryMatchesStandaloneReads` stays as the
  surviving equivalence oracle.
- Section statuses: `TestActiveWorkSummarySectionStatusesStayWithinDetailStatuses`
  requires every section status predicate outside grouped history to use `IN`
  or `=` with only `activeWorkDetailStatuses`. Seeded widenings (a
  `quarantined` backlog, a `succeeded` failure read, a `NOT IN` blockage) must
  be rejected.
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
any `Seq Scan on scope_generations` in the summary plan. That literal rule
bound the fixture, not the query. The C3 shim's baseline arm already took one
hashed full pass over `scope_generations` at ops-qa row counts, and the grouped
query takes a second hashed pass in its detail join at the busy shape. The
hazard #4446 and #6794 guard against is a per-row or per-group rescan of
`scope_generations` (O(rows x generations)), not a single pass (O(rows +
generations), about 4 ms on the 100,000-generation fixture).

The arbiter ruling for #7009 (D1) binds the guard to that cost class.
`checkSummaryGenerationScans` checks every `Seq Scan on scope_generations` line,
`Parallel Seq Scan` included:

- R1, single-pass shape: the nearest shallower plan line must be `Hash`,
  `Parallel Hash`, a hash join on either side (build or outer/probe), `Sort`,
  `Gather`, or `Gather Merge`. `Nested Loop`, `Materialize`, `Memoize`, and any
  other parent are rejected, and the error names that parent.
- R2, single-pass count: when EXPLAIN ANALYZE actuals are present, `loops` on
  the scan and on its parent must be at most `Workers Launched` of the nearest
  enclosing `Gather` or `Gather Merge` plus one, or 1 with no Gather. The live
  guard asserts the plan has `actual time`, so this rule is never vacuous there.
- R3, placement: at most one such scan inside `active_fact_work_items`, at most
  one inside `fact_work_history_counts`, and none anywhere else. That keeps the
  #6794 `LATERAL ... LIMIT 1` probe in `active_fact_work_items_scope_state`. A
  plan without the `active_fact_work_items` CTE fails closed.

The checker reports every violation, not only the first.
`TestCheckSummaryGenerationScansRejectsDetailFullScan` pins the rules on fixed
plan text. It accepts five shapes: probes everywhere, a hash build side, the
hash-join outer side that PostgreSQL 18.3 picks for the history join on the
live fixture, a parallel hash with `loops=3` under two launched workers, and a
sort under a merge join. It rejects a nested-loop rescan (`loops=2000`), a
nested loop over `Materialize` or `Memoize`, a hash rebuilt per outer row, a
rescanned outer side, a parallel hash above workers plus one, a scan outside
both CTEs, two scans in one CTE, a CTE whose root is the scan, and a plan with
no detail CTE. Removing each rule in turn fails at least one case.

The live guard also re-plans with index, bitmap, hash join, merge join,
materialize, and memoize paths disabled. On PostgreSQL 18.3 that seed plans
nested loops in both CTEs, and the scope-state `LIMIT 1` probe becomes a full
scan under `Limit`. The guard must reject both CTEs with a `Nested Loop`
parent, and it does. The seed is planned with plain EXPLAIN, because R1 needs
no actuals. One run with EXPLAIN ANALYZE showed the cost it guards against:
the detail scan ran with `loops=16000`, the scope-state scan with
`loops=2000`, and the seeded statement took about 3 minutes. The whole guard
takes 2.97 s locally under `-race`. It runs in the reducer-contention gate.

Known limitation: the standalone `stageCountsQuery` check in the same test
still rejects any `Seq Scan on scope_generations`, the old fixture-bound
literal. This change does not touch that statement or its check.

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
| ops-qa-like, stale VM, `work_mem=4MB` (temp blocks 14,812 / 5,349) | 550.1 | 171.3 | 191,276 / 141,573 |
| vacuumed, no index | 409.0 | 126.7 | 59,728 / 53,352 |
| busy, about 20% live rows, vacuumed | 336.3 | 290.5 | 72,685 / 104,265 |

At `work_mem=4MB` the grouped history HashAggregate spills (5,349 temp blocks).
The baseline spills more (14,812), and the grouped query still wins.

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
in the same `statusReadActiveWorkSummary` read label. The read label, decoder,
and section names are unchanged; only the statement text changed. An operator compares the same histogram
before and after deployment.

## Not proven

- The ops-qa plan shape and timing for the new statement, including ops-qa
  `work_mem`.
- Endpoint or bundle p95, and the API/MCP transport time.
- A built-binary or deployed run.
- Planner choice at real heap order and at 6.8 million intents.
- A busy-shape regression bound beyond the one 20%-live fixture.
