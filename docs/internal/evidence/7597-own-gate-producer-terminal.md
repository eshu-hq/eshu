# #7597 Own Gate Bounded By Its Producer

The own-generation gate of `deployable_unit_correlation` and
`workload_materialization` (#6184) defers in a non-counting class while the
scope generation's relationship generation is inactive. Only cross-repo
resolution, run by the `deployment_mapping` (DM) item, activates that
generation. A dead-lettered DM activates nothing, so before this change the
consumer retried forever with `attempt_count` frozen at 1. Whether the 614
`retrying` items reported on QA (issue comment of 2026-10-06) are this case is
NOT verified; see Not Checked.

This note records the arbiter rulings (Option E, then the review ruling that
moved the wake to activation), the proof, and the measured cost.

## Behavior

`crossrepo.EvaluateOwnGate` (`go/internal/reducer/crossrepo/own_gate.go`) runs
at both call sites (`deployable_unit_correlation.go`,
`correlated_workload_projection_input_loader.go`). Only when the own
generation reads inactive, with candidates present and both lookups wired, it
reads the newest DM row of the same `(scope_id, generation_id)`, the scope's
active generation, and the generation's `relationship_generations` status
(`postgres.NewResolutionProducerLookup`):

| producer state | result |
| --- | --- |
| scope active generation is not the intent's | unchanged deferral |
| DM `pending`, `claimed`, `running`, `retrying` | unchanged deferral naming DM id, status, failure_class; no time bound |
| DM `superseded`, `succeeded`, other | unchanged deferral with the producer detail |
| DM `dead_letter` (and legacy `failed`) | counting `*_producer_dead_lettered` |
| no DM row, generation row exists and is not active (re-resolution, e.g. refinalize) | unchanged deferral, no time bound |
| no DM row and no generation row | deferral until `crossscope.ProducerReadinessMaxWait` (30 min) from `ReadinessCycleAnchor`, then counting `*_producer_missing` |
| lookup error | `factload.ClassifyFactLoadError`, never a deferral |
| own lookup error | unchanged deferral, producer not read |

All four counting classes come from one per-domain map
(`ownGateClassesByDomain`). None is enrolled as non-counting
(`TestDeliberatelyCountingClassesAreNeverEnrolled`). None ends in
`_not_ready`, so `TestEveryReadinessFailureClassIsEnrolled` cannot see them;
the first test is their only guard.

## Wake At Activation

Every statement that sets a `relationship_generations` row active
(`activateResolutionGenerationForClaimSQL`, `activateResolutionGenerationSQL`,
`activateGenerationSQL`) runs through `RelationshipStore.activateWithConsumerWake`.
`TestEveryRelationshipActivationWakesConsumers` enumerates the module source,
so a new activation writer cannot skip it (planted-writer RED/GREEN in
`TestActivationWriterGuardBitesOnAPlantedWriter`). One transaction:

1. the activation statement (unchanged; zero rows rolls back, no wake);
2. `SET LOCAL lock_timeout` (2 s; 55P03 or 40P01 returns the retryable
   `ActivationWakeContentionError`, the DM re-runs, activation is idempotent);
3. `lockInflightOwnGateConsumersSQL`: plain `FOR UPDATE` on the generation's
   `claimed`/`running` own-gate consumers, ordered by `work_item_id`;
4. `wakeProducerTerminalConsumersSQL`: reopens the generation's dead letters in
   the four counting classes (dead rows locked `SKIP LOCKED`), with the replay
   reset list plus `reopened_at = now`, kept in lockstep with
   `replayFailedWorkItemsTemplate` by `TestActivationWakeSetClauseMatchesReplayReset`.

A consumer's own counting dead letter runs the same wake statement once more
(`wakeAfterProducerTerminalDeadLetter`, fresh snapshot, a no-op while the
generation is inactive).

Why three pieces and not the single plain-lock UPDATE the ruling sketched
(FACT, PostgreSQL Read Committed semantics, proven below): an UPDATE evaluates
its WHERE on the statement-snapshot version of a row. A final Fail that has
not committed leaves the snapshot version `running`, so a wake filtered on
`status = 'dead_letter'` skips the row without waiting, and the dead letter
commits after the wake (lost). Step 3 waits for such a Fail before step 4
takes its snapshot. A Fail that starts after step 3 waits on the activation's
lock, commits after it, and its own post-dead-letter wake then sees the
active generation. Dead rows use `SKIP LOCKED` because their only other
writers reopen or retire them (recovery and admin replay, the claim-time
supersede sweep, a concurrent wake); a plain lock there would add a wait edge
against the admin replay's unordered row locks.

Replays are unchanged from main. `ReplayFailedWorkItems` and the admin replay
wake nobody and do NOT stamp `reopened_at`; only the wake does. The missing
`reopened_at` stamp on operator replays is a separate issue.

### Lock order

1. claimant DM work row (`claimant` CTE, claim-fenced path only);
2. `relationship_generations` rows (deactivate, then insert or update);
3. in-flight own-gate consumer rows of the generation, ascending
   `work_item_id`, plain `FOR UPDATE` (waits at most `lock_timeout`);
4. dead-lettered own-gate consumer rows, `SKIP LOCKED` (never waits).

Other writers of those rows: the consumer's Fail, Ack and heartbeat lock only
their own row; the claim and the claim-time supersede sweep use `SKIP LOCKED`;
both replays touch only `dead_letter`/`failed` rows (never step 3's rows), and
step 4 skips any row they hold. Refinalize's `LOCK TABLE ... EXCLUSIVE`
conflicts at step 1 exactly as before (`TestRecoveryClaimFence*` pass). No
writer holds a step 3 or step 4 row and then waits for a step 1 or 2 lock.

## Prove-The-Theory-First Measurement

Performance Evidence: measured on the working tree over `bcf84931b`, local
Docker Postgres 18.6 (`eshu-p10-pg`), own database `p7597_owngate`, schema
`p7597_bench` bootstrapped with `ApplyBootstrapWithoutContentSearchIndexes`
and seeded with 420,001 `fact_work_items` rows (20,000 scopes, 2 generations,
10 reducer domains each, 400 dead-lettered DMs with 2 counting consumers each),
`ANALYZE`d. SQL text was printed from the shipped constants. This is not a
reference resource profile; the numbers are same-machine relative.

| statement | plan | time |
| --- | --- | --- |
| producer lookup | `ingestion_scopes_pkey`, `relationship_generations_pkey`, `fact_work_items_scope_generation_idx` | 0.083 ms warm |
| in-flight lock, no consumer in flight | `fact_work_items_reducer_live_lease_uniq`, 0 rows | 0.052 ms |
| in-flight lock, 2,000 other rows claimed | `fact_work_items_scope_generation_idx` cond `(scope_id, generation_id, status = ANY(claimed, running))`, 0 rows | 0.028 ms warm |
| wake, no dead consumer | `fact_work_items_dead_letter_poison_idx`, 0 rows, no buffer dirtied | 0.081 to 0.405 ms |
| wake, 2 dead consumers | same index, 2 rows updated; rerun 0 rows | 15.1 ms cold, 0.107 ms rerun |

The common case touches zero rows and writes no heap page. End to end, 300
activations on an isolated schema (Go client, same container): main's single
statement p50 62 us, p95 77 to 92 us; the activation transaction with the wake
p50 259 to 262 us, p95 343 to 356 us (two rounds). About 200 us per DM
activation, once per scope generation.

No-Regression Evidence: claim, ack, retry, dead-letter and replay SQL is
unchanged (the replay templates are byte-identical to main). The producer read
runs only on the branch that already defers.
`go test ./internal/storage/postgres/ ./internal/reducer/... ./internal/recovery/... ./cmd/reducer/...`
passes, and the claim-token fence live tests pass on the transactional
activation path.

## RED And GREEN

Live tests ran with
`ESHU_REDUCER_FAIRNESS_PROOF_DSN=postgres://...@localhost:55432/p7597_owngate`.

- `TestOwnGateProducerDeadLetteredConsumerDeadLettersLive`: RED (main
  behavior) `cycle 2: ... class=deployable_unit_correlation_resolution_not_ready
  attempts=1`; GREEN attempts 1, 2, 3, then `dead_letter` in the counting
  class naming the producer. `TestOwnGateProducerRetryingConsumerKeepsDeferringLive`:
  12 cycles stay non-counting at `attempt_count = 1`.
- `TestActivationWakesOwnGateConsumersLive`, all three activation writers.
  RED with the wake statement removed: `duc-1 after activation =
  {status:dead_letter ...}`. GREEN: only the generation's three counting dead
  letters reopen (attempts kept at 3, `reopened_at` set); the older-generation,
  other-scope, other-class and retrying controls are untouched;
  `ReplayWorkloadMaterializationOutcome` goes NotScheduled to Scheduled (#7670)
  and the woken workload item succeeds.
- `TestActivationWaitsForAnInflightFinalFailLive`. RED with step 3 removed:
  `activation finished (<nil>) while the consumer's Fail held its row`.
- `TestFinalFailAfterTheActivationLockIsWokenLive`. RED with the Fail-side
  wake removed: `c-1 = {status:dead_letter ...}, want reopened`.
- `TestActivationLockTimeoutFailsRetryablyLive`: a held consumer row makes the
  activation return a retryable `ActivationWakeContentionError` (55P03) with no
  generation row; the retry activates and wakes.
- `TestOwnGateWaitsThroughRefinalizeReResolutionLive` (real
  `RefinalizeScopeProjections`). RED with the re-resolution rule removed:
  `cycle 1 ... class=workload_materialization_producer_missing ... after 2h0m0s
  (bound 30m0s)`. GREEN: 5 cycles non-counting; the re-enqueued DM's
  activation wakes the dead-lettered consumer and the deferring one succeeds.
- Unit: `TestEvaluateOwnGate*`, `TestOwnGateCountingClassesAreDistinctAndOneSourced`
  (crossrepo); `TestOwnGate*`, `TestRecordReducerResult*` (reducer);
  `TestDeliberatelyCountingClassesAreNeverEnrolled`,
  `TestReducerQueueFailDeadLettersProducerTerminalClassesPastAttemptBudget`,
  `TestActivationWakeSetClauseMatchesReplayReset` (postgres).

The golden-corpus gate needs no new entry:
`TestEveryNonCountingFailureClassHasPreMaintenanceDecision` decides only
non-counting classes; the new classes are counting.

## Concurrency

`TestActivationWakeInterleaveLive` (`go/internal/query/admin`) races, per
iteration on independent connections, the claim-fenced activation, a final
counted Fail of one consumer, the public admin replay of a second dead-lettered
consumer (retried on 40P01 with exact accounting), and a consumer-domain claim
whose sweep supersedes an older-generation row. It requires zero 40P01 on the
activation, zero lost wakes, and each consumer reopened exactly once (admin
reported it XOR the wake stamped `reopened_at`).

| run | iterations | 40P01 activation / Fail / admin / claim | lost wakes |
| --- | --- | --- | --- |
| shipped, 5 runs | 1,000 | 0 / 0 / 0 / 0 | 0 |
| step 3 removed, 3 runs | 600 | 0 / 0 / 0 / 0 | 1, 6, 5 |
| Fail-side wake removed, 3 runs | 600 | 0 / 0 / 0 / 0 | 1, 11, 9 |

## Observability

Observability Evidence: a retryable failure in a non-counting class records
`eshu_dp_reducer_executions_total{status="deferred"}` with WARN
`reducer execution deferred` and `failure_class`; the counting classes stay
`status="failed"` with ERROR `reducer execution failed`
(`TestRecordReducerResult*`). The deferral message names the producer and, for
re-resolution, the generation status. The activation logs INFO `relationship
generation activation reopened own-gate consumers` (`scope_id`,
`generation_id`, `woken_consumers`); the Fail-side check logs INFO `own-gate
dead letter reopened: relationship generation already active`, and ERROR when
its statement fails. An activation lock timeout surfaces as a retrying DM with
`ActivationWakeContentionError` text. Every statement is timed by
`eshu_dp_postgres_query_duration_seconds`.

## Not Checked

- The QA path for the 614 retrying items, and whether they are this case.
- Whether a DM is always enqueued with its DUC/WM consumers per generation.
- External alerting outside this repository that keys on `status="failed"`.
- The live repro on QA after deploy.
