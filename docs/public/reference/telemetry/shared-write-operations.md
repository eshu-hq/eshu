# Shared-Write Operations

Shared-write telemetry exists because reducer follow-up work can look "stuck"
when it is actually waiting on readiness, partition conflict routing, stale
intent cleanup, or graph backend cost.

## Primary Signals

Start with:

- `eshu_dp_queue_depth`
- `eshu_dp_queue_oldest_age_seconds`
- `eshu_dp_shared_projection_cycles_total`
- `eshu_dp_shared_projection_intent_wait_seconds`
- `eshu_dp_shared_projection_processing_seconds`
- `eshu_dp_shared_projection_step_seconds`
- `eshu_dp_shared_projection_stale_intents_total`
- `eshu_dp_shared_edge_write_groups_total`
- `eshu_dp_shared_edge_write_group_duration_seconds`
- `eshu_dp_shared_edge_write_group_statement_count`
- `eshu_dp_shared_edge_target_miss_total`
- `eshu_dp_code_call_edge_batches_total`
- `eshu_dp_code_call_edge_batch_duration_seconds`

## Shared-Edge Target Misses

`eshu_dp_shared_edge_target_miss_total` counts shared-edge write batches
deferred because a runtime target was absent from the graph (#6184). It has
one bounded label:

- `domain` identifies the guarded shared projection domain (`handles_route`,
  `runs_in`, `deployable_unit_edges`, plus `workload_materialization`
  counted by the deployment-source target guard, whose deferral carries the
  non-counting failure class
  `workload_materialization_deployment_source_target_not_ready` so a slow
  target lane does not dead-letter it. The deferral is bounded by elapsed time
  since the repair cycle began, 30 minutes
  (`crossscope.ProducerReadinessMaxWait`); past it the pass fails with a
  counting error and dead-letters loudly, #6759).

Read it alongside the `shared edge batch target absent, deferring batch`
WARN, which carries the evidence source and one sample intent id —
deliberately kept out of the metric labels to bound cardinality. A miss is a
timing state, not a payload defect: the target commits later in the same
generation and the re-selected batch binds then, so a transient rise during a
rebuild drain is expected and a stuck non-zero rate across generations is
the signal worth alerting on. (This section lives here rather than in
`index.md` because that file is grandfather-capped and may not grow.)

Use traces and logs for repository, generation, source run, and lease-owner
detail. The shared-write metrics intentionally stay domain-scoped.

## Rollout Validation

When validating shared-write changes:

1. Confirm backlog trends are flat-to-down, not only that pods are up.
2. Compare selected-intent wait with processing duration. Wait points to
   readiness or partitioning. Processing duration points to graph writes or
   completion marking.
3. Confirm isolated `code_call` batches still flow and duration stays inside
   the expected environment envelope.
4. If backlog remains non-zero, inspect traces for the affected domain before
   assuming the fact queue is the bottleneck.
5. Use logs last to extract exact repository, generation, source run, or lease
   owner context.

## Tuning Order

The deterministic shared-write harness has shown this balanced dependency
scenario:

| Partition count | Batch limit | Drain rounds | Mean processed per round |
| --- | --- | --- | --- |
| 1 | 1 | 16 | 2.0 |
| 2 | 1 | 8 | 4.0 |
| 4 | 1 | 5 | 6.4 |
| 4 | 2 | 2 | 16.0 |

Interpretation:

- Increasing partition count gives the first major drain-round reduction by
  spreading stable lock domains across more workers.
- After partitioning is already helping, a modest batch-limit increase can
  remove tail rounds.
- Batch increases should come after partition increases, so larger per-round
  writes do not hide a partitioning bottleneck.

Recommended staging order:

1. Increase partition count and watch shared projection cycles plus stale-intent
   counts.
2. Confirm fact queue depth and oldest age stay flat-to-down.
3. Try a modest batch-limit increase only if backlog still drains in too many
   rounds after partitioning is healthy.

If partition count rises but oldest pending age still rises, open traces before
turning batch size again.

## Serialization Is Not A Fix

Do not ship worker-count reductions as the fix for a non-idempotent write path,
MERGE race, constraint conflict, or shared projection conflict. Lower worker
counts can be useful as a baseline or temporary safeguard, but the accepted fix
must preserve correctness and show measured performance evidence.
