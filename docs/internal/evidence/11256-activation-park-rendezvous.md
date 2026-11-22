# #11256 and #11074: the activation park window and the advisory round

Refs #11256, #11074, #10877, #10860.

Two intermittent CI failures, one root-cause question each: a test race or a
real race in the product? #11256 is a real window in the product. #11074 is a
race in the contention harness, and the product guard is right.

## #11256: a narrowed wait can outlive its blocker's activation

**Root-Cause Evidence:** `TestNarrowedWaitConcurrentConvergenceLive` failed 132
of 200 runs on `origin/main` (`c82e067cc2`, postgres 18.6, load about 30) with
the CI line `waits for wait-race-N after all activations = 1, want 0`, for N in
0 to 3. A deterministic test, built from production functions only, reproduces
the end state: the holder generation is `active`, one `reducer_waiting_items`
row survives, and its queue row is `retrying` on attempt 1 with `visible_at` ten
minutes out.

### The interleaving

Parking is two autocommit statements: insert the wait row, then read whether the
wait is already satisfied. Activation is one transaction: activate the
generation, run the wake statements, commit. Under READ COMMITTED each statement
reads one snapshot.

| Step | Activation transaction | Parker (Fail path) |
| --- | --- | --- |
| 1 | activates the holder generation (uncommitted) | |
| 2 | narrowed wake statement takes its snapshot: no wait row exists, reopens nothing | |
| 3 | | inserts the wait row, commits |
| 4 | | satisfaction check: the activation is not committed, so not satisfied; the row stays |
| 5 | commits | |

Both sides read a snapshot that predates the other's commit, so neither reopens
the wait. The design says a missed wake is bounded by the audit (5 minutes,
`DefaultWaitingAuditInterval`) and the backstop (10 minutes,
`WaitingBackstopDelay`); that bound is why the product kept working, and why one
CI run in many failed. A corpus-fenced reducer item that sleeps 5 to 10 minutes
is operator-visible latency, so the window is closed rather than documented.

### The fix

- The activation takes `pg_advisory_xact_lock(hashtextextended('relationship_generation_activation:' || scope, 0))`
  first in `activateWithConsumerWake`, before any row lock. Every statement that
  activates a relationship generation goes through that function
  (`TestEveryRelationshipActivationWakesConsumers`).
- A park of a generation-activation wait takes the shared lock on each provider
  scope between its insert and its satisfaction check, bounded by a 3 s context
  deadline (`narrowedParkRendezvousTimeout`). A non-blocking try comes first, so
  the common park costs one statement.
- Why the two cases are exhaustive: let `Wake` be the activation's wake snapshot
  and `Ins` the parker's insert commit.
  - `Ins` before `Wake`: the wake sees the row and reopens it.
  - `Ins` after `Wake`: the activation already holds the exclusive lock, taken
    before any of its statements. The parker's lock statement waits for the
    commit and the satisfaction check that follows sees the committed activation,
    so the existing in-park reopen fires.
  - The parker passes the lock before the activation starts: the activation's
    snapshot is later than the insert, so the first case applies.
- A timeout or cancellation of the wait parks as before (the audit and the
  backstop bound it) and records `timeout`. Any other error is returned so the
  Fail path retries.

### Lock order (concurrency-deadlock-rigor)

| Actor | Order | Holds while waiting |
| --- | --- | --- |
| Activation transaction | advisory X(scope), then `relationship_generations` rows, then `SET LOCAL lock_timeout`, then in-flight own-gate consumer rows by `work_item_id COLLATE "C"`, then wake updates (waiting rows `FOR UPDATE`, queue rows) | its own locks only |
| Parker (autocommit) | insert wait row (commits), try advisory S, advisory S (waits), satisfaction read | nothing: each statement is its own transaction |
| Two activations, same scope | serialize on advisory X before any row lock | no cycle: the second holds nothing |
| Two activations, different scopes | different keys | same row-lock order as before |
| Two parkers | shared with shared | compatible |

A parker that waits holds no lock, so it cannot be one edge of a cycle. The one
new wait an activation can have is on a parker's shared lock, which a parker
holds only inside its own statement and releases when that statement returns or
hits its 3 s bound. No other code in the repository takes this key.

### Kinds covered and not covered

| Kind | Provider | Covered |
| --- | --- | --- |
| narrowed corpus wait (`relationship_generation_active` with a blocking set) | activation of a blocking scope | yes |
| coarse `relationship_generation_active` | activation of the waiter's own generation | yes |
| `phase_committed` | producer's phase publish | no: #11104 already re-wakes after the publish commits (`phase_exact_post_commit`) |
| `producer_terminal`, `target_committed`, `generation_active`, `winners_watermark` | queue acknowledgement, producer saves, winners rebuild | no: different transactions and lock orders, none takes the activation lock; the wake there stays a hint bounded by the audit |

### Proof

| Check | Result |
| --- | --- |
| Original test, `origin/main`, `-count=200` | 132 failed of 200 |
| New `TestWaitParkDuringInFlightActivationIsWokenLive`, single-kind version, unfixed code | RED: `wait rows after the activation committed = 1, want 0` |
| Same test, single-kind version, fixed code | GREEN (18.9 s) |
| Two-kind version (narrowed and coarse), unfixed code (`b50e5600b5`), `-count=3` | RED 3 of 3, both kinds: `wait rows after the activation committed = 1, want 0` |
| Two-kind version, fixed code, `-count=20`, full output | 60 `--- PASS` lines (20 parents, 40 subtests), 0 FAIL, 0 SKIP, 48.9 s |
| Original test, fixed code, `-count=200` | 200 PASS, 0 FAIL, 0 SKIP (56.9 s) |
| Original test, fixed code, `-race -count=200` | 200 PASS, 0 FAIL, no DATA RACE (159.6 s) |
| Hermetic `./internal/storage/postgres` | only the 3 env-gated failures (`QuietAbsoluteReprove`, `StagingAutovacuumSoak`: host load; `SchemaBootstrapCoordination`: `ESHU_DEPLOY_CHECKOUT`) |
| Hermetic `./internal/reducer/... ./internal/workflow/... ./internal/telemetry/...` | ok, except the census alert-rule tests that need `ESHU_DEPLOY_CHECKOUT` |
| Live activation, refinalize, claim-fence and contention-gate tests that take the activation path, against a postgres 18.6 container | 204 PASS; the 4 failures are not this change (see below) |

The first hermetic run found ten `TestRelationshipStore*` tests failing: their
fake database did not know the new rendezvous statement. The fakes now accept
it, and the claim-fenced test asserts the lock is the first exec.

The live failures: `TestContainerImageIdentityActivationEpochMissIsSentinelLive`
needs a DSN whose database is `postgres` (passes with one);
`TestReducerContentionGateServiceCatalogRuntimeFenceConcurrentWorkers` timed out
on a claim at host load 30 and passed at once on rerun;
`TestReducerContentionGateBootstrapTemplateCloneMatchesBootstrap` fails the same
way on `origin/main` (`clone database ... still exists after cleanup: the clone
drop failed`); `TestGenerationRetentionCoveredNeverActivatedPrunesPastCeilingLive`
failed only on that same clone-drop cleanup, on the first run in a fresh
database (twice), and passed on the three runs after it and on `origin/main`.
None asserts on the activation lock.

The earlier unexplained narrowed failure (a 20 s FAIL of the narrowed subtest
while the coarse one passed) reproduced once, after the merge (1 of 140 runs of
the two-kind test, narrowed subtest, 19.2 s, coarse subtest passed). It is not
the fix: the only message is `clone database "..." still exists after cleanup:
the clone drop failed` from the shared template-clone helper, with no product
assertion failing. The disposable server's log has the cause:
`ERROR: canceling statement due to user request` on
`DROP DATABASE IF EXISTS "..." WITH (FORCE)`, issued while a checkpoint was in
its sync phase (`sync=24.322 s`, `total=27.730 s`). `DROP DATABASE` waits for a
checkpoint, the host disk stalled, and the helper's client timeout cancelled the
drop. The same message failed other tests on `origin/main` that do not touch the
activation path (`TestReducerContentionGateBootstrapTemplateCloneMatchesBootstrap`).
The fixed code passed 20 of 20 two-kind runs, 200 of 200 plain and 200 of 200
`-race` runs of the original test, and 139 of 140 runs after the merge.

Performance Evidence: Improvement. The measured subject is one extra autocommit
statement per generation-activation park and one `pg_advisory_xact_lock` per
activation. Conflict domain: one provider scope's activation. No worker, lease or
batch setting changes. `pgbench -c 1 -T 8` on postgres 18.6 (server side only,
host load about 25, so read the differences, not the absolute values):

| Statement | Latency |
| --- | --- |
| `BEGIN; SELECT 1; COMMIT` | 0.023 ms |
| `BEGIN; SELECT pg_advisory_xact_lock(...); COMMIT` | 0.031 ms (+0.008 ms per activation) |
| `SELECT 1` | 0.009 ms |
| try-lock shared, one scope | 0.022 ms (+0.013 ms per park) |
| try-lock shared, three scopes | 0.025 ms |

The cost of a park is that statement's round trip, once, and only for the two
generation-activation kinds. The contended path adds a wait only while an
activation of the same scope is open, bounded by 3 s. Before and after on the
interleaving: 1 wait row survives on `main`, 0 with the fix.

Concurrency proof: the lock order table above, no cycle (an activation holds one
advisory lock and takes it before any row lock; a parker holds nothing between
statements), the deterministic interleaving test for both kinds (RED on `main`
3 of 3, GREEN 20 of 20), 200 plain and 200 `-race` runs of the concurrent
convergence test, and the live activation, claim-fence and refinalize tests
above.

Observability Evidence: `eshu_dp_reducer_waiting_wake_total{wake_site="park_rendezvous"}`
with outcome `waited` (a park stood behind an in-flight activation) or `timeout`
(it gave up at 3 s), plus the warning log
`reducer_waiting.narrowed_park_rendezvous_timeout` with the provider scope IDs.
A rising `timeout` count with a rising `eshu_dp_reducer_waiting_missed_wake_total`
points at a stalled database, not at this rendezvous.

## #11074: the advisory round's own workers terminalize its targets

**Root-Cause Evidence:** the guard (`CreateRunWithWorkItemsIfNoOpenTargets`)
admits a run only when its targets have no non-terminal run or item. The round
(`contention/loops.go` `advisoryRound`) races 25 goroutines for the same four
targets and asserts one winner, but the harness's workers claim any visible
pending item of the same collector kind and instance. Under a loaded `-race`
runner a goroutine is delayed after the start barrier long enough for workers to
complete the first winner's items, and the guard then correctly admits a second
run (CI: round 4, 2 admitting goroutines, 8 items, want 1 and 4).

Deterministic reproduction, hermetic: a fake-leg fault `drainAdmitted` completes
every visible admitted item at once, as a worker would.
`TestAdvisoryRoundKeepsItsTargetsOpenAgainstDrainingWorkers` on the unfixed
round: `round 0: 25 admitting goroutines, 100 items inserted (want 1 and 4)`.

Fix: the round admits its items with `VisibleAt = now + AdvisoryHold` (default
`CallTimeout`, 30 s; unit tests use 300 ms), so claims skip them and the targets
stay open until every goroutine passed the guard. A guard call is cancelled
after `CallTimeout`, so no goroutine can still be waiting when the hold ends.
The fake honors `VisibleAt` on admission as the real insert and claim do. The
criterion, the goroutine count and the item count are unchanged.

| Check | Result |
| --- | --- |
| New test, unfixed round | RED (above) |
| `./internal/workflow/parity/contention`, fixed round | ok, 44.97 s |
| Hermetic `./internal/workflow/...`, `-race` contention package | ok (46.2 s), lint 0 issues, vet clean |
| Constrained load (`GOMAXPROCS=2`, `-race` binary) | new test 30 of 30 PASS; whole contention package `-count=3` ok |

No-Regression Evidence: #11256 adds 8 microseconds per activation and one
statement per generation-activation park (table above); #11074 touches only the
test harness. No worker count, batch size or concurrency level was reduced.
