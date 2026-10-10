# #7819: close the skip-blind split with a fence-bump-on-marker protocol

## Problem

After #7469, the projector claim can still split one statement two ways:
the sweep spares a marked retry (marker committed after the snapshot,
caught by the lock-time `marker_spared` flag) while the oldest-ready
subquery claims the scope's newer row in the same statement. It needs a
three-way conjunction: the holder is unmarked in the snapshot, the marker
transaction is in flight (and so SKIP-invisible) during the sweep's
generation pull, and the newer row sorts oldest by `updated_at`.

Root-Cause Evidence: on the base tree,
`TestProjectorClaimMarkerSweepRace` reports split decisions natural=0
drifted=198 of 1000 (reproduced on current main; the issue records 183
of 500 drifted), and the new `TestMarkProjectionWriteStartedFenceSync`
subtest `defers_on_busy_fence` fails: the marker marks through a held
fence row instead of deferring. The control (`no_bump_when_refused`)
stays GREEN on both trees.

## Design decision (arbiter: Muse Spark)

The issue offers two fixes: a fence bump on marker, or candidate-time
re-verification of older generations. Verdict: fence bump on marker,
with the marker locking the fence row SKIP LOCKED before the generation
row and deferring when the fence is busy.

- The fence lock serializes the marker commit against a claim's fence
  lock, which is the exact blind instant: a marker in flight makes the
  claim skip the scope (SKIP LOCKED, no wait), a claim in flight makes
  the marker defer (same `ErrWorkWriteMarkerDeferred` as a lock
  timeout, re-run by the existing 150-attempt loop), and a marker that
  commits between the claim's snapshot and its fence lock drops the
  candidate through the #7115 fence recheck. All six orderings of
  snapshot, fence lock, and marker commit are safe; the argument is in
  the PR body.
- Candidate-time re-verification would need blocking generation locks
  in the claim path to see an in-flight marker's future, which rejoins
  the #7108 wait cycles the claim's never-wait design removed.
  Rejected.
- Deadlock review: the claim never waits (all SKIP LOCKED), so no
  cycle can form with it. The marker's only wait stays the generation
  row under `lock_timeout`; its fence lock never waits. Two markers
  for one scope serialize on the fence row with defer-and-retry, and
  no other statement locks the fence table (verified: the sole
  `UPDATE projector_scope_claim_fences` outside the claim is the new
  bump; recovery, Ack, heartbeat, and the trigger never lock it).

## Fix implemented

`MarkProjectionWriteStarted`
(`go/internal/storage/postgres/projector_queue_delta_baseline.go`) now
runs three statements in its transaction: lock the scope's fence row
(`lockProjectorMarkerFenceQuery`, new file
`projector_queue_marker_fence_sql.go`), run the unchanged marker
UPDATE, and bump the fence (`bumpProjectorMarkerFenceQuery`) only when
the marker set. A busy or missing fence row rolls back and returns
`ErrWorkWriteMarkerDeferred`; a refused marker (superseded generation,
lost claim) bumps nothing. The #7115 protocol invariant comment in
`projector_queue_claim_sql.go` names the marker as the second fence
writer. The proof-domain tx harness routes both new statements (the
hermetic domain has no fence and no concurrent claimer, so lock finds
its row and bump is a no-op). The racing marker in
`TestProjectorClaimMarkerSweepRace` runs the shipped fence consts
verbatim around its modeled generation-row write.

## Proof

All proof ran against `postgres:18` on a disposable database.

- Functional: `TestMarkProjectionWriteStartedFenceSync` (busy fence
  defers, re-run marks and bumps by exactly one, refused marker bumps
  nothing) GREEN with the fix; the first subtest is RED on the base
  tree. The split assertion in `TestProjectorClaimMarkerSweepRace`
  reports natural=0 drifted=0 of 1000 with the fix (0 double wins, 0
  deadlocks, 0 errors).
- Mutation checks: a marker that skips the fence steps re-opens
  drifted=282 splits and fails the race; the fence-sync RED on the
  base tree pins the real marker's behavior. The two tests close the
  loop: the race proves a protocol-following marker never splits, the
  fence-sync test proves the shipped marker follows the protocol.
- EvalPlanQual: the #7469 EPQ set stays GREEN
  (`TestProjectorClaimDropsHolderClaimedAfterSnapshot`,
  `TestProjectorClaimGuardAgreesWithFenceAcrossClocks`,
  `TestProjectorClaimSweepSkipsGenerationLockedByMarkerTxn`), as do
  the #7473 EPQ tests and the fence lifecycle family
  (`TestProjectorClaimScopeFenceExcludesCrossSnapshotClaim`,
  `TestProjectorClaimSkipsBusyScopeWithoutWaiting`,
  `TestProjectorClaimLeavesFenceUnlockedWhenWorkRowBusy`,
  `TestProjectorClaimIgnoresIngestionHoldingScopeRow`,
  `TestProjectorClaimFenceRowLifecycle`,
  `TestProjectorClaimSkipsScopeWithoutFenceRow`).
- Plan and locks: the claim SQL is unchanged, so the marked- and
  full-guard plan-shape, at-scale, and lock-set tests re-run GREEN
  unmodified. EXPLAIN (ANALYZE, BUFFERS) on the two new statements at
  2,000 fence rows shows PK probes: the lock SELECT is an Index Scan
  plus LockRows at 0.095 ms (5 buffers hit), the bump UPDATE is an
  Index Scan at 0.185 ms (14 buffers hit). No new scans, no new lock
  modes; the claim's locking-clause counts are untouched.
- Regression: the full `go/internal/storage/postgres` unit suite
  GREEN (live tests skip without DSNs); the marker/heartbeat ordering
  family GREEN (5/5 subtests); the #7389
  `TestProjectorHeartbeatWriteMarkerInterleave` exclusion proof GREEN
  (1,000 races, marker-won 170, supersede-won 830, 0 deadlocks,
  exactly-one-wins on every trial) against the changed marker; the
  `go/internal/projector` write-marker loop tests GREEN; the
  live-tests ledger verifies (689 rows).

No-Regression Evidence: `TestProjectorClaimMarkedGuardContention` (16
workers, 512 scopes, 5 rounds per variant, 2,560 claims): before
median 1.761 s, after median 1.754 s, delta -0.42%.
`TestProjectorClaimFullGuardContention`: before median 2.976 s, after
median 3.033 s, delta +1.94%. Both deltas sit inside run-to-run noise
and under the 10% stop-and-profile threshold. Same-machine relative
comparison only: contributor hardware on a host shared with peer
sessions, not a quiet host or the reference profile, so no absolute
target is claimed. The marker path carries one extra PK SELECT and,
when the marker sets, one extra PK UPDATE per attempt; the claim hot
path is byte-identical.

Observability Evidence: no new metric, span, log key, or status. A
busy-fence deferral returns the existing
`ErrWorkWriteMarkerDeferred`, so the retry loop records it on the
#7470 write-marker `retried` outcome (and `gave_up` if the bound
exhausts) and the existing deferral log line fires — but neither
carries the cause: the counter has no fence value and the log message
stays "projector write marker waiting for busy generation row", so a
fence-busy deferral is indistinguishable from a generation-row wait in
both signals. The fence cause surfaces only in the returned error
text (`claim fence busy or missing`), visible on terminal paths.
Cause-aware enrichment is follow-up material (#7907, which also
covers pacing the retry loop for ms-defers). The fence bump rate is
visible in the existing fence-row
churn; a scope whose claims chronically skip on a hot fence shows as
repeated claim no-ops, the same signal as today's contended scope.

## Runner enrollment

`projector_queue_claim_marker_fence_live_test.go` is a `postgres_ci`
row in `specs/live-tests.v1.yaml`, enrolled in the
live-postgres-readiness runner with its test name pinned in
`scripts/lib/live_postgres_readiness_results.py`. It reuses the
existing `ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN` proof database (each
test mints and drops its own `liveness_proof_*` schema), already
wired into the runner. Local wall time is about 0.2 s.
