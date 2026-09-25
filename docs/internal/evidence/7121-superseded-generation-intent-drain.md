# Superseded-generation shared-projection intent drain (#7121)

## Problem

Shared-projection intents (`shared_projection_intents`) for `runs_in` and
`handles_route` whose scope generation was superseded stayed `pending` forever.
`FilterRowsByReadiness` blocks them on a `service_uid` /
`workload_materialization` phase row that is published only by workload
materialization, which never runs for a superseded generation. The acceptance
filter (`FilterAuthoritativeIntents`) only stales a row when the acceptance row
for the SAME `(scope, acceptance_unit, source_run_id)` names a different
generation; a new source run has its own acceptance row, so the orphan is never
stale. Read-only ops-qa observation (orchestrator, 2026-09-25): every pending
`runs_in` (128) and `handles_route` (128) row had
`scope_generations.status = 'superseded'`, and none was on an active
generation.

## Change

`SelectPartitionBatch` runs the acceptance filter, dedupe, and readiness gate as
before. It then makes ONE bounded lookup over the distinct generation ids of the
readiness-BLOCKED rows only (skipped when nothing is blocked) through the
optional `worker.SupersededGenerationReader` port, and moves the blocked rows on
a superseded generation into `StaleIDs`, out of `BlockedRows`. The Postgres
implementation is
`SharedIntentStore.SupersededGenerationIDs`:

```sql
SELECT g.generation_id
FROM scope_generations AS g
WHERE g.generation_id = ANY($1::text[]) AND g.status = 'superseded'
  AND NOT EXISTS (
    SELECT 1 FROM fact_work_items AS w
    WHERE w.scope_id = g.scope_id AND w.generation_id = g.generation_id
      AND ((w.stage = 'reducer' AND w.status IN ('claimed', 'running'))
        OR (w.stage = 'projector' AND w.status IN ('pending', 'retrying', 'claimed', 'running'))))
```

### Why only blocked rows

A generation superseded before its prerequisite-phase producer ran never
publishes that phase row, so its blocked intents wait forever; that is what the
drain removes. A generation that projected normally and was later
superseded keeps its phase rows (the readiness key includes the generation id),
so its still-pending intents are READY, and they must keep projecting. A delta
successor (`scope_generations.is_delta`, `sharedintent` refresh
`ApplyRepoRefreshDeltaScope`) carries only changed-file facts and its retract is
file-scoped, so it never re-emits the edge of a file it did not touch; draining
a ready row of the superseded full generation would lose that edge permanently.
A full successor would re-emit it, but the reader cannot tell which kind of
successor exists, so ready and terminal rows are never drained. The first
version of this change drained every row on a superseded generation; review
finding F1 caught the ready-row case and it was narrowed to blocked rows.

### Why only generations with no in-flight producer

Blocked-on-a-superseded-generation does not by itself mean "will never
publish": a producer already running when the successor activated can still
publish the phase row afterwards, after which the row is ready and must project
(review N1). The lookup therefore returns a superseded generation only when no
`fact_work_items` row of that `(scope_id, generation_id)` can still publish.
"Superseded AND no in-flight producer" means the phase never publishes, from
these claim rules:

- Reducer stage: both reducer claim statements (`claimReducerWorkQuery`,
  `reducer_queue_claim_query.go`, and `claimReducerWorkBatchQuery`,
  `reducer_queue_batch_query.go`) embed `supersedeInactiveReducerGenerationsCTE`
  (`reducer_generation_filter_sql.go`) and exclude the rows it supersedes from
  their candidate set in the same statement. An unleased
  (`pending`/`retrying`) reducer row of an older generation is therefore
  superseded, never claimed. Only `claimed`/`running` rows survive that sweep,
  and the candidate set re-claims them once their lease expires, so
  `claimed`/`running` counts as in flight whatever `claim_until` says.
  Readiness-gated reducer domains (`reducerClaimReadinessGateSQL`) are held out
  of the sweep only while the same gate also refuses to claim them, so the
  invariant holds for them too. `workload_materialization` and
  `semantic_entity_materialization` have no such requirement row.
- Projector stage (`canonical_nodes`): the projector claim supersedes only
  `pending`/`failed` generations that have a newer projector sibling
  (`projector_queue_claim_sql.go`), so a `pending` or `retrying` projector row
  of an already-superseded generation is still claimable and counts as in flight
  together with `claimed`/`running`.

A deferred generation is re-checked on every selection pass: when the producer
publishes, the row is ready and projects; when its lease expires and the sweep
supersedes it (or it finishes without publishing), the generation is reported and
the blocked row drains. Deferral costs one more pass, never an edge.

The guard applies to every gated domain `SelectPartitionBatch` serves
(`worker.ReadinessPhase`), not only `runs_in`/`handles_route`:

| Readiness phase | Domains | Producer that can still publish |
| --- | --- | --- |
| `workload_materialization` | `runs_in`, `handles_route` | reducer `workload_materialization` |
| `canonical_nodes_committed` | `invokes_cloud_action`, `inheritance_edges`, `sql_relationships`, `shell_exec`, `rationale_edges` | projector (source-local) |
| `semantic_nodes_committed` | `documentation_edges` | reducer `semantic_entity_materialization` |

`code_calls` maps to `canonical_nodes_committed` too but selects through its own
runner (`code/call/projection/selection.go`), not `SelectPartitionBatch`, so it
is not drained.

Residual, not closed by this change (#7130): the SQL does not enforce that
`superseded` is terminal on every writer. If Ack re-activated a superseded
generation between the lookup and the drain, or if the scope's
`active_generation_id` does not point at a newer generation (so the reducer
sweep does not apply), the invariant above does not hold and a drained blocked
intent could be lost. The in-flight guard closes the producer-in-flight window;
#7130 owns the terminality gap.

Drained rows count as progress in the scan-widening loop, so a window made only
of superseded blocked rows returns a batch instead of widening the scan toward
the cap. `blocked_count` and `blocked_intent_wait_seconds` exclude drained rows.

The predicate is the terminal `superseded` status (`go/internal/scope/scope.go`
`allowedGenerationTransitions`), not "not the active generation", which would
race with activation and drop a pending generation's live intents. Ids that are
not scope generations (repo_dependency relationship-generation ids) match
nothing and keep today's behavior. A reader without the port keeps the old
behavior byte for byte; a lookup error fails the selection. The code_calls and
repo_dependency runners do not go through `SelectPartitionBatch` and are not
touched. The SQL does not enforce that `superseded` is terminal on every writer;
that gap is tracked in #7130: if Ack re-activated a superseded generation, its
drained blocked intents would be lost. No occurrence has been observed; this
drain does not widen that gap because ready rows are untouched.

Conflict domain: read-only lookup plus the existing `MarkIntentsCompleted`
drain of the same `intent_id` rows; no new lock, lease, or worker. Partition
leases, worker counts, and batch limits are unchanged. Draining is idempotent:
completing an already-completed intent is a no-op update, and two workers only
overlap on a partition through the existing lease.

Performance Evidence: local `postgres:16`, `scope_generations` with 200,000 rows
(100,000 superseded). `EXPLAIN (ANALYZE, BUFFERS)` of the lookup over 6 ids (5
scope generations and one non-scope id): `Index Scan using
scope_generations_pkey`, `Index Cond: generation_id = ANY(...)`, `Filter:
status = 'superseded'`, `Buffers: shared hit=25`, `Execution Time: 0.166 ms`.
`generation_id` is the table's primary key (`schema/data-plane/postgres/002_scope_generations.sql`),
so the probe is bounded by the window's distinct generation count, one round trip
per selection pass, and only when at least one row is blocked.
With the in-flight producer guard: local `postgres:16`, 30,000 scopes, 90,000
generations, 360,000 `fact_work_items` (1% of superseded generations carry a
`running` reducer row), fresh `ANALYZE`. `EXPLAIN (ANALYZE, BUFFERS)` of the
lookup over 64 superseded generation ids: `Nested Loop Anti Join` over
`Bitmap Index Scan on scope_generations_pkey` (64 rows) and `Index Scan using
fact_work_items_scope_generation_idx` (`Index Cond: scope_id = g.scope_id AND
generation_id = g.generation_id`, 64 loops, 4 rows removed by filter per probe),
custom plan `Buffers: shared hit=422 read=32`, `Execution Time: 1.104 ms`;
`force_generic_plan` `Buffers: shared hit=454`, `Execution Time: 0.350 ms`. The
NOT EXISTS is an index probe on the existing `(scope_id, generation_id, status,
updated_at)` index (migration 005); no new index. Expected backlog effect (not yet measured on ops-qa): the drain stops the
permanent rescan of the 128 + 128 orphaned pending rows on every partition cycle.

Observability Evidence: `eshu_dp_shared_projection_stale_intents_total` gains a
closed `reason` attribute (`acceptance_mismatch`, `generation_superseded`); a
separate log line `shared projection drained intents of superseded generations`
carries `stale_reason`; `PartitionProcessResult.SupersededGenerationIntents`
carries the count. The blocked log line is renamed to `shared projection skipped
intents until their prerequisite graph phase is committed` (the gate is the
`workload_materialization` / `canonical_nodes` phase, not semantic readiness) and
gains `readiness_phase`. Blocked rows on a superseded generation drain right
after the gate, so `blocked_count` and `blocked_intent_wait_seconds` cover only
non-superseded generations.

## Proof

- `go test ./internal/reducer/intents/shared/worker -count=1`:
  `TestSelectPartitionBatchKeepsReadyRowOnSupersededGeneration` (F1: a ready row
  on a superseded generation stays in `LatestRows`, no lookup),
  `TestSelectPartitionBatchDrainsOnlyBlockedRowsOfSupersededGeneration` (only the
  blocked row drains; the lookup covers only its generation),
  `TestSelectPartitionBatchDrainsSupersededGenerationIntent` (blocked orphan
  drains), `TestSelectPartitionBatchReturnsWithoutWideningWhenSupersededRowsDrain`
  (F4: one window read, no widening), and
  `TestSelectPartitionBatchPropagatesSupersededLookupError` cover the change;
  `TestSelectPartitionBatchKeepsActiveGenerationBlockedOnMissingPhase`,
  `TestSelectPartitionBatchKeepsPendingGenerationBlocked`,
  `TestSelectPartitionBatchSupersededLookupSkippedWhenNothingBlocked`, and
  `TestSelectPartitionBatchWithoutSupersededReaderIsUnchanged` pin the
  boundaries; `TestProcessPartitionDrainsSupersededGenerationWithReasonTelemetry`
  proves the full cycle, counter reason, and log.
- `go test ./internal/reducer/intents/shared/worker -run
  TestSelectPartitionBatchKeepsBlockedRowWhileProducerIsInFlight`: the blocked
  row stays blocked while the store omits the generation, projects once the
  phase row publishes, and drains once the store reports the generation.
- `go test ./internal/storage/postgres -run SupersededGenerationIDs` (plus the
  env-gated `TestSupersededGenerationIDsAgainstPostgres` and
  `TestSupersededGenerationIDsDefersToInFlightProducersAgainstPostgres` with
  `ESHU_SUPERSEDED_GENERATION_PROOF_DSN`): superseded is returned; active,
  pending, failed, and a non-scope id are not; a superseded generation with a
  claimed/running reducer item (also with an expired lease) or a
  pending/retrying/claimed/running projector item is NOT returned (RED before
  the guard: 8 in-flight cases returned) and is returned once the item is
  succeeded; unleased, terminal, and other-generation items do not defer it.
- `go test ./cmd/reducer -run SupersededGenerationDrain`: the production
  `SharedProjectionRunner.IntentReader` implements the port.
