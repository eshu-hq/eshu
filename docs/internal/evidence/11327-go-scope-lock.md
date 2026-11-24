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
- The write transaction's closing statements, after the pass-state write and just before
  the commit (the arbiter's amendment, lock LAST): one `pg_advisory_xact_lock_shared` per
  distinct go scope in sorted order, then one plain recheck SELECT over every pair
  (`core/go_evidence_lock.go`), then only the owed marker. A pass with no resolved pair
  issues neither lock nor recheck. A stale pair rolls the whole transaction back, so an
  abort costs the pass's writes (`recheck_aborted` on the write-tx histogram shows how much).
- The owed marker moved. `RecordFanOutOwed` (`core/writer_retract.go`, query
  `markSupplyChainImpactFanOutOwedQuery`, `storage/postgres/supply_chain_impact_fanout_inflight.go`)
  updates the pass's own `fact_work_items` row, the row a fence's holder reopen updates. With
  the lock last and the marker before it, the pass would hold that row while waiting for the
  shared lock and the fence would hold the exclusive lock while waiting for the row: a
  deadlock, and the census guard read RED on it. The marker now runs after the lock, from the
  same in-memory change set, so its value is unchanged. This is one statement outside the
  ruling's "last statements" wording and is called out for the arbiter.
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

## Why the lock goes last, and what makes that safe

With the lock first it would be held for the whole write transaction (seconds on a loaded
database), and the fence would wait that long and time out at 2 s. Last, it is held for a
recheck, one marker update and a commit. That is deadlock-free only if the transaction holds
nothing the fence needs while it waits: the fence, holding the exclusive lock, reopens holder
work items (`fact_work_items`) and detaches the go scope (`ingestion_scopes`,
`scope_generations`). Two guards pin it:

1. `TestImpactWriteTransactionTakesNothingTheFenceNeedsBeforeTheLock` runs the real writer over
   the real storage transaction (protected-scan authorization and publication, fact upsert,
   supports, winners capture, retraction, pass state, owed marker, Go evidence) and fails if any
   statement before the lock names `fact_work_items` or writes or row-locks `ingestion_scopes`
   or `scope_generations`. Plain reads are allowed: they take no row lock.
   `TestCensusViolationNamesTheSeededStatements` is its seeded RED/GREEN pair, and
   `TestProtectedPublicationFunctionsTouchNoFenceRows` applies the same rule to the SQL bodies of
   `authorize_impact_publication_v1` and `publish_impact_publication_v1` (migrations 268, 269),
   which the statement text cannot show. Mutation proof: moving the owed marker ahead of the lock
   fails both this test and `TestWriteLocksGoEvidenceThenRechecksLastBeforeCommit` (run at this
   commit, then reverted).
2. `TestGoEvidenceProtectedPassAndFenceNeverDeadlockLive` and
   `TestGoEvidenceFenceReopensAnInFlightPassWithoutDeadlockLive`: 40 rounds each, the pass's own
   scope already a holder, the fence reopening that scope's work item while the pass waits. The
   protected-scan round runs as the least-privilege worker login. These are written, not run:
   the coordinator's slot runs them. If either fails, the arbiter's fallback applies (lock first,
   the fence's advisory wait bounded at 10 s, the 2,000 ms timeout restored before the detach).

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

## The other pointer writers (source, origin/main 9fa8f6049a plus this branch)

None of them takes the advisory lock and none is changed. For a go scope's pointer:

| Writer | Can it change a go scope's pointer | How holders get re-derived |
| --- | --- | --- |
| Ack (`projector_queue_ack.go:74,141`) | Yes: sets the successor, supersedes the old generation. | The successor generation raises its own impact pass (`projector/supply/chain/impact/intents.go`, #10077), whose `ChangedCVEIDs` fan-out reads holders by supports plus the in-flight arm, read first (`reducer/supplychain/core/HOLDER_FANOUT.md:45-52`). A pass in flight is named by the in-flight arm and a pass that committed is named by its supports, so a late first-finding commit is covered by reading. NOT verified live. The recheck also aborts a pass whose loaded pair the ack changed before the lock. |
| Ingestion upsert (`ingestion_queries.go:83,121`) | Yes, when the incoming generation arrives with status active (`activeGenerationID`); otherwise it keeps the pointer. | The same projector item and impact intent as the ack. The enqueue path of a directly active generation was not read beyond the `INSERT INTO fact_work_items` assertion in `ingestion_test.go:114`; NOT_CHECKED. |
| Retirement fence (`repository_retirements_fence_sql.go:69`, `fenceNullActivePointerQuery`) | Yes: NULLs the pointer and supersedes every generation of the repository's own scopes and its derived `go-reachability://` scope. | It does not re-derive holders and reads none. `retirementScopesOfReposSQL` (`repository_retirements_derived.go:34`) selects scopes by `partition_key` or the derived scope id of the repository, the scopes the repository owns. It does not select holder scopes (scopes of other kinds whose findings carry that repository id in supports). So retirement leaves a holder that already committed a Go verdict from the retired generation as it was; nothing reopens it. A pass still in flight is refused by the recheck. This is a pre-existing gap, not a holder read that a late commit can miss, and it is not widened here; flagged for the goal ledger and the arbiter. |
| Reconcile (`projector_queue_sql.go:241`, `scope_update`) | Yes: NULLs the pointer when the active generation is the one it fails. | Nothing re-derives holders: the generation failed, no successor exists. Same shape as retirement for a holder that already committed; an in-flight pass is refused by the recheck. |

## The 2 s question

The fence's exclusive wait is bounded by its own `lock_timeout` of 2000 ms
(`gomodules/fence.go`, `fenceTimeoutsQuery`). With the lock last, the pass holds the shared
lock for a recheck, one marker update and a commit, so the expected wait is milliseconds. A
pass that spends 3 s in its writes does not block the fence at all: the fence takes its lock at
once and commits, and the pass then fails its recheck and rolls back with the typed class
(`TestGoEvidencePassSpendingThreeSecondsInItsWritesLosesToAFenceLive`). A pass paused between its
recheck and its commit makes the fence wait, then reopen that pass's scope
(`TestGoEvidenceFenceWaitsForAPassHoldingTheSharedLockLive`), and a second pass arriving behind
the waiting fence queues behind it
(`TestGoEvidenceSecondPassQueuesBehindAWaitingFenceLive`). The cost moved to the pass: a pass
that loses to a fence has done all its writes for nothing. Two histograms size it:
`eshu_dp_supply_chain_impact_write_tx_duration_seconds{outcome}` (the `recheck_aborted` series is
the wasted work; `committed` p99 is how long a fence could wait if a pass stalled) and
`eshu_dp_supply_chain_impact_go_evidence_lock_wait_seconds` (the pass's wait behind a fence).
Evidence in hand before them: the lab real writer ran 0.4 to 0.7 s per 3,000 findings on an
idle database; the QA 10643j window showed a 7.64 s mean per fact insert batch on a saturated
database (`.claude/qa-accounting-10643j-2026-11-22.md`). The D3a threshold: a fence that times
out on the advisory lock for the same scope on three consecutive ticks returns to the arbiter
(`eshu_dp_go_fence_advisory_lock_timeouts_total`).

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

## The owed marker after the lock (arbiter ruling of 15:58Z)

The marker stays inside the write transaction, after the recheck and before the commit; moving it
after the commit, or into the recheck statement, is refused (a crash between commit and marker
loses the fan-out it re-drives; a statement cannot guarantee the lock is granted before its data
part runs). Both actors want one row, the pass's own work item, and they take it only under
opposite advisory modes of the same key: the fence while it holds the exclusive lock, the pass
while it holds the shared lock, and those cannot coexist.

What the marker waits on, from source: the fence's holder reopen of the same row (one statement,
`supply_chain_impact_fanout_reopen.go:93-104`; for a claimed or running item it only sets
`cross_scope_replay_required` and leaves status and lease, `:52`, so the marker still matches
under READ COMMITTED), a retirement fence's `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`
(`repository_retirements_fence_sql.go:69`), and the item's own lease heartbeat (NOT_CHECKED:
not read). An ack of a go scope does not hold this row: it updates the acked scope's projector
item and other scopes' waiting reducer rows (`projector_queue_ack.go:78,133,162`), and the
pass's own item is acknowledged by its own handler after the writer returns.
`TestGoEvidenceMarkerWaitsBehindAFenceStyleReopenLive` asserts the matched outcome and that the
marker really waited.

Residual (ruling 1.6): the recheck-to-commit window now includes the marker's possible row wait.
The fence cannot use that window (it holds no lock while it waits). A pointer change by the ack,
the ingestion upsert, the retirement fence or reconcile can use it for as long as the wait lasts;
those writers are outside this PR. If a later read finds a holder miss there, the first remedy is
a final recheck after the marker. No bound is put on the marker's wait and the pass has no lock
timeout of its own (ruling 2.3); the fence's 2,000 ms stands because it holds nothing while it
waits. The marker's duration is added to
`eshu_dp_supply_chain_impact_go_evidence_lock_wait_seconds`, so a fence timeout behind a stalled
marker can be explained (`TestGoEvidenceMarkerRowHeldThreeSecondsFenceTimesOutLive`).

## Claims table

Roles: "owner" is the disposable database's bootstrap owner (the harness login, not the
least-privilege reducer role); "worker" is the least-privilege runtime-writer login of the
protected-publish proof. Only the protected-scan probe runs as the worker; every other live test
runs as the owner (NOT_CHECKED under the least-privilege role: the convergence harness has no
role fixture). Live rows were not run here; the coordinator's slot runs them and reports the
skip count (must be 0) with both DSN variables. `ESHU_LIVE_REQUIRE_DSN=1` does not exist yet.

| Claim | Test | Seeded violation | Role | N |
| --- | --- | --- | --- | --- |
| A fence and a pass cannot both commit around one go scope (AC1) | `TestGoEvidenceFenceBetweenLoadAndWriteAbortsThePassLive` | red on main (the pass commits the stale verdict) | owner | 1 |
| A pass paused after its recheck makes the fence wait, then reopen its scope | `TestGoEvidenceFenceQueuesBehindPassPausedBeforeItsMarkerLive` | same test on main: no lock, the fence misses the pass | owner | 40 |
| A recheck abort is non-counting and writes nothing | `TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration`, `TestReducerQueueGoEvidenceChangedNeverSpendsAnAttempt` | a genuine failure in the same statement still counts (same test) | hermetic | 5 aborts |
| The abort leaves nothing after 3 s of writes | `TestGoEvidencePassSpendingThreeSecondsInItsWritesLosesToAFenceLive` | AC1 on main | owner | 1 |
| The lock is the last statement(s) before commit, then only the marker | `TestWriteLocksGoEvidenceThenRechecksLastBeforeCommit`, `TestImpactWritePassFootprintEndsWithLockRecheckMarker` | `TestGoEvidenceEpilogueViolationNamesTheSeededShapes` (11 shapes), mutation run | hermetic | 1 |
| Nothing before the lock holds a row the fence needs (recorded footprint) | `TestImpactWriteTransactionTakesNothingTheFenceNeedsBeforeTheLock`, `TestProtectedPublicationFunctionsTouchNoFenceRows`, `TestGoEvidenceRealHandlerPassFootprintLive` (real handler) | `TestCensusViolationNamesTheSeededStatements`, `TestFootprintViolationNamesTheSeededFootprints`, mutation run | hermetic; live: owner | 1 |
| No deadlock between the fence and a pass whose scope is a holder | `TestGoEvidenceFenceReopensAnInFlightPassWithoutDeadlockLive` | the marker-before-lock mutation is the deadlock shape | owner | 40 |
| The same through the protected-scan path | `TestGoEvidenceProtectedPassAndFenceNeverDeadlockLive` | same | worker | 40 |
| The marker waits behind a fence-style reopen and still matches | `TestGoEvidenceMarkerWaitsBehindAFenceStyleReopenLive` | n/a (asserts the documented behaviour) | owner | 40 |
| The marker waits behind a retirement-style table lock without deadlock | `TestGoEvidenceMarkerWaitsBehindATableLockWithoutDeadlockLive` | n/a | owner | 1 |
| A marker stalled 3 s makes the fence time out, detaching nothing | `TestGoEvidenceMarkerRowHeldThreeSecondsFenceTimesOutLive` | n/a | owner | 1 |
| The ack takes no lock and commits while a pass holds the shared lock | `TestGoEvidenceAckCommitsWhilePassHoldsTheSharedLockLive` (the ack is the production statement sequence, not `ProjectorQueue.Ack`) | n/a | owner | 40 |
| Two go scopes, two fences, one pass: no deadlock | `TestGoEvidenceMultiRepositoryPassWithParallelFencesLive` | n/a | owner | 10 |
| The recheck covers exactly the loaded pairs | `TestGoEvidenceRecheckCoversExactlyTheLoadedPairsLive`, `TestGoEvidenceLoadReturnsExactlyTheResolvedPairs` | n/a | owner / hermetic | 1 |
| 40 free interleavings leave no stale verdict (AC3) | `TestGoEvidenceFreeInterleavingLeavesNoStaleVerdictLive` | AC1 on main | owner | 40 |
| A second pass queues behind a waiting fence | `TestGoEvidenceSecondPassQueuesBehindAWaitingFenceLive` | n/a | owner | 1 |
| The fence's wait and timeout are observed; a timeout detaches nothing | `TestFenceLockTimeoutDetachesNothingAndIsObserved` | n/a | hermetic | 1 |
| Isolation is READ COMMITTED or the pass fails loudly | `TestWriteFailsLoudWhenTheWriteTransactionIsNotReadCommitted` | the non-read-committed row is the seeded violation | hermetic | 1 |
| The write-tx and lock-wait histograms record by outcome | `TestGoEvidenceLockSignalsRecordWithClosedLabels` | nil-receiver and unregistered cases | hermetic | 1 |
| The recheck's SELECT is granted to the writer login | `grantImpactWriterPregrants` now grants `SELECT ON ingestion_scopes`; `TestSupplyChainImpactProtectedStaticPublishLive` runs as the worker | the worker lacks it without the grant | worker | 1 |

The footprint test `TestGoEvidenceRealHandlerPassFootprintLive` runs the real handler through a
changed first pass of a holder with Go evidence. The hermetic footprint tests run the real
writer and the real storage transaction, not the handler.

## Proofs

| Claim | Test |
| --- | --- |
| The key is its own derivation | `TestGoEvidenceLockKeyDiffersFromTheConflictDomainKey` |
| Lock, then recheck, then only the owed marker, last before the commit; no pair means neither | `TestWriteLocksGoEvidenceThenRechecksLastBeforeCommit`, `TestWriteWithNoResolvedGoEvidenceIssuesNeitherStatement` |
| A rewrite cannot move them off the end unnoticed (seeded violations) | `TestGoEvidenceEpilogueViolationNamesTheSeededShapes` |
| Census guard: nothing before the lock names fact_work_items or writes or locks a scope row | `TestImpactWriteTransactionTakesNothingTheFenceNeedsBeforeTheLock`, `TestCensusViolationNamesTheSeededStatements`, `TestProtectedPublicationFunctionsTouchNoFenceRows` |
| The write-tx and lock-wait histograms record by outcome | `TestGoEvidenceLockSignalsRecordWithClosedLabels` |
| A changed generation aborts with the typed class and writes nothing | `TestWriteAbortsWithTheTypedClassWhenTheRecheckFindsAChangedGeneration` |
| N aborts spend no attempt; a genuine failure still counts | `TestReducerQueueGoEvidenceChangedNeverSpendsAnAttempt`, `TestReducerQueueClaimDoesNotCountGoEvidenceChanged` |
| 55P03 maps only on the shared-lock statement | `TestClassifyGoEvidenceLockTimeoutMapsOnlyTheSharedLockStatement` |
| The recheck covers exactly the loaded pairs | `TestGoEvidenceLoadReturnsExactlyTheResolvedPairs`, `TestGoEvidenceRecheckCoversExactlyTheLoadedPairsLive` |
| Fence order and timeout detach nothing | `TestFenceTakesTheExclusiveGoEvidenceLockBeforeAnythingElse`, `TestFenceLockTimeoutDetachesNothingAndIsObserved` |
| Isolation guard | `TestWriteFailsLoudWhenTheWriteTransactionIsNotReadCommitted` |
| AC1: the miss is red on main, aborted now | `TestGoEvidenceFenceBetweenLoadAndWriteAbortsThePassLive` |
| Pass paused between recheck and commit: the fence waits, then reopens | `TestGoEvidenceFenceWaitsForAPassHoldingTheSharedLockLive` |
| A pass spending 3 s in its writes loses to a fence; nothing written, typed class | `TestGoEvidencePassSpendingThreeSecondsInItsWritesLosesToAFenceLive` |
| A second pass queues behind a waiting fence | `TestGoEvidenceSecondPassQueuesBehindAWaitingFenceLive` |
| 40 rounds, in-flight holder reopened, zero deadlocks | `TestGoEvidenceFenceReopensAnInFlightPassWithoutDeadlockLive` |
| 40 rounds through the protected-scan path as the least-privilege login | `TestGoEvidenceProtectedPassAndFenceNeverDeadlockLive` |
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
resolves no go scope issues no new statement. A pass that resolves N distinct go scopes issues N
advisory lock statements and one recheck SELECT at the end of its write transaction (the
hermetic order test shows the exact statements); the fence issues one advisory lock statement.
Advisory locks write no WAL and touch no heap (shim: 339 and 338 WAL bytes per transaction against
345 and 338 without the lock; the row-lock alternative cost about 190 bytes). The lock is
transaction-scoped and shared passes do not wait on each other. Conflict domain: the go scope id.
Lock order: the pass takes the conflict-domain lock and its rows first, then the shared go-evidence
locks at the end, then updates its own work item; the fence takes the exclusive lock first, then
the scope row, the generation and the holders. The census guard pins that nothing the pass holds
before the lock is a row the fence needs. The cost of the lock-last position is the pass's rolled
back writes when a fence lands during its transaction; the `recheck_aborted` series of the write-tx
histogram measures it. The real-writer measurement of write-tx duration and WAL per pass with and
without the change (ruling 4.9) is NOT RUN here: it needs a coordinator slot on the real writer
path, and the toy shim is not that proof.

Observability Evidence: `eshu_dp_supply_chain_impact_go_evidence_recheck_aborts_total{reason}`
counts passes aborted by the recheck or the shared lock timeout,
`eshu_dp_go_fence_advisory_lock_timeouts_total` counts fence attempts that hit the lock timeout,
`eshu_dp_go_fence_advisory_lock_wait_seconds` records the fence's wait,
`eshu_dp_supply_chain_impact_write_tx_duration_seconds{outcome}` records each impact write
transaction by outcome with buckets to 300 s, and
`eshu_dp_supply_chain_impact_go_evidence_lock_wait_seconds` records the pass's wait for the shared
lock. Documented in `docs/public/reference/telemetry/supply-chain-impact-supports.md` and the
reducer coverage row. At 3 AM, a rising abort rate or `recheck_aborted` share means fences and
passes keep colliding on one repository; the retrying intents appear under failure class
`supply_chain_impact_go_evidence_changed` and the service reports them as deferred.

## Least-privilege logins

The recheck is a plain SELECT on `ingestion_scopes` and `scope_generations` in the write
transaction. The reducer login already reads both for every claim; a login that lacks either
fails the recheck with SQLSTATE 42501 and the pass rolls back. The protected-publish proof now
grants `SELECT ON ingestion_scopes` in `grantImpactWriterPregrants`, and the least-privilege
section of `supply-chain-impact-supports.md` names the requirement. The advisory lock functions
need no grant.
