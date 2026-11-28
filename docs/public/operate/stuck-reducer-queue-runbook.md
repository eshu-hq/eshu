# Stuck Reducer Queue Runbook

The reducer queue holds work items waiting to be projected into the graph.
When the depth stops falling, do not tune first. Read the pile: the rows
themselves say whether workers are slow, preconditions are dead, reads time
out, or a producer starves. Every query below is read-only; run them against
the read replica with a statement timeout.

```sql
SET statement_timeout = '30s';

-- 1. Depth by status: what shape is the pile?
SELECT status, count(*) FROM fact_work_items GROUP BY 1 ORDER BY 1;

-- 2. What is the retrying pile waiting on?
SELECT failure_class, count(*) FROM fact_work_items
WHERE status = 'retrying' GROUP BY 1 ORDER BY 2 DESC LIMIT 10;

-- 3. Poison or pacing? High attempts on the same error means poison;
-- low attempts on "not ready" means a dead precondition.
SELECT max(attempt_count),
       percentile_cont(0.5) WITHIN GROUP (ORDER BY attempt_count),
       count(*) FILTER (WHERE attempt_count > 50)
FROM fact_work_items WHERE status IN ('retrying', 'pending');

-- 4. Live churn or parked? Recent last_attempt_at with near-term
-- next_attempt_at means workers are trying and failing, not idle.
SELECT min(last_attempt_at), max(last_attempt_at),
       min(next_attempt_at), max(next_attempt_at), min(created_at)
FROM fact_work_items WHERE status = 'retrying';

-- 5. How old is the pending pile?
SELECT count(*) FILTER (WHERE now() - created_at > interval '1 hour'),
       min(created_at) FROM fact_work_items WHERE status = 'pending';

-- 6. Who keeps re-pending the rows? Per-reason breakdown (#11615); the
-- counter and gauges below stay consistent with this query.
SELECT COALESCE(last_requeue_reason, 'legacy_unclassified') AS reason,
       status, count(*)
FROM fact_work_items
WHERE status IN ('pending', 'claimed', 'running', 'retrying')
GROUP BY 1, 2 ORDER BY 1, 2;
```

## Reading the results

| Shape | Meaning | Next step |
|---|---|---|
| `claimed` + `running` near zero while `pending` is large | Workers are not picking up work — a starvation or claim problem, not slow work | Check worker pod restarts and the claim/lease path, not query speed |
| `retrying` large, attempts low, failures all `*_not_ready` | Preconditions never arrive — find the producer each class waits on | Failure-class table below |
| `retrying` large, attempts climbing on the same timeout error | A read path dies at scale — the data is usually fine, the query is too heavy | Timeout table below |
| Failures name a watermark or stamp that never advances | A maintainer runs but cannot keep pace — throughput starvation | Maintainer section below |
| One `failure_message` repeats verbatim across attempts | Deterministic failure on those rows, not flakes | Fix the path; do not wait for self-recovery |
| Query 6 shows one reason dominating the pile | That path re-pends faster than workers drain — the labels must first prove which class starves which; there are no separate queues to tune | `retry_backoff` with climbing attempts: poison or hot error, fix the handler. `readiness_wait`: dead precondition, find the producer. `waiting_wake` storm: a waiter flaps, read `eshu_dp_reducer_waiting_wake_total{site,outcome}` by site. `cross_scope_replay`/`fanout_holder` climbing: replay debt outruns the rerun, check the producer ACK path. `maintenance_reopen`/`admin_replay`/`admin_reopen` climbing without an operator action: an unexpected loop is reopening rows, find its caller. `legacy_unclassified` large on reducer rows: writers are missing their stamp, a rollout or a missed writer — plateau is the signal. |

## Rules before you touch anything

- Never delete stuck rows. Parking valid work to `dead_letter` strands
  downstream scopes with silently stale truth. Delete nothing; park only rows
  proven unsatisfiable, and only after a design review of the exact predicate.
- Never raise a statement timeout or retry budget as the fix. That trades a
  loud failure for longer resource pins under the same mass.
- Never cut workers or batch size to calm a queue. Serialization masks a cost
  defect the next backlog re-triggers.
- Throughput tuning (memory, CPU, workers) cannot fix a pile whose items fail
  on preconditions or timeouts. If queries 2–4 show waits and timeouts, stop
  tuning and diagnose the path.

## Known failure classes and their producers

| `failure_class` | Waited-on producer | How to confirm it is alive | Real case |
|---|---|---|---|
| `advisory_group_winners_watermark_not_ready` | Winners maintainer watermark (`supply_chain_impact_winners_materialization`, singleton row) | Read the watermark twice, minutes apart: `SELECT materialized_at FROM supply_chain_impact_winners_materialization WHERE singleton = 1` — advancing far below wall pace means starvation, a stale value means stall | 2026-11: maintainer ticked every ~30s but each round burned its scan budget on ~35 items while the stamp chased wall time; fixed by cutting per-item cost — narrow temp plus streamed body (#11564) |
| `workload_materialization_resolution_not_ready` | `deployment_mapping` cross-repo resolution activating the generation | Join stuck rows to `relationship_generations`: own generation active means waiting on foreign scopes; no generation row means the producer itself is stuck | 2026-11, operator-observed in live diagnosis (no code change: drains via the obligation fix #11570): 62 holder scopes blocked on a stalled evidence phase, not dead |
| `supply_chain_impact_fanout_failed` | Holder fan-out collect reads | `failure_details` shows statement timeout (SQLSTATE 57014), never decode or validation errors | 2026-11: 10s guard vs 155 GB table; fixed by retuning the routing gate (#11571) |
| `cross_scope_producer_not_ready`, `*_not_ready` siblings | Named producer per class | Same join pattern: check the waited-on row exists and advances | General rule, not one incident |

## Proving a fix before shipping it

A slow query needs a timing test before code: `EXPLAIN (ANALYZE, BUFFERS)`
of the exact shipped statement at representative mass, then the same timing
after the fix, against the same ids. A maintainer needs paired stamp reads
showing the stamp gaining on wall time, plus the stuck count falling with
sampled rows reaching `succeeded`. A retry loop needs the failing class
trending to zero with no new dead letters. Ship the numbers, not the theory.

## After the drain

Watch the rollout, not the deploy: the failing class count, the dead-letter
count (replay or explicitly disposition what the bug already stranded), and
the producer-side signal (stamp pace, per-statement timings, obligation
finishes). The queue drains itself once the path is repaired; no manual
replay of stuck rows should be needed, and if it is, that is a new finding.

## Safe remediation limits

- Park to `dead_letter` only rows whose precondition is proven permanently
  unsatisfiable (producer dead, key superseded, generation retired) — never
  rows that are merely slow or retrying.
- Replaying dead letters is the operator's call per row group after the path
  fix lands; replay before the fix just re-queues the same failure.
- A full resweep or rebuild resets lag once without changing steady-state
  pace. Use it as a stopgap only beside the pace fix, never instead of it.
