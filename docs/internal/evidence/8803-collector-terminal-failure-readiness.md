# #8803 Collector Terminal Failure Readiness Evidence

Performance Evidence: the status snapshot gains one read of
`workflow_work_items`: the newest `failed_terminal` row per enabled collector
instance that no later `completed` row for the same scope has superseded.
Backend: PostgreSQL 16 (alpine container), schema taken from the repository
bootstrap SQL, no tuning. Input shape: three instances (git with 8,000 scopes,
security_alert with 40 scopes, oci_registry with 200 scopes), terminal rows at
about 5 percent of work items. The plan is indexed at both sizes, uses
`workflow_work_items_claimable_idx` for the per-instance candidate set and
`workflow_work_items_collector_scope_generation_updated_idx` for the
supersession probe, and never reads the failure message column.

| Table size (work items) | Variant | Execution time | Plan |
| --- | --- | --- | --- |
| 49,440 | unbounded `NOT EXISTS` over all terminal rows (rejected) | 9.3 ms | index scans |
| 494,400 | unbounded `NOT EXISTS` over all terminal rows (rejected) | 305 ms | planner flipped to a sequential scan and hash anti join |
| 494,400 | shipped: 20 newest terminal rows per instance, then `NOT EXISTS` | 18.5 ms | index scans, top-N sort |

The unbounded variant was rejected because its plan flipped to a sequential
scan at 10 times the size. The shipped variant caps the anti-join work at 20
probes per instance. The remaining cost follows the instance's own terminal-row
history (about 8,000 rows per instance here), not the table size. The existing
status snapshot already runs `COUNT(*) ... GROUP BY status` over the whole
table, so the added read is small next to it. The metrics scrape path reads the
same snapshot, so it issues one more statement per scrape; the pinned statement
counts in `status_test.go`, `status_query_shape_test.go`, and
`runtime/metrics_summary_test.go` moved by one.

Known bound: if the 20 newest terminal rows of an instance are all superseded
by later completed rows on their own scopes, an older unresolved terminal
failure on another scope stays hidden until it ages into the window. This needs
more than 20 consecutive recovered scopes for one instance and is accepted
for the status signal; the next cycle fails the old scope again and surfaces it.

Behavior proof on the same database (the shipped query text, extracted from
the Go constant):

```text
--- 1. newest unresolved terminal for sa-1 (scope 3, auth_denied)
 security_alert | sa-1        | auth_denied | list_alerts | t        | 2026-10-23 16:15:51.889555+00
--- 2. after a later completed item on the SAME scope: failure flips back (next older unresolved shown, if any)
 security_alert | sa-1        | auth_denied | list_alerts | t        | 2026-10-23 12:16:12.602599+00
--- 3. terminal on scope 4, success on a DIFFERENT scope 5: scope 4 stays reported
 security_alert | sa-1        | rate_limited | list_alerts | t        | 2026-10-23 16:24:21.280747+00
--- 4. scope 4 now a retryable pending row
 security_alert | sa-1        | auth_denied | list_alerts | t        | 2026-10-23 12:16:12.602599+00
```

Step 1 shows the newest unresolved failure. Step 2 inserts a later `completed`
work item for the same scope: that failure drops out (the row now shown is an
older failure on another scope of the seeded history, which never recovered).
Step 3 shows a success on a different scope does not clear a failure on this
scope. Step 4 shows a retryable row (`pending`, `last_failure_terminal=false`)
is never reported.

Plan for the shipped query at 494,400 work items:

```text
                                                                                                 QUERY PLAN
------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------
 Incremental Sort  (cost=8406.53..15142.73 rows=3 width=88) (actual time=18.386..18.387 rows=3 loops=1)
   Sort Key: i.collector_kind, i.instance_id
   Presorted Key: i.collector_kind
   Full-sort Groups: 1  Sort Method: quicksort  Average Memory: 25kB  Peak Memory: 25kB
   Buffers: shared hit=24499
   ->  Nested Loop  (cost=5038.48..15142.60 rows=3 width=88) (actual time=17.821..18.365 rows=3 loops=1)
         Buffers: shared hit=24496
         ->  Index Scan using collector_instances_kind_enabled_idx on collector_instances i  (cost=0.13..12.18 rows=3 width=15) (actual time=0.013..0.015 rows=3 loops=1)
               Index Cond: (enabled = true)
               Filter: (deactivated_at IS NULL)
               Buffers: shared hit=2
         ->  Limit  (cost=5038.35..5043.46 rows=1 width=33) (actual time=6.115..6.115 rows=1 loops=3)
               Buffers: shared hit=24494
               ->  Nested Loop Anti Join  (cost=5038.35..5104.78 rows=13 width=33) (actual time=6.114..6.114 rows=1 loops=3)
                     Buffers: shared hit=24494
                     ->  Limit  (cost=5037.93..5037.98 rows=20 width=48) (actual time=6.094..6.094 rows=1 loops=3)
                           Buffers: shared hit=24479
                           ->  Sort  (cost=5037.93..5044.85 rows=2769 width=48) (actual time=6.089..6.090 rows=1 loops=3)
                                 Sort Key: t.updated_at DESC
                                 Sort Method: top-N heapsort  Memory: 27kB
                                 Buffers: shared hit=24479
                                 ->  Index Scan using workflow_work_items_claimable_idx on workflow_work_items t  (cost=0.42..4964.25 rows=2769 width=48) (actual time=0.016..5.368 rows=8240 loops=3)
                                       Index Cond: ((collector_kind = i.collector_kind) AND (collector_instance_id = i.instance_id) AND (status = 'failed_terminal'::text))
                                       Buffers: shared hit=24476
                     ->  Index Scan using workflow_work_items_collector_scope_generation_updated_idx on workflow_work_items s  (cost=0.42..20.62 rows=6 width=23) (actual time=0.013..0.013 rows=0 loops=4)
                           Index Cond: ((collector_kind = i.collector_kind) AND (scope_id = t.scope_id) AND (updated_at > t.updated_at))
                           Filter: (status = 'completed'::text)
                           Buffers: shared hit=15
 Planning:
   Buffers: shared hit=426
 Planning Time: 1.575 ms
 Execution Time: 18.476 ms
(32 rows)

```

Observability Evidence: `eshu_runtime_coordinator_collector_terminal_failure_instances`
(labels `collector_kind`, `failure_class`) is a gauge on the existing status
metrics scrape. It is a gauge, not a counter, because a status read cannot count
failure events without double counting and a recovered instance must drop out.
The failure class and operation also appear in `/api/v0/status/collectors`,
`/api/v0/status/collector-readiness`, the status JSON, and the plain-text status
render. The failure message and provider status code are never read or
returned. The 3 AM runbook section "Symptom: collector failure" lists the
operator steps.
