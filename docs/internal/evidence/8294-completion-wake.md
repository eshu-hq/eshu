# Completion-Driven Wake For deployable_unit_correlation

Issue #8294 (Refs #8283). A `deployment_mapping` pass that appends resolved
rows under an already-active generation now wakes the succeeded
`deployable_unit_correlation` items whose recorded input no longer holds, in
that same resolution pass, instead of waiting for the next maintenance pass
a one-shot run may never get.

## Design (after the Deep-tier preliminary review of the first cut)

- **Convergence, not a one-shot trigger.** The first cut gated the wake on
  `Inserted > 0`, but the per-row inserts autocommit before the wake, so any
  wake failure, partial-upsert failure, or crash in between consumed the
  trigger and the retry never re-woke. `UpsertResolved` now reports `RepoIDs`
  over the full upsert set, the wake runs on every resolution pass over that
  scope, and the #8283 changed-evidence verdict (missing fingerprint row,
  moved logic version, or fingerprint mismatch against
  `ResolvedInputFingerprintSQL`) decides what reopens. A lost wake converges
  on the ordinary retry; a settled re-run is a no-op.
- **Replay floor.** Both wake arms compose the single scope replay floor
  (`backfill.ScopeReplayFloorCTE` / `ScopeReplayFloorJoins`, exported for
  this one composer) with the failed-generation exclusion and floor tuple
  predicate, so below-floor succeeded history is never locked or reopened.
- **Ordered, bounded locking.** Candidates lock `FOR UPDATE` in
  `work_item_id COLLATE "C"` order inside one transaction under a 2 s
  `lock_timeout`; 40P01/55P03 classify as retryable
  `CompletionWakeContentionError`. The lock rechecks the candidate quals
  (EvalPlanQual), so a row another transaction reopened or claimed in the
  window drops out unrewritten.
- **In-flight items.** A claimed/running item that read the pre-append input
  keeps its lease and gains `cross_scope_replay_required` (the flag arm runs
  first and names no status column, so the migration 093 ACK trigger, which
  is `BEFORE UPDATE OF status`, fires only at the real ACK and returns the
  item to pending).
- **Missing fingerprint rows** (pre-#8283, #8316-invalidated, or a crash
  before record) reopen under the floor bound, matching the maintenance
  gate's always-reopen treatment — for the succeeded arm. The in-flight arm
  requires a recorded fingerprint row instead: with no recorded input there
  is nothing to prove stale, and flagging a mid-first-execution claim forces
  a redundant replay whose diverted ACK resets `attempt_count`, erasing
  kill-recovery reclaim evidence (fault-injection shard 3/4
  `killworkerdeployableunit` failed with `1|0, want 1|1` because a pass that
  inserted nothing flagged the admission-blocked victim; the fix narrows only
  the in-flight arm, lock order and retryable-contention behavior unchanged).

No-Regression Evidence: live PostgreSQL 18 proofs in
`relationship_store_completion_wake_live_test.go` (isolated schemas, full
production bootstrap, production store/queue code throughout) all pass:
floor (below-floor stays succeeded, at-floor reopens), the production
statement's EXPLAIN (ANALYZE, BUFFERS) over 30 below-floor plus 1 at-floor
changed items (materialized `scope_replay_floor` CTE, `LockRows rows=1`,
0.905 ms), missing fingerprint, unchanged-noop, live `INSERT 0 0` reporting
`Inserted=0` with the retry wake still reopening, the running-item replay
flag through the ACK trigger, a claimed item with no recorded fingerprint
left unflagged (kill-worker reclaim evidence:
`TestCompletionWakeClaimedMissingFingerprintNotFlaggedLive`; its live run
is pending in CI — this host has no Postgres server, only the client — while
the hermetic query-shape twin `TestCompletionWakeFlagArmRequiresRecordedFingerprint`
was observed RED pre-fix and GREEN post-fix locally), lock_timeout giving up as classified 55P03 in
about 300 ms behind a held row, an EvalPlanQual drop against a concurrent
maintenance reopen (sentinel `reopened_at` preserved, wake counts 0), and a
wake/maintenance-reopen/claim race with no raw deadlock escaping. The same
tests fail on the pre-fix statement for the reviewed reasons (below-floor
reopened, missing-fingerprint blind, unchanged churned, running item
missed). One bounded wake transaction per resolution that resolved rows; no
worker, batch, lease, or claim shape changes.

Observability Evidence: `eshu_dp_reducer_completion_wake_total{outcome}`
counts one per wake transaction (reopened, noop, error), reopened rows add
to `eshu_dp_correlation_reopened_total{domain="deployable_unit_correlation"}`
beside the maintenance reopen, and each wake logs scope, generation,
inserted, reopened, and replay-flagged counts. Emission is asserted live in
`TestCompletionWakeMetricsLive` (reopened=1, noop=1, correlation_reopened
>= 1); statements are timed by `eshu_dp_postgres_query_duration_seconds`.
