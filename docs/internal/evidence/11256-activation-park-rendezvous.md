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
| CI readiness shard 6 of 8 (the one holding the new tests), run through `scripts/run-live-postgres-readiness-tests.sh --shard 6/8` with every readiness DSN and opt-in variable set against a disposable postgres 18 | 153 of 153 PASS after the runner's own excuses (the Neo4j graph proof, and `TestRetirementDerivedStatementPlansLive`, which the workflow excuses with `LIVE_READINESS_EXTRA_SKIP`); the five new and touched tests are in it |
| Reducer contention gate, both storage/postgres halves, with the workflow's `-run` filters and DSNs (without `-race`, which CI adds) | half one 191 PASS, half two 247 PASS, 0 FAIL, 0 SKIP |
| Contention runner contract (`reducer_contention_events.py`, its two self-tests, `verify-live-tests-ledger.sh`) and `TestReducerContentionPostgresProofsRunInTheReducerContentionGate` | pass (126 contention files verified) |
| `go test ./internal/replay/...`, `go vet`, `check-changed-coverage-floors.sh` | ok (18 packages); vet clean; telemetry 77% >= 77, contention 81% >= 81, storage/postgres env_gated |
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
session (reviewer shim). Before the key bound below, the key itself capped the
blocking set (a few hundred scope IDs failed the insert); the bound now limits
a new key to 16 scopes, and the rendezvous keeps its chunked try for rows
parked before the bound.

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

## The narrowed wait key must fit two btree indexes (#11256)

A pre-existing defect on `main`, found while measuring the rendezvous and
fixed here by bounding the key. The arbiter's ruling is
`.claude/arbiter-waits-for-key.md`.

**Root-Cause Evidence:** `narrowedWaitingKey` rendered the whole blocking set
and the whole read set into `waits_for_key`, which sits in two btree indexes
(`reducer_waiting_items_pkey` and `reducer_waiting_items_wake_idx`, migration
316). A btree entry may not exceed 2,704 bytes after compression. Through the
real `ReducerQueue.Fail` on the migrated schema, 600 real-shape blocking scope
IDs (`git-repository-scope:repository:r_<8 hex>`, random hex) returned
`park waiting reducer work: ERROR: index row size 6288 exceeds btree version 4
maximum 2704 for index "reducer_waiting_items_pkey" (SQLSTATE 54000)`, and one
blocker plus 600 read-set repositories returned 6312. The failure comes after
Fail's retry UPDATE committed, and `Service.Run` returns the error, so the
whole reducer exits and does so again at each backstop while the verdict
stays wide (`TestServiceSurvivesAWideVerdictDeferralLive` on the unfixed
code: `Service.Run` returned the error before the normal item was
acknowledged). The arbiter measured 220 git scope IDs inserting and 230
failing, and one blocker plus 245 repository IDs inserting and 250 failing.

Reachability, measured with the built reducer (item below): a fresh bootstrap
of 600 git repositories on the unfixed code made the reducer exit 1 on the
first wide deferral (a verdict holding 598 scopes) with exactly that error.

### The fix

- Key: `{scope_id, generation_id, blocking_scope_ids}` where
  `blocking_scope_ids` is the longest sorted prefix of the verdict set that
  fits 16 IDs and 1,024 bytes of rendered key. An ID that cannot fit alone is
  skipped. `repo_ids` is no longer written (nothing read it; old rows still
  decode). The key is deterministic for a set in any order. No migration, no
  primary-key change, no new index.
- Exact because the wake is a hint and the retried handler re-checks the whole
  fence; once 16 or fewer blockers remain all are named, so the last
  activation always wakes the item. The audit predicate needs the named
  scopes complete, so a satisfied prefix reopens and the next park names the
  next prefix.
- Size guard in `insertWaitingItem` for every kind: `work_item_id`, `scope_id`
  and `generation_id` have no length bound. A row whose `work_item_id`, kind
  and key exceed 2,048 uncompressed bytes is not inserted: no wait row, the
  queue row keeps the timer path the retry UPDATE set, the counter
  `eshu_dp_reducer_waiting_key_oversize_total{kind}` increments and
  `reducer_waiting.key_oversize` logs at ERROR; `Fail` returns nil. A
  narrowed deferral with no ID that fits takes the same path. It never falls
  back to the coarse wait: that wait is satisfied by the waiter's own active
  generation, so it would reopen the item at once and spin.
- `eshu_dp_reducer_waiting_key_truncated_total{domain}` counts a park that
  named fewer scopes than the verdict held.
- The budget 2,048 comes from measurement: with incompressible text both
  indexes took about 2.6 KB of composite `work_item_id + kind + key` text for
  any split between `work_item_id` and key (work_item_id of 64, 256, 512 and
  1,024 bytes accepted keys of 2,554, 2,358, 2,102 and 1,590 bytes). The
  worst row the guard admits (a key at the 1,024 byte cap and the longest
  `work_item_id`) inserts on both indexes in
  `TestWorstCaseAdmittedWaitRowInsertsOnBothIndexesLive`.

### Proof

| # | Proof | Result |
| --- | --- | --- |
| 1 | RED through `ReducerQueue.Fail`, real PG, real migrations, real-shape IDs | unfixed: SQLSTATE 54000 for 600 blockers and for 1 blocker + 600 repos; fixed: Fail nil, one wait row, `retrying` in the non-counting class, attempt count unchanged |
| 2 | `Service.Run`, one wide and one normal item | unfixed: Run returned the error before the normal item was acknowledged; fixed: normal item acknowledged, Run nil on shutdown |
| 3 | Hermetic key builder, 1/5/16/17/200/5,000 IDs, real-shape and incompressible 200-byte | key at most 1,024 B, at most 16 named, sorted prefix, order independent, no `repo_ids`, oversize-alone ID skipped |
| 4 | Worst admitted row on both indexes | inserts (key at the cap, longest work_item_id, incompressible) |
| 5 | Guard RED/GREEN | oversize composite: no insert, counter 1, Fail nil; with the guard neutered `inserted true ... want false` |
| 6 | Contention, `-race`, `GOMAXPROCS=2`, `-count=20`: 12 waiters with 20 to 40 blockers through `Service.Run`, activations in sorted, reverse and random order | 80 PASS (20 parents, 60 subtests), 0 FAIL, 0 DATA RACE, 0 SKIP, no 40P01; every item succeeded on the wake path in seconds with `attempt_count` 1, no wait row left, audit reopened 0. A first run of this proof failed 8 times on a data race in the test's own shared random source (fixed in the test); a harness that seeded the waiter without its own active relationship generation never satisfied the park check and was also fixed |
| 7 | No hot loop | activating 3 un-named blockers left the wait row and `visible_at` untouched; activating a named blocker reopened it |
| 8 | Old-shape row (wide set plus `repo_ids`) | still wakes by a named scope and reopens by audit |
| 9 | Performance | tables below |
| 10 | Built binaries, 600 git repositories | below |

**Per-park cost** (`setAsideWaitingForCause` through the production path on
the migrated schema, 150 parks per row, postgres 18.6, load about 20; WAL from
`pg_current_wal_lsn()`), real-shape random IDs:

| Blockers | Unfixed p50 / p99 | Unfixed WAL per park | Fixed p50 / p99 | Fixed WAL per park |
| --- | --- | --- | --- | --- |
| 5 | 3.58 / 10.35 ms | 3,255 B | 1.53 / 2.89 ms | 3,368 B |
| 16 | 5.22 / 16.2 ms | 4,716 B | 3.3 / 11.3 ms | 3,893 B |
| 200 | fails 150 of 150 (SQLSTATE 54000) | 0 (nothing written) | 3.04 / 16.5 ms | 3,883 B |
| 600 | fails 150 of 150 (12,288 B row over the 8,191 B limit) | 12,599 B of failed writes | 3.0 / 13.1 ms | 3,831 B |
| 5,000 | fails 150 of 150 (93,256 B) | 99,956 B of failed writes | 2.59 / 3.68 ms | 3,803 B |

The fixed cost is flat in the verdict size. Timing at 5 and 16 blockers is
within the load noise of the host; the WAL per park is the figure to read.

**Wake statement at scale** (`wakeNarrowedCorpusWaitsForScopeQuery`,
unchanged by this change; `EXPLAIN (ANALYZE, BUFFERS)` inside a rolled-back
transaction on the migrated schema, parked rows with 16-scope keys drawn from
a 600-scope pool):

| Parked rows | Activated scope | Rows reopened | Execution | Plan |
| --- | --- | --- | --- | --- |
| 8,713 | named by no wait | 0 | 4.0 ms, 1,247 buffers | seq scan of `reducer_waiting_items` |
| 8,713 | inside many named prefixes | 238 | 12.0 ms, 8,294 buffers | seq scan, then 238 pkey probes |
| 87,130 | named by no wait | 0 | 48.2 ms, 12,450 buffers (5,397 read cold) | seq scan |
| 87,130 | inside many named prefixes | 2,386 | 149.5 ms, 85,061 buffers | seq scan, then 2,386 pkey probes |

The scan is linear in parked rows, as the arbiter's shim found at 8,713 rows.
At the fleet's present scale (tens of parked rows in QA) it is not a cost; at
87,130 parked rows it is about 50 ms per activation inside the activation
transaction, which the arbiter ruled does not justify a GIN expression index
(a migration on a table QA applied) until a measured scan dominates.

**Built binary, 600 git repositories** (`bootstrap-data-plane`,
`bootstrap-index --path` over 600 generated repositories each with a
Dockerfile, a Kubernetes Deployment and a Go module, then the `reducer`
binary, postgres 18.6, pinned neo4j community image, 11,401 reducer work items
per arm). Before is the F1 to F6 head `3c418af75f`, after is this branch:

| Arm | Reducer outcome |
| --- | --- |
| Before | exit 1 within seconds of start: `fail reducer work: park waiting reducer work: ... ERROR: index row size 6496 exceeds btree version 4 maximum 2704 for index "reducer_waiting_items_pkey" (SQLSTATE 54000)`, on a deferral whose verdict held 598 scopes; 1 wait row parked, the rest of the queue undrained |
| After | no exit; 1,788 wide deferrals (verdicts of up to 598 scopes) parked and woke; the queue drained in 330 s with 11,401 of 11,401 items `succeeded`, 0 wait rows left, no 54000 and no `reducer_waiting.key_oversize` in the log |

Performance Evidence: Improvement for any verdict wider than the btree limit (63
random 36-character IDs in the first measurement, 220 git scope IDs in the
arbiter's; it could not park before and now parks), and no change below it. The
measured subject is the narrowed park: one statement for the insert, the same
rendezvous (at most 16 keys now), and the wake statement unchanged. Conflict
domain: one waiting row and one scope's activation; no worker, lease or batch
setting changes. The cost of a park is flat in the verdict size (about 3.8 KB
of WAL and 3 ms), and the park rate is bounded by the number of wide deferrals.
No-Regression Evidence: parks of 5 and 16 blockers cost the same WAL (3.4 and
3.9 KB against 3.3 and 4.7 KB) and the same order of time; the wake statement is
unchanged, its plan and cost are above. The size guard adds one length
comparison per park.

Observability Evidence: `eshu_dp_reducer_waiting_key_truncated_total{domain}`
counts a park that named fewer scopes than its verdict held (expected during a
bootstrap); `eshu_dp_reducer_waiting_key_oversize_total{kind}` and the ERROR log
`reducer_waiting.key_oversize` (item, kind, row bytes, budget, blocking
scopes) report a wait that was not parked, with the queue row still `retrying`
at the backstop. Both counters have telemetry-coverage rows and a reference
page (`docs/public/reference/telemetry/waiting-key-bound.md`). Hermetic tests
read both counters.

Not changed here, by design: the key still reaches `reducer_waiting_items` as
before; a rolling deploy leaves old pods writing wide keys that the new code
reads, wakes and audits (proof 8).

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
