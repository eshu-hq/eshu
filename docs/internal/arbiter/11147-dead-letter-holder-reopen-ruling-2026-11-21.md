# Arbiter ruling: a dead-lettered holder pins its stale copies (#11147)

Date: 2026-11-21. Refs #11147, epic #10860, #10765, #9432, #11141, #10643.
Arbiter: a separate-context deepest-tier model, given the fan-out source on
`main` at 7c2948825d and a read-only census of the QA replica. The coordinator
recorded the ruling, and the implementing lane re-checked each source claim
below against that tree. This file amends the bound recorded in
[the #10765 ruling](10765-stale-copy-bound-ruling-2026-11-16.md), which stays as
it was written.

## Decision

**A dead-lettered holder named by a change is a correctness gap, not a state for
the operator to own. The holder fan-out reopens it as a full genuine pass.** The
dominant cause of these dead letters is the holder-read statement timeout
(#10643), which is fixed first, in its own PR.

## What changes

1. The holder reopen also matches a `dead_letter` row at the scope's active
   generation. The row goes to `pending` with `attempt_count = 0`, lease, retry,
   and failure fields cleared, and `visible_at` and `reopened_at` set to now.
   The container-image-identity authorization columns are set the way the
   admin replay sets them. `fanout_origin` is removed from the payload and no
   new stamp is written. `fanout_owed` and `fanout_remaining` stay.
2. The pass must be genuine and full. A stamped attempt-1 pass runs as a
   CVE-scoped one hop (`fanOutOneHop`: stamped, attempt at most 1, not owed),
   marks the item succeeded, and hides the unfinished genuine pass.
3. Classes: every dead-letter class except those in the admin unsafe-replay
   set, read from its one source (`recovery.UnsafeReplayFailureClasses`, which
   the replay endpoint also reads). A dead letter in an unsafe class stays dead.
4. No attempt cap across reopens. A cap is a silent stale copy. The cost is
   bounded by the change rate times the attempt budget.
5. The fan-out failure class stays counting. A non-counting class would loop
   10-second statements against the database.
6. Abandon at shutdown should not spend the dead-letter budget, but not by
   enrolling that class as non-counting. That would freeze `attempt_count` at
   1, so a genuine pass stamped while running and abandoned before its commit
   would retry as a one hop and never fan out its own changes. The rule is to
   track shutdown abandons apart from `attempt_count`, or to make one-hop
   detection independent of the attempt count, proven by a live test of that
   exact interleaving. This is deferred behind the holder-read fix and this
   reopen (#11154); abandons were 10 of the 249 dead letters.

## Bound text

The #10765 bound (one fan-out pass, one holder pass, one winners resweep tick)
gains three sentences, now in the HTTP API page, the winners telemetry page,
and the fan-out contract:

> The bound holds only while every holder's item is reopenable and the
> originating pass finishes its fan-out. A dead-lettered holder, or a
> dead-lettered pass that still owes a fan-out, pins its copies until it is
> reopened. Queue latency is part of the bound.

## Telemetry

- Fan-out outcomes `dead_letter_reopened` and `dead_letter_skipped`, each with
  the prior `failure_class`, and `no_active_generation`. `pending_noop` now
  means only a queued or already-flagged row.
- One log line per reopened or skipped dead letter, naming the prior class.
- A gauge of active-generation dead letters by class, with a separate series
  for those carrying `fanout_owed` or `fanout_remaining`. The alert is the
  owed series above zero for 30 minutes.

## Evidence behind the ruling

Source, read on `main` at 7c2948825d:

- The reopen selected only `succeeded, claimed, running, pending, retrying`. A
  dead letter fell to the enqueue path, whose insert is `ON CONFLICT
  (work_item_id) DO NOTHING`, so it reported `pending_noop`.
- `supply_chain_impact_fanout_failed` is retryable and counting on purpose.
  Abandon at shutdown is also a counting retryable class.
- The reducer dead-letter transition sets `cross_scope_replay_required =
  FALSE`.
- The #10566 retouch precedent already treats a touch on a dead-letter slot as
  "new evidence, not the same poison" and reopens it with zeroed attempts.
- The admin replay (`POST /api/v0/admin/replay`) sets `attempt_count =
  GREATEST(attempt_count, 1)` and writes `fact_replay_events`.

QA replica census (supply_chain_impact reducer items):

- 249 dead letters: 231 `supply_chain_impact_fanout_failed`, 10
  `reducer_handler_abandoned_at_shutdown`, 8 `reducer_handler_timeout`. Another
  729 were `retrying` in the fan-out class.
- Every one of the fan-out failure messages was a statement timeout (SQLSTATE
  57014) in a fan-out read, mostly the holder lookup. That is #10643.
- 211 of the 231 carried `fanout_owed`: the writer committed and the holders
  were never told. All 10 abandoned dead letters carried it too.
- 11,103 winner rows had a winner scope whose item was dead-lettered.
- The oldest pending `visible_at` was about 15.7 hours old, so queue latency is
  not small.

## Order and what this ruling does not do

Order: #11144, then #10643, then the dead-letter reopen PR. The reopen PR
rebases on #11103's rename wave if that merges first.

Not done under this ruling:

- Lower worker counts, the holder cap, or the chunk size as the answer to the
  timeouts.
- Raise the 10-second holder-read guard. It would hide a regressed plan.
- Make the fan-out class non-counting.
- Add a recency rule to the winner select. No winner statement changes, so no
  winners contract version bump.
- Replay the dead letters already on QA. That waits for the #10643 fix to be
  deployed, two stable reads with zero new fan-out dead letters, and a fresh
  arbiter review (#11155).
- Change abandon-at-shutdown budgeting (#11154, deferred).

## Residual: a flagged holder that then dead-letters (#11159)

A running holder that a fan-out flags and that then dead-letters loses the
flag. The generic reducer dead-letter statement sets
`cross_scope_replay_required = FALSE`, and the migration 093 trigger clears it
on any transition other than `succeeded`, so the change that flag carried is
lost until another change reaches the holder. This PR does not fix it. A later
ruling (#11159) puts the fix in the dead-letter statement itself, with no
migration, in its own PR after this one.
