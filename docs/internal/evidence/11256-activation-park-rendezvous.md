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
  scope between its insert and its satisfaction check, bounded by one 3 s
  context deadline across all keys (`narrowedParkRendezvousTimeout`). A
  non-blocking try over the keys comes first, in chunks of at most 128 keys per
  statement (`rendezvousTryChunkSize`), so the common park costs one statement
  per 128 blocking scopes and one statement never holds more locks than that.
  The keys that were held are then waited on one statement each, so the park
  holds no lock while it waits.
- Why the two cases are exhaustive: let `Wake` be the activation's wake snapshot
  and `Ins` the parker's insert commit.
  - `Ins` before `Wake`: the wake sees the row and reopens it.
  - `Ins` after `Wake`: the activation already holds the exclusive lock, taken
    before any of its statements. The parker's lock statement waits for the
    commit and the satisfaction check that follows sees the committed activation,
    so the existing in-park reopen fires.
  - The parker passes the lock before the activation starts: the activation's
    snapshot is later than the insert, so the first case applies.
- The wait row is already committed when the rendezvous runs, and a missed wake
  is a hint the audit and the backstop bound. So a rendezvous that cannot
  finish never fails the park: the bound firing records `timeout`, any other
  statement error records `error`, both log a warning, and the satisfaction
  check still runs. Returning the error would not retry anything (the Fail
  path's retry UPDATE has already committed, and an error from the park makes
  the service cancel every worker). Only the caller's own cancellation is
  returned.

### Lock order (concurrency-deadlock-rigor)

| Actor | Order | Holds while waiting |
| --- | --- | --- |
| Activation transaction | advisory X(scope), then `relationship_generations` rows, then `SET LOCAL lock_timeout`, then in-flight own-gate consumer rows by `work_item_id COLLATE "C"`, then wake updates (waiting rows `FOR UPDATE`, queue rows) | its own locks only |
| Parker (autocommit) | insert wait row (commits), try advisory S over chunks of at most 128 keys (never waits), then advisory S on one busy key per statement (waits), satisfaction read | nothing while it waits: the try statements release at their end, each blocking statement holds one key |
| Two activations, same scope | serialize on advisory X before any row lock | no cycle: the second holds nothing |
| Two activations, different scopes | different keys | same row-lock order as before |
| Two parkers | shared with shared | compatible |

A parker that waits holds no lock, so it cannot be one edge of a cycle, and an
activation of one scope never waits behind a parker that waits on another
scope. The one new wait an activation can have is on a parker's shared lock on
its own scope, which a parker holds only inside its own statement and releases
when that statement returns or hits the 3 s bound. No other code in the
repository takes this key. The first version waited on all busy keys in one
statement, which kept the shared locks it had already taken while it waited on
a later key; a reviewer measured an unrelated scope's activation stalled for
3.93 s that way. `TestParkWaitingOnOneScopeDoesNotStallAnotherScopesActivationLive`
reads `pg_locks` while a park waits and requires it to hold none, and it failed
on that version (`the waiting park holds 1 advisory locks, want 0`).

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

After the review fixes (per-key waits, bounded try chunks, errors handled like
the timeout, the void rule), at `0d8ee73d3b` on postgres 18.6, every live run
with zero skips:

| Check | Result |
| --- | --- |
| Interleaving (2 kinds), convoy, 400-scope and contended tests, `-count=20` | 120 PASS, 0 FAIL, 0 SKIP |
| `TestNarrowedWaitConcurrentConvergenceLive` `-count=200`, plain and `-race` | 200 PASS and 200 PASS, 0 FAIL, no DATA RACE |
| Live activation, refinalize, claim-fence, replay and contention-gate tests that take the activation path, all opt-ins set | 218 PASS, 0 FAIL, 17 SKIP (supply-chain attachment and fan-out proofs that need other fixtures, unrelated to the activation lock) |
| `-race` contention package, `GOMAXPROCS=2` advisory round tests `-count=30` three times | ok; 60 of 60 PASS each |
| Hermetic storage/postgres, reducer, workflow, telemetry | only the known environment failures above |
| golangci-lint and `go vet` on the touched packages | 0 issues, clean |

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

Performance Evidence: Added cost, measured. The measured subject is the
rendezvous: one `pg_advisory_xact_lock` per activation and one try statement per
128 blocking scopes per generation-activation park. Conflict domain: one provider
scope's activation. No worker, lease or batch setting changes.

Uncontended, `pgbench -c 1 -T 8` on postgres 18.6 (server side only, host load
about 25, so read the differences, not the absolute values):

| Statement | Latency |
| --- | --- |
| `BEGIN; SELECT 1; COMMIT` | 0.023 ms |
| `BEGIN; SELECT pg_advisory_xact_lock(...); COMMIT` | 0.031 ms (+0.008 ms per activation) |
| `SELECT 1` | 0.009 ms |
| try-lock shared, one scope | 0.022 ms (+0.013 ms per park) |
| try-lock shared, three scopes | 0.025 ms |

Contended, `TestRendezvousContendedLatencyLive` (postgres 18.6, host load 15 to
30, 10 s per run): A activators each loop the production exclusive lock with 5 ms
of held work, P parkers loop the production `awaitInFlightActivations` over a
random blocking set. Before is the first version of the rendezvous (`5dead1db4d`,
all busy keys in one statement), after is this branch. Same test file, same
database, run back to back.

| Run | Version | Activations | Parks | Activation lock wait p50 / p99 / max | Park p50 / p99 / max |
| --- | --- | --- | --- | --- | --- |
| 16 activators, 16 parkers, 32 scopes, 20 blocking | before | 11857 | 2081 | 0.13 / 106.9 / 193.3 ms | 76.6 / 181.7 / 216.2 ms |
| 16 activators, 16 parkers, 32 scopes, 20 blocking | after | 21228 | 7312 | 0.15 / 12.1 / 28.1 ms | 21.0 / 46.7 / 85.9 ms |
| 32 activators, 32 parkers, 8 scopes, 6 blocking | before | 6919 | 1401 | 5.3 / 356.0 / 423.0 ms | 280.4 / 415.5 / 429.9 ms |
| 32 activators, 32 parkers, 8 scopes, 6 blocking | after | 12126 | 2632 | 13.5 / 98.1 / 136.4 ms | 124.8 / 198.5 / 238.0 ms |

The second row pair is dominated by same-scope queueing (32 activators on 8
scopes, 5 ms each), which no version avoids; the first version added a
cross-scope convoy on top of it. These runs are measurements, not a throughput
claim: the activation stand-in is the lock plus 5 ms, not the production
activation. The test asserts only that no activation waits more than half the
park bound (1.5 s).

A park whose blocking set spans several chunks takes bounded locks:
`TestParkWithManyBlockingScopesTakesBoundedLocksLive` (400 blocking scopes, the
in-flight activation in a middle chunk) failed on the first version with the
park holding 200 advisory locks while it waited, and passes with none. The
first version's single statement took one lock per blocking scope, and stock
postgres 18.6 failed with `out of shared memory` at about 20000 keys in one
session (reviewer shim). Today the key itself bounds the blocking set: it is
part of the primary key of `reducer_waiting_items`, so a set of a few hundred
scope IDs already fails the insert with `index row size ... exceeds btree
version 4 maximum 2704` (measured with 37-character IDs: 100 fail; with
sequential IDs 500 fit and 1000 fail). That insert failure is older than this
change and is not fixed here; it is the next bound on a bootstrap-sized blocking
set, and the rendezvous no longer adds one of its own.

The cost of a park is one try statement per 128 blocking scopes, and only for the
two generation-activation kinds. The contended path adds a wait only while an
activation of the same scope is open, bounded by 3 s in total. Before and after
on the interleaving: 1 wait row survives on `main`, 0 with the fix.

Concurrency proof: the lock order table above, no cycle (an activation holds one
advisory lock and takes it before any row lock; a parker holds nothing while it
waits), the deterministic interleaving test for both kinds (RED on `main` 3 of
3, GREEN 20 of 20), the `pg_locks` test that a waiting park holds no lock, 200
plain and 200 `-race` runs of the concurrent convergence test, and the live
activation, claim-fence and refinalize tests above.

Observability Evidence: `eshu_dp_reducer_waiting_wake_total{wake_site="park_rendezvous"}`
with outcome `waited` (a park stood behind an in-flight activation), `timeout`
(it gave up at 3 s) or `error` (a rendezvous statement failed), plus the warning
logs `reducer_waiting.narrowed_park_rendezvous_timeout` and
`reducer_waiting.narrowed_park_rendezvous_error`, each with the provider scope
count, the first five scope IDs and the error. Hermetic tests cover the free
park (no metric), `waited`, `timeout`, `error` and the shutdown path. A rising
`timeout` or `error` count with a rising
`eshu_dp_reducer_waiting_missed_wake_total` points at a stalled database, not at
this rendezvous. There is no signal yet for an activation waiting on the
advisory lock; the contended test above is the measurement for that.

Template-clone flake, not in scope: the first test of a fresh process can fail
on `clone database ... still exists after cleanup`, because the shared
template-clone helper cancels `DROP DATABASE ... WITH (FORCE)` at its 15 s
client timeout while the server is in a checkpoint. It is the helper's timeout
(`go/internal/testutil/postgresproof/template_clone.go`), already failing
unrelated tests on `origin/main`, and is tracked separately.

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
`CallTimeout`, 30 s; unit tests use 300 ms), so claims skip them and the
targets stay open while the goroutines run. The hold is a window, not a
guarantee: a goroutine delayed past it (a stalled runner) can still reach the
guard after workers drained the items, and a first version of this note claimed
otherwise. So the round records when each guard call returned and judges its
verdict only if every call returned before the hold ended; a round that did not
is voided (counted as `voided_rounds`, logged, never recorded as a duplicate).
A guard that returned before `visibleAt` evaluated its targets while they were
still hidden from claims, so the verdict it gave is the guard's own.
`TestAdvisoryRoundVoidsWhenTheHoldEndsBeforeEveryGuardReturned` stretches every
guard call past the hold and requires no duplicate to be recorded; it failed on
the first version (`round 0: 25 admitting goroutines, 100 items inserted`). The
fake honors `VisibleAt` on admission as the real insert and claim do. The
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
