# #6686: reducer work for a not-yet-active generation

## Problem

Reducer intents for a newer generation G2 are enqueued inside the projector's
`Project`, before the projector's `Ack` activates G2. In the default claim mode
nothing holds them back. A worker that claims one in that window ran the
production `GenerationFreshnessCheck`, which reported G2 as not current because
G1 was still `active_generation_id`. `Runtime.execute` turned that into
`ResultStatusSuperseded`, the service acked it, and the row read `succeeded`.
Nothing reopened it after the projector activated G2, so that generation's work
was lost with no error and no dead letter.

Root-Cause Evidence: the live regression
`TestReducerContentionGatePreActivationGenerationRunsAfterActivation`
(`go/internal/storage/postgres/reducer_queue_pre_activation_live_test.go`)
drives production `ReducerQueue` Enqueue/Claim/Ack/Fail,
`Runtime.Execute` with the production `NewGenerationFreshnessCheck`, and
`ProjectorQueue.Ack` against a real PostgreSQL 18. Before the fix it failed
with `pre-activation: status = "succeeded", handler calls = 0`.

## Fix

`NewGenerationFreshnessCheck` now reads, in one statement, the scope's active
generation, the intent generation's status, and whether the intent generation
sorts after the active one by `(ingested_at, generation_id)`. It returns:

| Case | Result |
| --- | --- |
| intent generation is active, scope has no active generation, or scope unknown | `(true, nil)`: handler runs (unchanged) |
| intent generation is `pending` and sorts after the active generation | `(false, GenerationNotYetActiveError)`: retryable, class `generation_activation_not_ready` |
| older, superseded, failed, or missing generation | `(false, nil)`: terminal supersession (unchanged) |

The ordering matches the projector's own supersession SQL
(`supersedeProjectorObsoleteGenerationsQuery`, the running-work heartbeat check)
and the reducer claim's `superseded_stale_reducer_generations` CTE, all of which
decide the winning generation by `ingested_at` with `generation_id` as the
tie-break. `observed_at` was considered and rejected: a generation observed
earlier but ingested later is the one the projector activates, and ordering by
`observed_at` would still ack its work as superseded.

`Runtime.execute` already wraps a check error with `%w` and returns it, so the
service routes it to `WorkSink.Fail`. That holds on the per-item path and the
batch path (`service_batch.go` calls the same `Executor.Execute` and per-item
`WorkSink.Fail`). The one handler that calls the check itself,
`CloudInventoryAdmissionHandler`, also wraps and returns the error. The class is
enrolled in `nonCountingReducerRetryFailureClasses`, which feeds both the Go
retry decision and the SQL claim attempt-count `CASE`. Waiting on the projector
therefore never erodes the retry budget or dead-letters.

The wait is bounded by the projector lifecycle, not by the retry budget. If the
projector acks, the generation becomes `active` and the next claim runs the
handler. If the projector dead-letters, `failProjectorWorkQuery` marks the
generation `failed`. If a newer generation wins, the projector marks it
`superseded`. In both of those cases the next attempt gets `(false, nil)` and
acks terminally.

## Concurrency

- Conflict domain: one `fact_work_items` reducer row, plus the read-only
  `ingestion_scopes` and `scope_generations` rows of its scope. The check
  takes no locks.
- Single snapshot: the active pointer and the intent generation's status come
  from one statement, so the check can't combine a pre-Ack pointer with a
  post-Ack status.
- Race with the projector Ack: if the Ack commits after the check reads
  `pending`, the row is retried after its retry delay and then runs. If the Ack
  commits first, the check returns `(true, nil)`. Neither order loses work.
- Duplicate delivery: a deferred row is `retrying`, not `succeeded`, so the
  claim hands it back once. After activation the handler runs once and the row
  acks `succeeded`. The live test asserts exactly one handler call across five
  deferral cycles, activation, and a final empty claim.
- Worker and lease settings in the proof: `LeaseDuration=1m`, `RetryDelay=1s`,
  `MaxAttempts=2`, and an injected queue clock. The row survived four more
  claims than `MaxAttempts` at `attempt_count = 1`.

No-Regression Evidence: the check moved from one primary-key lookup on
`ingestion_scopes` to one statement with three index lookups. Measured on
PostgreSQL 18 (`postgres:18-alpine`, local Docker) with the real
`ingestion_scopes` and `scope_generations` DDL, seeded with 2,000 scopes and
10,000 generations, then `ANALYZE`d. Results from `EXPLAIN (ANALYZE, BUFFERS)`:

| Query | Plan | Buffers | Execution |
| --- | --- | --- | --- |
| old `SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1` | Index Scan `ingestion_scopes_pkey` | shared hit=3 | 0.012 ms |
| new, custom plan, newer pending intent | Index Scan `ingestion_scopes_pkey`, Index Scan `scope_generations_scope_generation_idx`, Index Only Scan `scope_generations_scope_generation_idx` | shared hit=9 | 0.047 ms |
| new, custom plan, older intent | same three index scans | shared hit=9 | 0.052 ms |
| new, `plan_cache_mode = force_generic_plan` | same three index scans | shared hit=9 | 0.106 ms |

No plan has a sequential scan on `scope_generations`. The added cost is about
0.04 ms and 6 buffer hits per claimed intent, measured on a single local
execution per plan. That is small next to the claim statement and handler work
that already run for every intent. No end-to-end wall-time claim is made. A
deferral adds one claim plus one retry `UPDATE` per retry delay (default 30 s)
for each intent waiting on its projector. Before this change the same intent
did one claim plus one ack and then lost its work.

Observability Evidence: each deferral is durable on
`fact_work_items.failure_class` as `generation_activation_not_ready` and is
counted by the existing `eshu_dp_reducer_retry_surge_total{failure_class}`.
`failure_class` is a bounded label drawn from the closed set of self-classified
classes, never a scope or generation id. That counter, with this class value,
is the operator signal for skips caused by a not-yet-active generation. The
attempt is also recorded by `eshu_dp_reducer_executions_total{status="failed"}`
and the reducer run log, whose error text names the scope, the pending
generation, and the active generation. A dedicated counter was not added
because it would duplicate the retry-surge series for this class. No
telemetry-coverage row was added: the change registers no instrument and adds
no pipeline stage (`scripts/verify-telemetry-coverage.sh` passes), and
`docs/public/observability/telemetry-coverage.md` is grandfathered at its line
cap and may not grow.

## Commands

- `go test ./internal/storage/postgres/ -run '^TestReducerContentionGate(PreActivationGenerationRunsAfterActivation|OlderGenerationSupersessionStaysTerminal|CrossScopeReadiness)' -race -count=1`
  with `ESHU_REDUCER_FAIRNESS_PROOF_DSN` set: RED before the fix, GREEN after.
- `go test ./internal/storage/postgres/ ./internal/reducer/... ./cmd/reducer/ -count=1`
