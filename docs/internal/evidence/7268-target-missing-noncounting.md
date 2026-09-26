# #7268: shared-edge target deferral — non-counting, bounded, fail-closed

`deployable_unit_correlation` writes `CORRELATES_DEPLOYABLE_UNIT` inline
through the shared-edge writer. The writer's target-presence guard
(`go/internal/storage/cypher/edge/writer/unroutable.go`) fails a batch with a
retryable `targetMissingError` when an endpoint `Repository` node is not yet
in the graph. Another scope's materialization commits that node, with no
ordering against this batch. The error's own doc calls the miss "a timing
state, not a payload defect", but the error carried no failure class. The
reducer queue therefore counted every miss toward `MaxAttempts` (default 3).
When the sibling write ran slow, the intent dead-lettered. A dead letter is
never reopened, so the edge was lost.

This is the #6759 defect on the sibling probe. The #7123 Ifá diagnosis
(Shape C, run 36082600396) found it: a counted natural miss pushed the
injected fault onto attempt 2. Attempt 2's 120 s backoff then came due after
the 4-minute drain deadline.

## Change

1. `targetMissingError` now self-classifies as `shared_edge_target_not_ready`
   (`reducer.SharedEdgeTargetNotReadyFailureClass`). The class is enrolled in
   `nonCountingReducerRetryFailureClasses` and in the golden-corpus-gate
   readiness-deferred set.
2. The non-counting wait is bounded by elapsed time in
   `DeployableUnitCorrelationHandler`. That is the only caller whose miss
   reaches a `fact_work_items` attempt budget. The bound is
   `crossscope.ProducerReadinessMaxWait` (30 minutes) since
   `crossscope.ReadinessCycleAnchor(intent)`, which is
   `COALESCE(reopened_at, created_at)`.
   - Past the bound, the handler replaces the deferral with a retryable error.
     That error has no class and no `Unwrap`, so the ordinary budget
     dead-letters it.
   - A zero anchor keeps deferring.
   - The elapsed check (`targetDeferralExpired`) is now shared with the #6759
     deployment-source bound.
3. `targetProbeError` stays classless and counting, so it fails closed. Its
   comment had claimed it shared a non-counting class with the miss; that is
   corrected.
4. `TestEveryReadinessFailureClassIsEnrolled` used to scan only
   `internal/reducer`. It now also walks the rest of `go/internal`, for
   `FailureClass()` bodies that return a reducer-tree constant.

`handles_route` and `runs_in` hit the same guard through the shared-projection
worker. Its `shared_projection_intents` rows have no `attempt_count`, and a
failed batch is re-polled every cycle. The class is inert there, and the
30-minute bound does not cover those domains. Their re-poll behaviour is
unchanged by this fix.

## RED/GREEN

Every RED below was run against the pre-fix code. The class constant and a
pass-through bound stub existed so the tests compiled, which makes each RED
behavioural rather than a compile failure.

| Test | RED before the fix | GREEN after |
| --- | --- | --- |
| `TestTargetMissingErrorCarriesNonCountingReadinessClass` (writer) | `failure class = "" (found=false)` | pass |
| `TestTargetProbeErrorStaysCounting` (writer) | pass; it pins that the probe error stays classless | pass |
| `TestReducerQueueFailDefersSharedEdgeTargetMissPastAttemptBudget` (fake queue; real writer error at `AttemptCount=3`) | dead-lettered: `deferred retry query missing "status = 'retrying'"` | pass |
| `TestReducerQueueClaimKeepsSharedEdgeTargetDeferralAttemptCount` | claim CASE missing `work.failure_class = 'shared_edge_target_not_ready'` | pass |
| `TestReducerContentionGateSharedEdgeTargetDeferralKeepsItsAttemptBudget` (live Postgres 16) | `defer cycle 1: failure_class="reducer_retryable"` | pass in 0.39 s |
| `TestDeployableUnitCorrelationTargetDeferralIsBoundedByElapsedTime` (real handler) | past the bound, the class was still `shared_edge_target_not_ready` | pass (5 subtests) |
| `TestBoundSharedEdgeTargetDeferralLogsTheBoundTrip` | deferral returned unchanged past the bound | pass |

The live test drives the real claim/fail SQL against the real
`fact_work_items` DDL. Five defer cycles, more than `MaxAttempts=3`, each
reclaim at `attempt_count=1` and stay `retrying` under the class. The bounded
error's queue contract then counts from 1 to 2 to 3, and the row ends
`dead_letter` at 3. That second phase uses a stand-in with the bounded
error's queue contract (retryable, no class, no `Unwrap`), because the
reducer type is unexported. The reducer's own handler test proves the handler
returns exactly that contract.

Seeded guard check for `TestEveryReadinessFailureClassIsEnrolled`, with the
class removed from `nonCountingReducerRetryFailureClasses`:

- Old scan: PASS, so the guard was blind to the writer's class.
- Widened scan: FAIL, `shared_edge_target_not_ready
  (storage/cypher/edge/writer/unroutable.go)`.
- Widened scan with the class restored: PASS.

The widened scan takes about 0.9 s standalone, where the old scan took 0.09 s.

## Evidence

No-Regression Evidence: the fix adds one string method to `targetMissingError`
and one enrolled class string. The target probe and the MERGE statements are
untouched, as is their batching, so a batch whose targets are present runs no
new statement, round trip or lock. On a miss, the handler adds one `errors.As`
and one time subtraction against the cycle anchor. On the queue, a retrying
row in this class costs what the sibling non-counting classes cost: one claim
plus one retry UPDATE per retry delay, with a constant delay because
`attempt_count` is frozen. That cost is capped at 30 minutes of wall time,
where before the row stopped after 3 counted passes. The claim CASE gains one
more `failure_class = '...'` equality in the non-counting disjunction. Proof
is the focused Go, fake-Postgres and live-Postgres suites listed above, run on
go1.27.1 darwin/arm64 at the branch head.

Observability Evidence: a deferral shows as
`eshu_dp_reducer_retry_surge_total{failure_class="shared_edge_target_not_ready"}`
and as `fact_work_items.failure_class` on the retrying row. Each detected
miss still counts in
`eshu_dp_shared_edge_target_miss_total{domain="deployable_unit_edges"}`, with
the `shared edge batch target absent, deferring batch` WARN. Past the bound,
the handler logs `shared edge target absent past the wait bound, failing the
intent so the retry budget counts it`, with `domain`, `scope_id`,
`generation_id`, `elapsed_since_cycle_start`, `max_wait`, `sample_repo_id` and
`sample_intent_id`. It logs elapsed time, never `attempt_count`, which the
class froze. A probe fault logs `shared edge target probe failed, deferring
unverified batch` and counts. No new metric was added.
