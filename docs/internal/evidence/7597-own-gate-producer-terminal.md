# #7597 Own Gate Bounded By Its Producer

The own-generation gate of `deployable_unit_correlation` and
`workload_materialization` (#6184) deferred in a non-counting class while the
scope generation's relationship generation was inactive. Only cross-repo
resolution, run by the `deployment_mapping` (DM) item, writes that generation
row. A dead-lettered DM writes no row, so the consumer retried forever with
`attempt_count` frozen at 1 (the 614 `retrying` items on QA, issue comment of
2026-10-06). This note records the binding arbiter ruling (Option E), the
proof, and the measured cost.

## Behavior

`crossrepo.EvaluateOwnGate` (`go/internal/reducer/crossrepo/own_gate.go`) runs
at both call sites (`deployable_unit_correlation.go`,
`correlated_workload_projection_input_loader.go`). Only when the own
generation reads inactive, with candidates present and both lookups wired, it
reads the newest DM row of the same `(scope_id, generation_id)` and the
scope's active generation (`postgres.NewResolutionProducerLookup`):

| producer state | result |
| --- | --- |
| scope active generation is not the intent's | unchanged deferral |
| `pending`, `claimed`, `running`, `retrying` | unchanged deferral, message names DM id, status, failure_class; no time bound |
| `superseded`, `succeeded`, other | unchanged deferral with the producer detail |
| `dead_letter` (and legacy `failed`) | counting `*_producer_dead_lettered` error |
| no DM row | deferral until `crossscope.ProducerReadinessMaxWait` (30 min) from `ReadinessCycleAnchor`, then counting `ProducerMissingError` (no class) |
| lookup error | `factload.ClassifyFactLoadError`, never a deferral |
| own lookup error | unchanged deferral, producer not read |

Decisions inside the ruling, recorded here: legacy `failed` counts like
`dead_letter` (FACT: replay already treats both as terminal,
`WHERE status IN ('dead_letter', 'failed')`); `succeeded` keeps the deferral
(a refinalize reopen of the DM, not a dead letter, is the path that clears it).

Wake: a reducer `ReplayFailedWorkItems` that reopens a DM row reopens, in the
same statement, the `dead_letter` consumers of the same scope generation in
the two producer-terminal classes (`replayWakeProducerConsumersCTE`). Both
replay templates and the wake share `replayResetSetClause`, which keeps retry
evidence (`attempt_count = GREATEST(attempt_count, 1)`, pinned by
`TestRecoveryStoreReplayFailedWorkItemsPreservesRetrySemantics`) and now sets
`reopened_at = $1` on every reopened row. Woken ids are returned in
`ReplayResult.WokenConsumerIDs` and are not counted against `Limit`.

Unchanged: `nonCountingReducerRetryFailureClasses`, retry delay, worker
counts, batch sizes, the corpus fence, and Path B (corpus fence held by a DM
retrying on `cross_repo_backward_evidence_not_ready`; out of scope).

## Prove-The-Theory-First Measurement

Performance Evidence: Postgres 18.6 container, own database `p7597_owngate`,
schema bootstrapped with `ApplyBootstrapWithoutContentSearchIndexes`, seeded
with 420,001 `fact_work_items` rows (20,000 scopes, 2 generations each, 10
reducer domains per generation, 400 dead-lettered DMs each with 2
producer-terminal consumers), `ANALYZE` run. Production SQL text was printed
from `resolutionProducerStateSQL` and `buildReplayFailedWorkItemsQuery`.

Producer lookup (`EXPLAIN (ANALYZE, BUFFERS)`, custom and generic plan):
`Index Scan using ingestion_scopes_pkey`, then
`Index Scan using fact_work_items_scope_generation_idx` with
`Index Cond: (scope_id = s.scope_id) AND (generation_id = $2)`, filter on
stage/domain removing 10 rows, top-1 sort. Execution 0.054 to 0.163 ms,
5 to 17 shared buffers. The existing index serves it; no index was added. The
read runs only on the branch that already returns a deferral, so the
succeeding path pays nothing.

Wake UPDATE (inside `BEGIN`/`ROLLBACK`):

| replay | rows replayed | rows woken | main (no wake) | with wake |
| --- | --- | --- | --- | --- |
| scoped, 3 scopes, `retry_exhausted` | 3 | 6 | not measured | 2.26 ms |
| broad, `retry_exhausted`, all scopes | 400 | 800 | 14.3 to 14.7 ms (3 runs) | 34.3 to 38.9 ms |

The consumer scan uses `fact_work_items_dead_letter_poison_idx`
(`(scope_id, generation_id) WHERE status = 'dead_letter'`), one index search
per replayed DM, then `fact_work_items_pkey` per woken row. The added time
tracks the extra rows updated (about 30 us per woken row, against about 36 us
per replayed row in the same statement). Replay is an operator path, not a hot path.

No-Regression Evidence: the claim, ack, retry and dead-letter SQL is
unchanged. `go test ./internal/storage/postgres/ -count=1` passes (all
replay-template shape tests, with the fake result rows given the third
`woken` column), as do `./internal/reducer/...`, `./internal/recovery/...`,
`./internal/runtime/...`, `./cmd/reducer/...` and
`./cmd/golden-corpus-gate/...`.

## RED And GREEN

Live tests run with
`ESHU_REDUCER_FAIRNESS_PROOF_DSN=postgres://...@localhost:55432/p7597_owngate`.

- `TestOwnGateProducerDeadLetteredConsumerDeadLettersLive` (both consumers).
  RED (main behavior): `cycle 2: status=retrying
  class=deployable_unit_correlation_resolution_not_ready attempts=1 ... want it
  to advance`. GREEN: attempts 1, 2, 3 then `dead_letter` in the
  producer-terminal class, `failure_message` names `dm-1`.
- `TestOwnGateProducerRetryingConsumerKeepsDeferringLive`: 12 cycles stay
  `retrying` in the non-counting class with `attempt_count = 1`; the message
  names `dm-1`, `status=retrying`, `cross_repo_backward_evidence_not_ready`.
  RED: the message did not name the producer.
- `TestReplayDeadLetteredDeploymentMappingWakesProducerTerminalConsumersLive`
  (unbounded and bounded). RED: `duc-1 after replay = {status:dead_letter
  ...}`. GREEN: only the same-scope-generation consumers reopen, with
  `reopened_at` = replay time; the older-generation, other-scope,
  other-class and retrying controls are untouched. The repo-dependency
  runner's `ReplayWorkloadMaterializationOutcome` reports `NotScheduled`
  (fail closed, #7670) before the replay and `Scheduled` after it; the DM then
  activates and the woken workload item succeeds and acks.
- Unit, RED against a main-equivalent stub and GREEN now:
  `TestEvaluateOwnGateProducerMatrix`, `...DeadLetterNamesProducer`,
  `...NonTransientLookupErrorIsNotADeferral` (crossrepo);
  `TestOwnGateDeadLetteredProducerIsACountingErrorNotTheDeferral`,
  `TestOwnGateMissingProducerIsBoundedByElapsedTime`,
  `TestOwnGateProducerLookupErrorIsClassifiedNotDeferred`,
  `TestRecordReducerResultLogsNonCountingFailureAsDeferred`,
  `TestRecordReducerResultKeepsProducerTerminalFailureAsFailed` (reducer);
  `TestDeliberatelyCountingClassesAreNeverEnrolled` (postgres). Controls that
  pass on both sides by design: `TestOwnGateNonTerminalProducerKeepsTheDeferral`,
  `TestEvaluateOwnGateSkipsProducerReadOutsideTheDeferringBranch`,
  `TestRecordReducerResultUnwiredPredicateKeepsDeferralFailed`,
  `TestReducerQueueFailDeadLettersProducerTerminalClassesPastAttemptBudget`.

The wake commits before the runner can observe the consumer dead-lettered: the
runner replays workload items only for repo-dependency intents the DM handler
writes, the DM runs only after its replay statement commits, and that same
statement already moved the consumer to `pending`.

The golden-corpus gate needs no new entry:
`TestEveryNonCountingFailureClassHasPreMaintenanceDecision` decides only
non-counting classes, and the new classes are counting, so a retrying row in
them is `live` and a dead-lettered one is terminal.

## Concurrency

Conflict domain: `fact_work_items` rows of one `(scope_id, generation_id)`.
Lock order inside one replay statement: replayed rows (unbounded: wait;
bounded: `SKIP LOCKED`), then woken consumers. The `DISTINCT` producer set
(HashAggregate) and the hashed `NOT IN (SELECT work_item_id FROM replayed)`
read the whole `replayed` CTE before `LockRows` takes any consumer, and the
consumer lock is `FOR UPDATE OF consumer SKIP LOCKED`, so the wake never waits
and adds no edge to a lock cycle. A row the replay predicate selected is left
to `replayed`, so no row is updated twice in one statement. `dead_letter`
rows are never claimable, so claim and the wake do not contend on consumers.

`TestReplayWakeConcurrentReplaysAreIdempotentLive` races, on independent
connections, per round: two identical targeted replays, one broad stage-wide
replay that selects the consumers directly, one bounded replay in reverse
scope order, and a consumer-domain claimer; 15 rounds of 6 scopes. It requires
no error and every row reopened exactly once (85 to 88 of 180 consumers per run
went through the wake, the rest through the broad replay). Passing 5 of 5 runs.
Mutation: with `SKIP LOCKED` removed from the wake, a 5-run `-count=5` pass
failed with 9 `SQLSTATE 40P01` "deadlock detected" errors, so the test bites;
restoring it gave 5 of 5 passes.

## Observability

Observability Evidence: `Service.IsDeferredFailureClass`
(`postgres.IsNonCountingReducerRetryFailureClass`) records a retryable
failure in a non-counting class as `eshu_dp_reducer_executions_total{status="deferred"}`
with WARN `reducer execution deferred` and `failure_class`; producer-terminal
and every other failure stay `status="failed"` with ERROR
`reducer execution failed` (`TestRecordReducerResult*`). The deferral message
names the producer. The wake logs INFO `replay reopened producer-terminal
consumers of replayed deployment_mapping work` (`woken_consumers`,
`sample_work_item_ids`, at most 10). The producer read is timed by
`eshu_dp_postgres_query_duration_seconds`. In-repo consumers of
`status="failed"` (EshuReducerErrorRateHigh and its runbook in
`deploy/observability/alerts.yaml` and `prometheus-rule.yaml`, the
`Failed Executions` panels) now exclude readiness waits; the denominator is
unchanged. Documented in
`docs/public/reference/telemetry/metrics-reducer-storage.md` and
`docs/public/observability/telemetry-coverage.md`.

## Not Checked

- The QA path for the 614 retrying items (no QA access in this lane).
- Whether a DM is always co-enqueued with its DUC/WM consumers per generation
  (the missing-producer bound assumes it usually is).
- External alerting outside this repository that keys on `status="failed"`.
- The live repro on QA after deploy.
