# #11327: the go-evidence lock closes the fence's commit-after-snapshot miss

Refs #11327, #11197, #10880. Design B of `.claude/arbiter-11327-fence-b.md`: a shared
transaction-scoped advisory lock keyed by the go scope in the impact pass's write
transaction, the exclusive form in the Go-modules fence, and a recheck of every loaded
(scope, generation) pair before the pass writes.

## The miss

A supply-chain impact pass loads Go evidence on a read-only repeatable-read snapshot
(`storage/postgres/active_go_reachability.go`), then writes in a separate transaction
(`reducer/supplychain/core/writer_retract.go`). The fence retracts a go scope's active
generation and reopens the scopes that hold a finding for the repository. A scope whose
first finding for that repository commits after the fence read its holders is not
reopened and keeps the retracted generation's verdict until its next pass; nothing
schedules one. #11325 narrowed the drain part of the window; this closes the rest.

## What changed

- The pass writes its `(go scope, generation)` pairs on `SupplyChainImpactWrite.GoEvidence`.
  The loader returns them from the same snapshot as the facts
  (`FactStore.ListActiveGoReachabilityEvidence`); a pair is a named scope whose pointer
  names an `active` generation, the same condition the recheck applies.
- The write transaction's first lock-taking statements, before the protected-scan
  authorization and the conflict-domain lock: one `pg_advisory_xact_lock_shared` per
  distinct go scope in sorted order, then one plain recheck SELECT over every pair
  (`core/go_evidence_lock.go`). A pass with no resolved pair issues neither.
- The fence takes `pg_advisory_xact_lock` on the same key right after its timeouts and
  before the readiness check, the scope row, the generation and the holders
  (`gomodules/fence_goevidence.go`); the lock-order comment in `gomodules/fence.go` is
  rewritten.
- A failed recheck, or a 55P03 on the pass's shared-lock statement, is
  `SupplyChainImpactGoEvidenceChangedError`, failure class
  `supply_chain_impact_go_evidence_changed`: retryable, non-counting, enrolled beside
  `supply_chain_impact_supports_not_ready` (`reducer_queue_readiness_sql.go`), not parked
  (`reducer_waiting.go`), tolerated by the golden-corpus drain decision, and counted in
  the stale-gate guard digests (batch and single re-pinned; the funnel is unchanged).

## Why B and not a row lock

A row lock on the go scope row would queue the ack, the ingestion upsert, the retirement
fence and the fence behind every in-flight pass write transaction, and write the hot
tuple on every pass (about 190 WAL bytes in the shim). The advisory lock is taken by the
pass and the fence only. Shim (toy, PostgreSQL 18.6, 16 clients, insert-only
transactions, two runs): none 345 and 338 WAL bytes per transaction, advisory shared 339
and 338, row `FOR SHARE` 532 and 529; throughput within noise. That is theory proof only;
the real-writer measurement is owed (plan below).

## Ack-path census (source, origin/main 9fa8f6049a)

`projector_queue_ack.go` `Ack` (:29), one transaction under its own lock timeout, lock
order comment :65-73 (every blocking path takes the scope row first). Statements in order:
:74 `UPDATE ingestion_scopes` of the acked scope (`projector_queue_sql.go:129`, sets
`active_generation_id`), :78 `UPDATE fact_work_items` of that scope's projector item
(`sql:137`), :92 `checkAckDeltaBaseline`, :106 `supersedeProjectorObsoleteGenerationsQuery`
(`projector_queue_marker_fence_sql.go:137`), :111 `supersedeProjectorActiveGenerationQuery`
(`sql:44`), :133 `wakeTerminalGenerationActiveWaits`, :141 `activateProjectorGenerationQuery`
(`sql:63`), :150 `activation.Insert`, :155 `oweProducerActivationObligation`, :162
`wakeAckActivationWaits` (other scopes' reducer wait rows,
`reducer_waiting_generation_wake.go:372`), :165 commit. The other writers of the same go
scope row, each taking it first: the ingestion upsert (`ingestion_queries.go:83`), the
repository-retirement fence (`repository_retirements_fence_sql.go:69`, after a
`LOCK TABLE fact_work_items`), the reconcile update (`projector_queue_sql.go:241`) and the
fence detach (`gomodules/fence.go`). None of them takes the advisory lock, and the pass
write transaction touches none of the rows they lock after the scope row (no
`fact_work_items`, no `scope_generations` write, no `ingestion_scopes` write), so no cycle
exists with the lock first.

## How a successor activation reaches holders (ruling 5.4)

A go-reachability generation raises its own `supply_chain_impact` pass
(`projector/supply/chain/impact/intents.go`, `buildGoReachabilityIntent`, #10077), which
fans out through `ChangedCVEIDs`. The fan-out reads holders by CVE from the supports table
plus the scopes whose item at their active generation is claimed or running, read first
(`reducer/supplychain/core/HOLDER_FANOUT.md:45-52`, the in-flight arm). A holder whose
first finding commits late is in flight when the fan-out reads, so the in-flight arm
reaches it. The change leaves that path unchanged and the ack takes no lock. NOT_CHECKED
live: no test here drives a successor against a late first-finding commit; if the in-flight
arm misses it, that is the same race on another actor and returns to the arbiter.

## Is the fence enabled anywhere

From source: the coordinator runs the fence only for a collector instance that enables
`derive_from_go_modules` (`coordinator/activation_go_modules.go:136-146`, `derivation.Enabled()`),
which is operator configuration. Whether any environment enables it is NOT_CHECKED from this
repository.

## The 2 s question

The fence's exclusive wait is bounded by its own `lock_timeout` of 2000 ms
(`gomodules/fence.go`, `fenceTimeoutsQuery`). A pass write transaction that holds the
shared lock longer makes that fence attempt return 55P03 with nothing detached; the
coordinator retries the page next tick, and a pass that arrives while the fence waits queues
behind it (fair lock queue) and proceeds without error once the fence times out
(`TestGoEvidenceFenceTimesOutBehindALongPassAndDetachesNothingLive`). No metric records the
pass write-transaction length today. Evidence in hand: the lab real writer ran 0.4 to 0.7 s
per 3,000 findings on an idle database; the QA 10643j window showed a 7.64 s mean per fact
insert batch on a saturated database (`.claude/qa-accounting-10643j-2026-11-22.md`), which is
only the insert and not specific to one repository. The threshold at D3a: a fence that times
out on the advisory lock for the same scope on three consecutive ticks returns to the arbiter.
`eshu_dp_go_fence_advisory_lock_wait_seconds` and
`eshu_dp_go_fence_advisory_lock_timeouts_total` show it.

## Isolation

The recheck is a plain SELECT after the shared lock is granted, so it sees the fence's commit
only under READ COMMITTED. Production opens the write transaction through
`SQLDB.Begin` (`adapters.go:267`, `BeginTx(ctx, nil)`): the server's
`default_transaction_isolation`, read committed unless an operator changed it. The writer
does not set the level (a `SET TRANSACTION` first statement would move the writer setting off
the first-statement pin of `supply_chain_impact_supports_contract_test.go`). Instead the recheck
rows carry `current_setting('transaction_isolation')`, and a write transaction that is not
`read committed` fails with a plain counting error
(`TestWriteFailsLoudWhenTheWriteTransactionIsNotReadCommitted`). The pass's shared-lock statement
has no `lock_timeout` of its own; the connection's applies, and a 55P03 maps to the typed class.

## Proofs

| Claim | Test |
| --- | --- |
| The key is its own derivation | `TestGoEvidenceLockKeyDiffersFromTheConflictDomainKey` |
| Lock, then recheck, before everything else; no pair means neither | `TestWriteLocksGoEvidenceThenRechecksBeforeAnythingElse`, `TestWriteWithNoResolvedGoEvidenceIssuesNeitherStatement` |
| A rewrite cannot move them off the front unnoticed (seeded violations) | `TestGoEvidencePrologueViolationNamesTheSeededShapes` |
| A changed generation aborts with the typed class and writes nothing | `TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration` |
| N aborts spend no attempt; a genuine failure still counts | `TestReducerQueueGoEvidenceChangedNeverSpendsAnAttempt`, `TestReducerQueueClaimDoesNotCountGoEvidenceChanged` |
| 55P03 maps only on the shared-lock statement | `TestClassifyGoEvidenceLockTimeoutMapsOnlyTheSharedLockStatement` |
| The recheck covers exactly the loaded pairs | `TestGoEvidenceLoadReturnsExactlyTheResolvedPairs`, `TestGoEvidenceRecheckCoversExactlyTheLoadedPairsLive` |
| Fence order and timeout detach nothing | `TestFenceTakesTheExclusiveGoEvidenceLockBeforeAnythingElse`, `TestFenceLockTimeoutDetachesNothingAndIsObserved` |
| Isolation guard | `TestWriteFailsLoudWhenTheWriteTransactionIsNotReadCommitted` |
| AC1: the miss is red on main, aborted now | `TestGoEvidenceFenceBetweenLoadAndWriteAbortsThePassLive` |
| Pass first, fence waits and reopens | `TestGoEvidenceFenceWaitsForAPassHoldingTheSharedLockLive` |
| 3 s pass: fence timeout, nothing detached, second pass queues | `TestGoEvidenceFenceTimesOutBehindALongPassAndDetachesNothingLive` |
| 40 rounds, in-flight holder reopened, zero deadlocks | `TestGoEvidenceFenceReopensAnInFlightPassWithoutDeadlockLive` |
| Ack commits during a 3 s pass; ends on the successor | `TestGoEvidenceAckCommitsWhilePassHoldsTheSharedLockLive` |
| Two go scopes, two fences, one pass | `TestGoEvidenceMultiRepositoryPassWithParallelFencesLive` |
| AC3: 40 free-interleaving rounds | `TestGoEvidenceFreeInterleavingLeavesNoStaleVerdictLive` |
| Unchanged and expected green | `TestFenceAndAckRaceEndOnTheSuccessorLive`, `TestFenceReopensHoldersInDeadLetterLockOrderLive` |

The live tests are enrolled in the live-postgres-readiness runner (`ESHU_POSTGRES_DSN` with
`ESHU_POSTGRES_DISPOSABLE`), not the reducer contention gate, so they add nothing to its
supply-chain half. They were written, compiled and vetted here; running them, including the AC1
RED on main, is the coordinator's slot.

## No-Regression Evidence

No-Regression Evidence: baseline is `origin/main` 9fa8f6049a; after is this branch. A pass that
resolves no go scope issues no new statement. A pass that resolves N distinct go scopes
issues N advisory lock statements and one recheck SELECT (the hermetic order test shows the
exact statements); the fence issues one advisory lock statement. Advisory locks write no WAL
and touch no heap (shim: 339 and 338 WAL bytes per transaction against 345 and 338 without
the lock; the row-lock alternative cost about 190 bytes). The lock is transaction-scoped and
shared passes do not wait on each other. Conflict domain: the go scope id; the fence waits
only for in-flight passes that loaded that scope. Lock order: pass takes the shared go-evidence
lock first, then the conflict-domain lock and its rows; the fence takes the exclusive lock first,
then the scope row, the generation and the holders. The real-writer measurement of pass
write-transaction duration and WAL per pass with and without the change (ruling 4.9) is NOT RUN
here: it needs a coordinator slot on the real writer path, and the toy shim is not that proof.

Observability Evidence: `eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total{reason}`
counts passes aborted by the recheck or the shared lock timeout,
`eshu_dp_go_fence_advisory_lock_timeouts_total` counts fence attempts that hit the lock timeout,
and `eshu_dp_go_fence_advisory_lock_wait_seconds` records the fence's wait, granted or timed out.
Documented in `docs/public/reference/telemetry/supply-chain-impact-supports.md` and the reducer
coverage row. At 3 AM, a rising abort rate or timeout count means fences and passes keep colliding
on one repository; the retrying intents appear under failure class
`supply_chain_impact_go_evidence_changed` on the queue's failure-class series and the service
reports them as deferred.
