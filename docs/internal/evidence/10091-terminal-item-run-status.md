# A terminal item keeps its run open until its siblings drain (#10091)

`ReconcileRunProgress` used to fail a run as soon as one item failed terminally,
even with siblings still pending. A failed run leaves the open-target guard and
the reconciliation list, while its pending items stay claimable, so the next
planner pass could admit a second item for the same target. The run now becomes
`failed` only when none of its items is `pending` or `claimed`.

The rule comes from the arbiter ruling on #10091. Neither the claim SQL nor the
guard predicate changes, and there is no new lock. One reconciler read does
change: the concurrent-lifecycle proof below exposed that the #9257
factless-completed count came from a separate statement after the progress
counts, so under Read Committed a completion landing between the two
statements made factless exceed completed and failed the whole reconcile tick.
The probe now runs inside the progress statement, so every count the rule
validates against another count comes from one snapshot. No predicate changes;
one statement fewer per run per tick.

## Behavior

| Items in the run | Before | After |
|---|---|---|
| 60 pending + 1 `failed_terminal` | `failed`, finished | `collection_active`, not finished |
| 59 pending + 1 claimed + 1 `failed_terminal` | `failed`, finished | `collection_active`, not finished |
| 60 completed + 1 `failed_terminal` | `failed`, finished | `failed`, finished |
| 1 pending + 1 completed + terminal reducer dead-letter | `failed`, finished | `collection_active`, then `failed` once the item completes |

Completeness rows stay `blocked` from the first tick in every case, so the
failure is visible before the run ends.

A terminal item's scope is re-admitted at the first planner pass after its run
ends. On a healthy fleet that is the next visit. On a saturated fleet it waits for
the page to drain, which is the order the ruling asks for.

## Why the flip is safe

The conflict domain is one run's `workflow_work_items` rows, read by the
reconciler and the guard, and written by claim, complete, fail, release and reap.

The reconciler takes no planning lock. The guard reads `run.status` and the items
inside its advisory-locked transaction. The argument has three steps.

1. **The count is one snapshot.** `listWorkflowCollectorProgressQuery` counts all
   statuses in one statement, so one Read Committed snapshot covers them.
2. **An in-flight transition still reads as open.** Every transition starts from
   `pending` or `claimed`, and a concurrent reader sees the last committed row. So
   a snapshot with `pending = 0` and `claimed = 0` sees no item with an uncommitted
   transition.
3. **Nothing leaves a terminal state.** Each write that sets `pending` requires
   `item.status = 'claimed'` on the locked row: `failWorkflowClaimRetryableQuery`,
   `reapExpiredWorkflowClaimsQuery` and `releaseWorkflowClaimQuery`. No statement
   moves `completed`, `failed_terminal` or `expired` back.

So the flip happens only at a snapshot where every item has ended, and those items
stay ended. A guard read before the flip counts every item as open and admits
nothing. A guard read after the flip finds no open item left to duplicate.

Two limits on that argument, both already present before this change:

- **Two reconcilers can regress the status.** `updateWorkflowRunStatusQuery` has no
  predicate on the old status, so a coordinator holding an older snapshot can
  overwrite `failed` with `collection_active`. This is conservative: the guard then
  counts already-ended items as open, and the next tick corrects it. The rule never
  computes `failed` from a snapshot with an open item, so the reverse error, `failed`
  over a run with work left, cannot come from this path.
- **A same-run insert can miss the snapshot.** The guard can add new targets to an
  existing non-terminal run id. If that transaction commits after a reconcile
  snapshot that saw every other item ended, a pending item lands in a `failed` run.
  This needs a same-bucket replay with new targets, and the old rule was exposed to
  it in more states. The optional guard hardening in the ruling, which counts
  `item.status` directly, would close it. This change does not add that hardening.

A run whose siblings can never be claimed, for example because the collector
instance was removed, now stays open and keeps its targets closed. Any pending-only
run already behaves that way.

## Evidence

No-Regression Evidence: no predicate, index, lock, lease or claim path changes.
The only SQL text change folds the factless probe's two `NOT EXISTS` subqueries
into the progress statement's completed `FILTER`; they hit the same
`(scope_id, generation_id)` index prefixes for the same completed rows, and a
reconcile tick now costs three reads per run instead of four, plus one status
update and one upsert per required phase. Conflict domain: one run's
`workflow_work_items` rows; worker and lease settings are unchanged. A run with a
terminal item and pending siblings now stays in the reconciliation list until
collection ends, at the same per-tick cost as any active run. Row counts before and
after are the table above: the 61-item run moves from `failed` with 60 items still
claimable to `collection_active`, with those items still counted as open by the
guard. No throughput or latency was measured: the folded probe replaces an
identical per-completed-item probe, so the per-tick statement cost does not grow.

Observability Evidence: no new instrument. While the run is open, the signal is
`collection_active` run status plus the `blocked` rows in
`workflow_run_completeness`, counted by `recent_blocked_completeness` and
`recent_terminal_work_items` on the coordinator status surface. The run then
appears in `recent_failed_runs` once it ends.
`eshu_dp_workflow_run_terminal_dead_letter_blocks_total` and its warning log now
fire only on the reconciliation that terminates the run. That is what the counter
documents. Without this gate, a dead-lettered run with pending items would add to
the counter on every tick.

## Proof

Hermetic, RED before the fix and GREEN after:

- `TestReconcileRunProgressTerminalItemWaitsForCollectionToEnd` and its two
  companions in `go/internal/workflow/progress_terminal_siblings_test.go`.
- `TestReconcileCountsDeadLetterBlockOnlyWhenTheRunTerminates` in
  `go/internal/storage/postgres/run_reconciliation_terminal_blocks_test.go`.
- `TestReconcileFactlessCountSharesProgressSnapshot` in
  `go/internal/storage/postgres/run_reconciliation_factless_test.go` (regression for
  the cross-statement snapshot skew; RED before the probe was folded into the
  progress statement — on old code the 7-column fixture row fails the 6-destination
  progress scan (`scan destination count = 6, want 7`), and the 4-query-count
  assertion structurally forbids a separate factless statement. The production
  invariant text (`factless completed items must not exceed completed work items`,
  `go/internal/workflow/progress.go:137`) was observed in the live
  concurrent-lifecycle proof below that exposed the skew).

Live proofs on a disposable Postgres, in
`go/internal/storage/postgres/run_reconciliation_terminal_siblings_live_test.go`:

- `TestTerminalSiblingsStayOpenThroughReconcileGuardAndClaimLive` walks reconcile,
  guard and claim to the flip and the re-admission.
- `TestTerminalSiblingsEveryPathBackToPendingKeepsRunOpenLive` covers retry, release
  and lease expiry.
- `TestTerminalSiblingsFailedRunItemsStayFrozenLive` replays stale mutations after
  the flip.
- `TestTerminalSiblingsNoDuplicateUnderConcurrentLifecycleLive` races four workers,
  two reconcilers and a planner, and checks after every admission that no target
  holds two open items.
