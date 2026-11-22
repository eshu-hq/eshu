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
window. On main (`662083f413`) the waiter kept its wait row and the backstop
`visible_at`:

```text
waiter ack-park-held-waiter still parked (wait rows = 1, visible_at = <clock+10m>,
backstop = <clock+10m>): the Ack's wake was lost and only the audit or the
backstop can reopen it (#11228)
```

## Fix

After the commit, `ProjectorQueue.wakeAckWaitsAfterCommit` runs the Ack's
wakes again on the pool connection. These are the terminal-generation probe,
the acked generation's generation_active key, and the producer-terminal keys
of the collector kind. The re-wake is detached from the caller's cancellation
and bounded by `AckPostCommitWakeTimeout` (5 s). This is the design #11104
shipped for exact-phase waits (`WakeExactPhaseWaitsAfterCommit`).

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

The fix does not raise `GATE_DRAIN_TIMEOUT`, cut workers, or shorten the
audit.

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
- `eshu_dp_reducer_waiting_audit_resolved_total{failure_class, outcome}` is
  new. It counts the waits the slow audit resolved instead of a wake:
  `reopened` when the audit reopened a satisfied wait, `reconciled` when the
  queue row had already left the set-aside (usually the backstop claim).
  Any `reopened` increase for a class whose wake site is in-tx (such as
  `generation_activation_not_ready`) is a lost wake. Audit-only classes such
  as `shared_edge_target_not_ready` resolve there by design.
  `TestWaitingAuditResolvedCounterLive` pins both outcomes.
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
