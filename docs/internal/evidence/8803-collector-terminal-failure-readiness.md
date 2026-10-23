# #8803 Collector Terminal Failure Readiness Evidence

Performance Evidence: the status snapshot reads, per enabled and
non-deactivated collector instance, the newest `failed_terminal` work item
inside that instance's window that no later `completed` item of the same
instance and scope superseded. The window is `3 x scan_interval` with a 30
minute floor (`terminalFailureWindowCycles`, `defaultCoordinatorRecentFailureWindow`
in `go/internal/storage/postgres/workflow_status.go`). Migration 210 adds the
partial index the read seeks:
`workflow_work_items_failed_terminal_instance_updated_idx ON workflow_work_items
(collector_kind, collector_instance_id, updated_at DESC, work_item_id ASC)
WHERE status = 'failed_terminal'`, built `CONCURRENTLY` and migration-only.

## Setup

- Backend: PostgreSQL 18.6 (`postgres:18-alpine`, aarch64), all repository
  migrations applied in order, `shared_buffers=256MB`, data on the container's
  disk volume.
- Shape: 542,880 work items. One `security_alert` instance (`sa-hot`,
  `scan_interval` 15m) fails all 40 targets every 15 minute cycle for 72 days:
  276,480 `failed_terminal` rows. One `security_alert` instance (`sa-recovered`,
  15m) fails every target each cycle and completes it 5 minutes later for 30
  days: 115,200 `failed_terminal` and 115,200 `completed` rows. Eighteen `git`
  instances (no `scan_interval`, 30 minute floor) with 2,000 `completed` rows
  each. 391,680 `failed_terminal` rows in total. `VACUUM ANALYZE` after
  seeding.
- Statement: the shipped `workflowCollectorTerminalFailuresQuery` text, as a
  prepared statement bound with the 20 instances (two 45 minute cutoffs and
  eighteen 30 minute cutoffs), the way Go binds it.
- Cold: the first execution after `docker restart` (empty `shared_buffers`; the
  host page cache was not dropped). Warm: the third consecutive execution.

## Results

| Variant | Run | Execution time | Buffers | Temp blocks |
| --- | --- | --- | --- | --- |
| Before migration 210 | cold | 5,702.0 ms | 51 hit, 2,708 read | 623 read, 623 written |
| Before migration 210 | warm | 83.6 ms | 2,759 hit | 623 read, 623 written |
| After migration 210 | cold | 3.0 ms | 498 hit, 296 read | none |
| After migration 210 | warm | 1.9 ms | 794 hit | none |

`CREATE INDEX CONCURRENTLY` on the populated table took 1,513.7 ms (1.5 s). The
new index is 26 MB for 391,680 entries. `workflow_work_items_claimable_idx` is
9,920 kB and the heap is 106 MB; the new index does not deduplicate because
every entry carries a distinct `work_item_id`.

Without the index the planner reads both instances' terminal rows through
`workflow_work_items_claimable_idx` and builds a hash anti join over all
115,200 `completed` rows of `sa-recovered` (the plan prints 57,600 rows as the
average of its two loops), which spills to disk. With the
index each instance's lateral read seeks the index newest first, stops at the
first unresolved row (one row for `sa-hot`), and probes the
`(collector_kind, scope_id, ...)` index once per candidate (121 probes across
the 20 instances). The plan has no per-instance Sort node: `work_item_id` is the
index's last column, so the `ORDER BY t.updated_at DESC, t.work_item_id ASC`
tiebreak is read in index order.

Supersession probe ordering: the `(collector_kind, scope_id, generation_id,
updated_at DESC, work_item_id)` index orders a scope's rows by `generation_id`,
so how soon a probe finds a later `completed` row depends on generation ids. A
rerun with every `sa-*` `generation_id` rewritten to `md5(work_item_id)`
(`UPDATE` then `VACUUM ANALYZE`) picked a different probe plan (materialized
`completed` rows of the instance from `workflow_work_items_claimable_idx`) and
ran in 0.96 ms warm with 232 buffers. Before the `VACUUM`, with dead tuples
from a rolled-back version of that update, the planner chose
`workflow_work_items_phase_tuple_idx` for the probe: 15.4 ms and 12,336
buffers. That is the worst plan observed with the index in place.

Write tax: the index holds only `failed_terminal` rows. Every work-item UPDATE
is already non-HOT because `workflow_work_items_claimable_idx` carries
`status`, `visible_at`, and `updated_at`, so the index adds one entry write on
the claimed to `failed_terminal` transition and nothing on claims, heartbeats,
or completions.

Statement count: the read runs only when at least one enabled, non-deactivated
instance exists. The status snapshot pins in `status_test.go` and
`status_query_shape_test.go` (27) and the scrape inventory in
`runtime/metrics_summary_test.go` (25, 21, 22) use stores with no instance, so
they are unchanged from `main`; `TestReadCoordinatorSnapshotIncludesTerminalFailures`
pins the read when an instance exists.

## Window semantics

A `failed_terminal` row counts while `updated_at >= asOf - GREATEST(3 x
scan_interval, 30 min)` and no `completed` row of the same `collector_kind`,
`collector_instance_id`, and `scope_id` has a later `updated_at`. Pending,
claimed, or retrying rows do not clear it. `scan_interval` comes from
`configuration->'scan_interval'`, parsed in Go by
`schedule.ParseScanIntervalValue` (the coordinator's rules); unset uses the
coordinator default, which the floor covers; a malformed value uses the floor
and logs once per read at debug. The instance list and the failure read use
one `asOf`.

The live proofs in
`go/internal/storage/postgres/workflow_terminal_failure_status_live_test.go`
and `workflow_terminal_failure_index_live_test.go` (enrolled in the
`live-postgres-readiness` runner) cover: clearing by a later completion on the
same instance and scope; no clearing by a sibling scope, by another instance of
the same kind on a shared `scope_id`, or by a later pending or retrying row;
non-terminal rows, disabled, and deactivated instances never reported; an
orphan older than the window not reported while a row inside it is; 12h
interval keeps 20 h and drops 40 h; no `scan_interval` keeps 25 min and drops
35 min; a malformed `scan_interval` uses the floor; equal `updated_at` picks the
lowest `work_item_id`; a completion at the failure's exact `updated_at` does
not clear it (a `>` to `>=` mutation fails this proof); a `scanner_worker`
`failed_terminal` row whose `last_failure_terminal` flag is false still degrades
its instance through the status builder, including when an older row of the
same instance has the flag true (the reader never reads the flag); `EXPLAIN`
seeks the migration 210 index with no per-instance sort; re-applying migration 210 twice on a populated table leaves
one valid index. Five SQL mutations (drop the same-instance probe, drop the
window, flip the tiebreak, let pending or retrying rows clear, drop the floor)
each fail at least one of these proofs.

Known limits:

- A coordinator that stops planning lets a real failure age out after the
  window. Coordinator liveness and freshness signals own that case.
- The window uses the instance's `scan_interval`, not the coordinator's global
  `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL`, which the status reader does
  not know. If an instance's items wait in the claim backlog for more than about
  two scan intervals, or the global reconcile interval is longer than 10
  minutes with no `scan_interval` set, a failing instance can age out between
  re-failures and the signal can flap.
- A bootstrap instance is always planned on the global reconcile interval;
  the coordinator ignores its `scan_interval`, but the status window still uses
  it, so an orphaned bootstrap failure stays reported for 3 x that
  `scan_interval`, or the 30 minute floor when that is longer.
- To clear a failure in this version, fix the cause and let a completed item
  land, or remove the target and wait one window. There is no explicit
  acknowledge.

## Plans

Before migration 210, cold:

```text
 Sort (actual time=1441.643..1441.644 rows=1.00 loops=1)
   Sort Key: i.collector_kind, i.instance_id
   Sort Method: quicksort  Memory: 25kB
   Buffers: shared hit=51 read=2708, temp read=623 written=623
   ->  Nested Loop (actual time=1384.938..1441.620 rows=1.00 loops=1)
         Buffers: shared hit=48 read=2708, temp read=623 written=623
         ->  Function Scan on i (actual time=1384.319..1384.324 rows=20.00 loops=1)
         ->  Limit (actual time=2.863..2.863 rows=0.05 loops=20)
               Buffers: shared hit=48 read=2708, temp read=623 written=623
               ->  Sort (actual time=2.860..2.860 rows=0.05 loops=20)
                     Sort Key: t.updated_at DESC, t.work_item_id
                     Sort Method: quicksort  Memory: 25kB
                     Buffers: shared hit=48 read=2708, temp read=623 written=623
                     ->  Hash Anti Join (actual time=2.803..2.819 rows=6.00 loops=20)
                           Hash Cond: (t.scope_id = s.scope_id)
                           Join Filter: (s.updated_at > t.updated_at)
                           Rows Removed by Join Filter: 5459
                           Buffers: shared hit=45 read=2708, temp read=623 written=623
                           ->  Bitmap Heap Scan on workflow_work_items t (actual time=0.020..0.043 rows=12.00 loops=20)
                                 Recheck Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'failed_terminal'::text) AND (updated_at >= i.cutoff))
                                 Heap Blocks: exact=86
                                 Buffers: shared hit=40 read=112
                                 ->  Bitmap Index Scan on workflow_work_items_claimable_idx (actual time=0.013..0.013 rows=12.00 loops=20)
                                       Index Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'failed_terminal'::text) AND (updated_at >= i.cutoff))
                                       Index Searches: 20
                                       Buffers: shared hit=40 read=26
                           ->  Hash (actual time=21.949..21.949 rows=57600.00 loops=2)
                                 Buckets: 131072 (originally 4096)  Batches: 2 (originally 1)  Memory Usage: 7169kB
                                 Buffers: shared hit=5 read=2596, temp written=621
                                 ->  Bitmap Heap Scan on workflow_work_items s (actual time=1.350..14.468 rows=57600.00 loops=2)
                                       Recheck Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'completed'::text))
                                       Heap Blocks: exact=2402
                                       Buffers: shared hit=5 read=2596
                                       ->  Bitmap Index Scan on workflow_work_items_claimable_idx (actual time=1.234..1.234 rows=57600.00 loops=2)
                                             Index Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'completed'::text))
                                             Index Searches: 2
                                             Buffers: shared hit=5 read=194
 Planning:
   Buffers: shared hit=388 read=60 dirtied=3
 Planning Time: 2.035 ms
 Execution Time: 5702.041 ms
```

Before migration 210, warm:

```text
 Sort (actual time=71.332..71.334 rows=1.00 loops=1)
   Sort Key: i.collector_kind, i.instance_id
   Sort Method: quicksort  Memory: 25kB
   Buffers: shared hit=2759, temp read=623 written=623
   ->  Nested Loop (actual time=16.469..71.304 rows=1.00 loops=1)
         Buffers: shared hit=2756, temp read=623 written=623
         ->  Function Scan on i (actual time=16.236..16.241 rows=20.00 loops=1)
         ->  Limit (actual time=2.752..2.752 rows=0.05 loops=20)
               Buffers: shared hit=2756, temp read=623 written=623
               ->  Sort (actual time=2.749..2.749 rows=0.05 loops=20)
                     Sort Key: t.updated_at DESC, t.work_item_id
                     Sort Method: quicksort  Memory: 25kB
                     Buffers: shared hit=2756, temp read=623 written=623
                     ->  Hash Anti Join (actual time=2.704..2.709 rows=6.00 loops=20)
                           Hash Cond: (t.scope_id = s.scope_id)
                           Join Filter: (s.updated_at > t.updated_at)
                           Rows Removed by Join Filter: 5459
                           Buffers: shared hit=2753, temp read=623 written=623
                           ->  Bitmap Heap Scan on workflow_work_items t (actual time=0.009..0.016 rows=12.00 loops=20)
                                 Recheck Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'failed_terminal'::text) AND (updated_at >= i.cutoff))
                                 Heap Blocks: exact=86
                                 Buffers: shared hit=152
                                 ->  Bitmap Index Scan on workflow_work_items_claimable_idx (actual time=0.007..0.007 rows=12.00 loops=20)
                                       Index Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'failed_terminal'::text) AND (updated_at >= i.cutoff))
                                       Index Searches: 20
                                       Buffers: shared hit=66
                           ->  Hash (actual time=21.189..21.190 rows=57600.00 loops=2)
                                 Buckets: 131072 (originally 4096)  Batches: 2 (originally 1)  Memory Usage: 7169kB
                                 Buffers: shared hit=2601, temp written=621
                                 ->  Bitmap Heap Scan on workflow_work_items s (actual time=0.969..13.714 rows=57600.00 loops=2)
                                       Recheck Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'completed'::text))
                                       Heap Blocks: exact=2402
                                       Buffers: shared hit=2601
                                       ->  Bitmap Index Scan on workflow_work_items_claimable_idx (actual time=0.879..0.879 rows=57600.00 loops=2)
                                             Index Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'completed'::text))
                                             Index Searches: 2
                                             Buffers: shared hit=199
 Planning:
   Buffers: shared hit=458
 Planning Time: 1.588 ms
 Execution Time: 83.591 ms
```

After migration 210, cold:

```text
 Sort (actual time=2.912..2.913 rows=1.00 loops=1)
   Sort Key: i.collector_kind, i.instance_id
   Sort Method: quicksort  Memory: 25kB
   Buffers: shared hit=498 read=296
   ->  Nested Loop (actual time=0.842..2.892 rows=1.00 loops=1)
         Buffers: shared hit=495 read=296
         ->  Function Scan on i (actual time=0.016..0.020 rows=20.00 loops=1)
         ->  Limit (actual time=0.143..0.143 rows=0.05 loops=20)
               Buffers: shared hit=495 read=296
               ->  Nested Loop Anti Join (actual time=0.143..0.143 rows=0.05 loops=20)
                     Buffers: shared hit=495 read=296
                     ->  Index Scan using workflow_work_items_failed_terminal_instance_updated_idx on workflow_work_items t (actual time=0.005..0.016 rows=6.05 loops=20)
                           Index Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (updated_at >= i.cutoff))
                           Index Searches: 20
                           Buffers: shared hit=133 read=49
                     ->  Index Scan using workflow_work_items_collector_scope_generation_updated_idx on workflow_work_items s (actual time=0.021..0.021 rows=0.99 loops=121)
                           Index Cond: ((collector_kind = i.collector_kind) AND (scope_id = t.scope_id) AND (updated_at > t.updated_at))
                           Filter: ((collector_instance_id = i.instance_id) AND (status = 'completed'::text))
                           Index Searches: 121
                           Buffers: shared hit=362 read=247
 Planning:
   Buffers: shared hit=425 read=65 dirtied=5
 Planning Time: 2.173 ms
 Execution Time: 2.992 ms
```

After migration 210, warm:

```text
 Sort (actual time=1.813..1.813 rows=1.00 loops=1)
   Sort Key: i.collector_kind, i.instance_id
   Sort Method: quicksort  Memory: 25kB
   Buffers: shared hit=794
   ->  Nested Loop (actual time=0.407..1.792 rows=1.00 loops=1)
         Buffers: shared hit=791
         ->  Function Scan on i (actual time=0.016..0.019 rows=20.00 loops=1)
         ->  Limit (actual time=0.088..0.088 rows=0.05 loops=20)
               Buffers: shared hit=791
               ->  Nested Loop Anti Join (actual time=0.088..0.088 rows=0.05 loops=20)
                     Buffers: shared hit=791
                     ->  Index Scan using workflow_work_items_failed_terminal_instance_updated_idx on workflow_work_items t (actual time=0.002..0.005 rows=6.05 loops=20)
                           Index Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (updated_at >= i.cutoff))
                           Index Searches: 20
                           Buffers: shared hit=182
                     ->  Index Scan using workflow_work_items_collector_scope_generation_updated_idx on workflow_work_items s (actual time=0.014..0.014 rows=0.99 loops=121)
                           Index Cond: ((collector_kind = i.collector_kind) AND (scope_id = t.scope_id) AND (updated_at > t.updated_at))
                           Filter: ((collector_instance_id = i.instance_id) AND (status = 'completed'::text))
                           Index Searches: 121
                           Buffers: shared hit=609
 Planning:
   Buffers: shared hit=484
 Planning Time: 1.759 ms
 Execution Time: 1.884 ms
```

Observability Evidence: `eshu_runtime_coordinator_collector_terminal_failure_instances`
(labels `collector_kind`, `failure_class`) is a gauge on the existing status
metrics scrape. It is a gauge, not a counter, because a status read cannot count
failure events without double counting and a recovered instance must drop out.
Every runtime that scrapes status exports the same value, so read it with
`max by (collector_kind, failure_class)`, never summed. The failure class and
operation also appear in `/api/v0/status/collectors`,
`/api/v0/status/collector-readiness`, the status JSON, and the plain-text status
render. The failure message and provider status code are never read or
returned. A malformed `scan_interval` logs `collector scan_interval unreadable;
terminal failure window uses the floor` at debug with
`malformed_scan_interval_instances` and `collector_instance_ids`. The 3 AM
runbook section "Symptom: collector failure" lists the operator steps.
