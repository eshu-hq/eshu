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

`SelectPartitionBatch` makes ONE bounded lookup over the distinct generation ids
of the scan window through the optional `worker.SupersededGenerationReader`
port and drains rows on a superseded generation as stale, before the acceptance
filter and the readiness gate. The Postgres implementation is
`SharedIntentStore.SupersededGenerationIDs`:

```sql
SELECT generation_id FROM scope_generations
WHERE generation_id = ANY($1::text[]) AND status = 'superseded'
```

The predicate is the terminal `superseded` status (`go/internal/scope/scope.go`
`allowedGenerationTransitions`), not "not the active generation", which would
race with activation and drop a pending generation's live intents. Ids that are
not scope generations (repo_dependency relationship-generation ids) match
nothing and keep today's behavior. A reader without the port keeps the old
behavior byte for byte; a lookup error fails the selection. The code_calls and
repo_dependency runners do not go through `SelectPartitionBatch` and are not
touched. The SQL does not enforce that `superseded` is terminal on every writer;
that gap is tracked in #7130 and is independent of this drain.

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
per selection pass. Expected backlog effect (not yet measured on ops-qa): the drain stops the
permanent rescan of the 128 + 128 orphaned pending rows on every partition cycle.

Observability Evidence: `eshu_dp_shared_projection_stale_intents_total` gains a
closed `reason` attribute (`acceptance_mismatch`, `generation_superseded`); a
separate log line `shared projection drained intents of superseded generations`
carries `stale_reason`; `PartitionProcessResult.SupersededGenerationIntents`
carries the count. The blocked log line is renamed to `shared projection skipped
intents until their prerequisite graph phase is committed` (the gate is the
`workload_materialization` / `canonical_nodes` phase, not semantic readiness) and
gains `readiness_phase`. After this change superseded rows never reach the gate,
so `blocked_count` and `blocked_intent_wait_seconds` cover only non-superseded
generations.

## Proof

- `go test ./internal/reducer/intents/shared/worker -count=1`:
  `TestSelectPartitionBatchDrainsSupersededGenerationIntent` and
  `TestSelectPartitionBatchPropagatesSupersededLookupError` failed before the
  change (stale ids empty; lookup error swallowed) and pass after;
  `TestSelectPartitionBatchKeepsActiveGenerationBlockedOnMissingPhase`,
  `TestSelectPartitionBatchLeavesSuccessorGenerationUntouched` (one lookup over
  the distinct batch generations), and
  `TestSelectPartitionBatchWithoutSupersededReaderIsUnchanged` pin the
  boundaries; `TestProcessPartitionDrainsSupersededGenerationWithReasonTelemetry`
  proves the full cycle, counter reason, and log.
- `go test ./internal/storage/postgres -run SupersededGenerationIDs` (plus the
  env-gated `TestSupersededGenerationIDsAgainstPostgres` with
  `ESHU_SUPERSEDED_GENERATION_PROOF_DSN`): superseded is returned; active,
  pending, failed, and a non-scope id are not.
- `go test ./cmd/reducer -run SupersededGenerationDrain`: the production
  `SharedProjectionRunner.IntentReader` implements the port.
