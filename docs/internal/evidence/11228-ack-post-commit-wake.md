# #11228: the projector Ack re-wakes its waits after its commit

Issue #11228: the Ifá determinism gate failed on PR #11219 (run 44992661362,
job 137117414046). The drain stopped with
`gcp_resource_materialization/retrying/generation_activation_not_ready` still
parked, past the 3-minute drain. The arbiter ruling
(`.claude/arbiter-merge-11219.md`, decision 4, issue B) asked for a red
reproduction first. The fix and both orderings come after it.

## Cause

This is a one-sided Dekker pattern between two writers that each write, then
read the other's write.

- The reducer park (`failIntent` → `setAsideWaitingForCause`) is not
  transactional. The retry `UPDATE`, the wait-row `INSERT`, and the provider
  re-check (`waitingSatisfiedQuery`) each autocommit on the pool. So the park
  writes first and reads second.
- The projector Ack activated the generation and ran its wakes
  (`reopenWaitingRowsQuery`) inside one transaction, before its commit. The
  wake reads only committed wait rows. So the Ack read before its write was
  visible.

If the park's insert commits after the Ack's wake statement starts, the wake
misses the row. If the park's re-check starts before the Ack commits, the
re-check misses the activation. Both miss, and the row waits for the audit
(5 minutes) or the backstop (10 minutes). The code named the miss:
"A missed wake still resolves through the audit and the 10-minute backstop."

The red reproduction observed this, so it is no longer an inference.
`TestWaitingAckParkInterleaveLive` holds the production `ProjectorQueue.Ack`
after its generation_active wake. It row-locks the producer-terminal wait row
that the Ack's next statement locks, and polls `pg_blocking_pids` until the
Ack blocks. It then runs the production `ReducerQueue.Fail` park inside that
window. On the pre-fix tree (the RED commit, on base `4f5d38b5f5`) the waiter
kept its wait row and the backstop `visible_at`:

```text
waiter ack-park-held-waiter still parked (wait rows = 1, visible_at = <clock+10m>,
backstop = <clock+10m>): the Ack's wake was lost and only the audit or the
backstop can reopen it (#11228)
```

## Fix

After the commit, `ProjectorQueue.wakeAckWaitsAfterCommit` runs the Ack's
wakes again on the pool connection. These are the terminal-generation probe,
the acked generation's generation_active key, and the producer-terminal keys
of the collector kind. One function, `wakeAckActivationWaits`, yields the keys
for both the in-tx wake and the re-wake, and
`TestAckPostCommitRewakeRepeatsTheInTxKeysAtTheWakeTime` shows the two lists
are equal for a producer-terminal kind (`oci_registry`). The re-wake reads the
queue clock when it runs, so `visible_at` and `reopened_at` move to the wake
time. It is detached from the caller's cancellation and bounded by
`AckPostCommitWakeTimeout` (5 s). This is the design #11104 shipped for
exact-phase waits (`WakeExactPhaseWaitsAfterCommit`).

Ordering proof, under read committed (each statement sees every transaction
committed before it starts):

- Call the park's insert commit `tP` and its re-check start `tR`, with
  `tP < tR`.
- Call the Ack's commit `tA` and its post-commit re-wake start `tW`, with
  `tA < tW`.
- A double miss needs `tA > tR`: the re-check did not see the activation.
- It also needs `tP > tW`: the re-wake did not see the wait row.
- Together that gives `tP > tW > tA > tR > tP`, which is a contradiction.

So at least one side reopens the row.

- **No new lock.** The re-wake takes the same locks as the in-tx wake: the
  waiting rows `FOR UPDATE` in `work_item_id COLLATE "C"` order, then their
  queue rows. It runs after the Ack released every lock, so it adds no
  wait-for edge to the Ack's lock order (#7389). No deadlock cycle is
  introduced.
- **Idempotent.** A woken wait row is deleted in the same statement. When
  the in-tx wake or the park's own re-check already reopened a waiter, the
  re-wake reopens nobody. The `park_first` live case asserts this:
  `wake_site="in_tx"` reopened 1, `wake_site="ack_post_commit"` reopened 0.
- **Failure.** A failed or timed-out re-wake logs ERROR and counts
  `outcome="error"`; it never fails the committed Ack. A crash between the
  commit and the re-wake is still bounded by the audit and the backstop. The
  in-tx wake still covers every waiter that parked before the Ack.
  `TestWaitingAckCrashResidualAuditLive` runs the held-window interleave with
  the re-wake skipped (`skipAckPostCommitWake`, a test-only seam): the waiter
  stays parked, the next audit pass reopens it, and the fallback counter
  records it as `reopened` on `wake_site="in_tx"`.

## Contention proof

Conflict domain: one scope generation, its eight reducer waiters' queue rows
and `generation_active` wait rows, the producer-terminal wait row the hold
plants, and the projector work row. `TestWaitingAckParkContentionLive` runs,
under `-race`, the activating Ack held after its in-tx wakes, a stale-owner
duplicate Ack of the same generation, the reducer `Fail` parks, a reducer
claim loop, a projector claim loop, and the waiting audit loop, each on its
own connection.

- Round `seeded` parks all eight waiters inside the window and stops the
  audit before the hold releases. Only the post-commit re-wake can reopen
  them. A later park would not do: its re-check sees the activation, and its
  own wake reopens every waiter on the shared generation key.
- Round `audit_throughout` parks four inside the window and four while the
  Ack commits, with the audit racing the re-wake and the parks' own wakes.
- Both rounds assert no 40P01 from any actor and no change in
  `pg_stat_database.deadlocks`, which also counts a deadlock the projector
  Claim retried internally. They also assert that no waiter is parked once
  the Ack committed, with attempt counts unchanged, and that an explicit
  audit pass afterwards reopens nobody.

With the re-wake removed (mutant), round `seeded` failed: all eight waiters
`still parked after the Ack committed (status "retrying", visible_at
<clock+10m>)`, and the final audit reopened 8. With the re-wake, both rounds
pass.

The fix does not raise `GATE_DRAIN_TIMEOUT`, cut workers, or shorten the
audit.

## Provider-site census

The ruling (decision 1, condition 3) requires every site that wakes
`generation_active` or `producer_domain_terminal` keys to re-wake after its
commit or to state why the window cannot occur. `TestWaitingWakeCensus`
parses the package's non-test sources and finds every call of the wake
helpers for these two kinds. It fails on an unregistered site, on a site that
wakes on an open transaction (a `Begin` result or a transaction parameter)
without a registered re-wake after its `Commit`, and on a pool site whose
function commits after the wake. The registry is
`waitingWakeCensusRegistry`.

| Site | Class | Why the window cannot occur |
| --- | --- | --- |
| `ProjectorQueue.Ack` (`projector_queue_ack.go`) | in-tx, re-woken | re-runs the same wakes after `Commit` (`wakeAckWaitsAfterCommit`) |
| terminal `ProjectorQueue.Fail` (`projector_queue.go`) | post-commit | `failProjectorWorkQuery` marks the generation `failed` in one autocommit statement on the pool; the wake starts after it returned |
| `claimOnce`, claimed and empty (`projector_queue_scan.go`) | post-commit | the claim sweep supersedes generations inside `claimProjectorWorkQuery`, one autocommit statement; the claimed probe starts after `rows.Close`, and the empty probe after `rows.Next` returned false, which in pgx v5 closes the result through `ReadyForQuery` (`baseRows.Next`, `ResultReader.Close`), sent only after the server committed the implicit transaction |
| heartbeat `supersedeRunningWork` (`projector_queue_scan.go`) | post-commit | `supersedeRunningProjectorWorkQuery` turns the generation terminal in one autocommit statement; the wake starts after `rows.Close` |
| reducer `Ack` and `AckBatch` | post-commit | autocommit Ack statements; the producer-terminal wake starts after the last one returned |
| `parkProducerTerminalWaits`, `setAsideWaitingForKey` | parker | the parker reopens its own key after its re-check saw the provider |
| `AuditWaitingItems` | audit | reads committed state and is itself the fallback |

For a post-commit site the ordering proof above applies unchanged: the
provider's write commits before the wake statement starts. The deferral map
text in `WaitingDeferralMap` said the heartbeat supersede wakes in-tx; it
wakes after its statement committed, and the text now says so. The seeded
RED/GREEN pair is `TestWaitingWakeCensusFlagsSeededSites`. On the real tree,
adding an in-tx `wakeGenerationActiveWaits` site in a throwaway file failed
the census (`unregistered wakeGenerationActiveWaits wake site (db tx)`), and
removing the Ack's re-wake failed it (`in-tx wake without a call to
wakeAckWaitsAfterCommit after Commit`).

Other wake kinds have the same write-then-read shape and are out of this
census: relationship activation (`relationship_store_resolved.go`) and the
winners watermark wakes. They are tracked as the follow-up #11250.

## Performance Evidence:

Each committed Ack adds two statements, plus one for each producer domain of
the collector kind (0 for `git`, 1 for `oci_registry` and `ci_cd_run`). Each
runs after the Ack's locks are released, so it is off the Ack's lock-hold
time. The cost comes from a shim measurement (`EXPLAIN (ANALYZE, BUFFERS)`,
three runs) on disposable postgres:18.6. The shim held 20,000 parked
generation_active waits over 2,000 pending generations, plus 2,000
producer-terminal waits, with the production
`reducer_waiting_items_wake_idx` and `reducer_waiting_items_scope_idx`.

| Statement (post-commit) | Plan | Execution |
| --- | --- | --- |
| generation_active wake, no-op key (common case) | wake_idx index scan; join never executed | 0.19 ms |
| producer-terminal wake, no-op key | wake_idx index scan | 0.07 ms |
| terminal-generation probe, no terminal generation | scan of `scope_generations`; wait index never executed | 0.13 ms |
| generation_active wake, hit (10 waiters in the window) | wake_idx index scan, 10 pkey probes | 0.29 ms |

The backstop-exit probe (`selectBackstopClaimWaitRowsQuery`) adds no
statement to a healthy reducer claim: it runs only when a claimed row of a
parking class has `visible_at` past `reopened_at`, which a woken row never
has. When it runs, it reads the `reducer_waiting_items` primary key by its
leading column. Shim on postgres:18.6 with 20,000 wait rows: 1 candidate is
an index-only scan on `reducer_waiting_items_pkey`, 4 buffers, 0.026 ms. 8
candidates are a bitmap index scan on the same key, 18 buffers, 0.023 to
0.041 ms.

The terminal probe already runs post-commit after every projector Claim
(#11016), and Ack and Claim run once per projector work item, so the Ack adds
at most one more probe per item. The shim table has no production status
index on `scope_generations`. See the #11016 note for the plan on the
production schema.

## Observability Evidence:

- `eshu_dp_reducer_waiting_wake_total{wake_site="ack_post_commit", outcome}`
  counts each re-wake statement as `reopened`, `noop`, or `error`. A steady
  `reopened` rate there counts waiters that parked inside an Ack window,
  which the old code left to the audit.
- `eshu_dp_reducer_waiting_fallback_resolved_total{failure_class, wake_site,
  outcome}` is new. It counts waits a fallback resolved instead of a wake.
  `wake_site` is the class's registered wake site from the waiting registry
  (`in_tx`, `completion_event`, `audit`, or `unregistered`), and
  `failure_class` is bounded to registered parking classes. The outcomes:
  - `reopened`: the slow audit reopened a satisfied wait.
  - `backstop`: the reducer `Claim` or `ClaimBatch` took the row at its
    10-minute backstop with its wait row still present. The probe runs only
    for claimed rows of a parking class whose `visible_at` is past their
    `reopened_at`. Every wake sets both to the wake time, so a woken row,
    including one woken on one key while a sibling key's wait row survives,
    sends no statement. The probe reads the `reducer_waiting_items` primary
    key after the claim committed. A failure logs WARN and never fails the
    claim.
  - `reconciled`: the queue row already left the set-aside and the audit
    dropped its wait row. This can follow a `backstop` count for the same
    wait.

  Any `reopened` or `backstop` increase with `wake_site` other than `audit`
  (such as `generation_activation_not_ready` on `in_tx`) is a lost wake.
  `wake_site="audit"` classes such as `shared_edge_target_not_ready` resolve
  there by design. `TestWaitingAuditResolvedCounterLive` pins `reopened` and
  `reconciled`. `TestWaitingBackstopClaimCounterLive` pins `backstop`
  through both claim paths, and pins that a woken-sibling row and a row
  without a wait row are not counted.
- A failed re-wake logs ERROR
  `projector post-commit generation_active wake failed; the audit and backstop still bound the miss`
  with `op=ack`, `scope_id` and `generation_id`.
- On a failed drain, `scripts/verify-ifa-determinism.sh` now dumps
  `scope_generations`, `reducer_waiting_items`, the non-terminal
  `fact_work_items`, and the full projector and reducer logs
  (`ifa_det_dump_drain_failure`). Before, it printed `tail -30`, which hid the
  Ack in run 44992661362.

## Second occurrence (PR #11192, fault-injection shard 2)

The `failgraphwritedeployableunit` residual
(`deployable_unit_correlation/retrying/shared_edge_target_not_ready`) is a
different edge. It is not this race.

- That class maps to `target_committed` with wake site `audit` by design
  (`reducer_waiting.go`: "sibling-scope endpoint commit; audit
  terminal-progress fallback + backstop"). No in-tx wake exists for it.
- It parked after the reducer's startup audit, and the cell's 4-minute drain
  is shorter than the 5-minute audit interval.
- The error says the post-write verification found the source Repository node
  `repo-ifa-deployable-unit-app` absent.
- Why that node was absent after the maintenance pass was not captured: the
  log kept only the tail of the reducer log. This is NOT_CHECKED here and is
  tracked separately.
