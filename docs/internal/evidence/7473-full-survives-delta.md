# #7473: a pending full snapshot must survive a newer delta

## Problem

The claim path's stale-generation supersede retired a `pending` full
generation (reconcile or heal full) when a newer delta arrived before the
full was claimed. Branch 1 of `supersedable_projector_generations`
(`projector_queue_claim_sql.go`) selects a stale generation with any newer
same-scope generation and has no full-versus-delta distinction, so the full
was treated like any other stale generation. The supersede records
`failure_class = 'projector_superseded_by_newer_generation'` and carries no
heal reason forward, and the retired full then holds the collector's
reconcile sweep off for a quarter of the reconcile interval (6 h by
default): a hot repository under projector backlog repeats the heal in 6 h
steps instead of at the next sync.

Gating the supersede alone is insufficient: a full backing off a retryable
failure waits on `visible_at`, so without an admission guard the newer delta
is claimed first anyway — even when the full is visible, when the full's
retry failed after the delta was enqueued and sorts after it by
`updated_at`.

Root-Cause Evidence: on the base tree,
`TestProjectorClaimFullSurvivesNewerDelta`,
`TestProjectorClaimHoldsDeltaBehindBackoffFull`,
`TestProjectorClaimRunsDriftedFullBeforeNewerDelta`, and
`TestProjectorClaimClaimsDeltaBehindTerminalFull` fail: the newer delta is
claimed first and the full is superseded. The controls
(`TestProjectorClaimStillSupersedesFullBehindNewerFull`,
`TestProjectorClaimStillSupersedesDeltaBehindNewerDelta`,
`TestProjectorAckStillSupersedesFullBehindAckedDelta`) stay GREEN on both
trees.

## Design decision (arbiter: Muse Spark)

The issue offers two fixes: let the full survive, or carry the heal reason
onto the superseding generation. Verdict: survive plus hold, in the claim
statement only.

- A newer full covers the older full's heal (a complete observation at a
  newer instant), but a newer delta does not. So a stale full is
  supersedable only when a newer full exists; sparing it from a delta is
  always safe, and the oldest-ready order then projects the full first.
- Carrying the reason forward would need a new consumer contract: nothing
  reads a carried reason today, so the collector or projector would have to
  learn it and the reconcile throttle would need a bypass. Larger blast
  radius for the same outcome.
- Ack's obsolete-generation sweep deliberately stays full-blind as the
  misordering backstop. If a full's retry commits after a delta's claim
  snapshot (the race corner below), the delta is already claimed; retiring
  the full at the delta's Ack keeps generation order, while sparing it
  there would project stale full content over the newer delta and cascade
  into #7319 baseline refusals. Ordering is a correctness invariant; a
  deferred heal is a latency cost.

## Fix implemented

- Branch 1 of `supersedable_projector_generations` requires
  `(stale_generation.is_delta OR NOT newer_generation.is_delta)`: a stale
  full is supersedable only by a newer full. A stale delta keeps today's
  rule. Branch 2 (already-`superseded` generations) is untouched.
- The oldest-ready subquery skips a delta held behind an older same-scope
  full with waiting (`pending`, `retrying`) work. The hold lives inside
  the oldest-ready subquery (a copy in the pool's outer WHERE stalls when
  the holder sorts after the held row by `updated_at`), compares
  generation order in row-form (`(ingested_at, generation_id) < (...)`),
  identical to the OR tiebreak in branch 1, and restricts the held side
  to deltas: a newer full supersedes the older one instead of waiting
  behind it, so no claim round is wasted. Terminal (`failed`,
  `dead_letter`) fulls never hold: they cannot become claimable on their
  own, and the collector's next reconcile decision (not the queue) heals
  them.
- No lock-clause change: the additions lock no rows (pinned by unit test).
- The statement outgrew the 500-line file cap (530 lines), so the const
  split at the sweep/pool CTE boundary into `claimProjectorWorkSweepSQL`
  (`projector_queue_claim_sql.go`, 278 lines) and
  `claimProjectorWorkPoolSQL` (new file
  `projector_queue_claim_pool_sql.go`, 273 lines), assembled by
  concatenation. A segment-level comparison of the pre-split const
  against sweep+pool proves the shipped SQL byte-identical; the
  before-text round-trip unit tests re-pass on the assembled const. The
  new file re-pins the `internal/storage/postgres` dirgate ledger row
  335 to 336 with a regenerated `grandfather.go` mirror.

## Proof

All proof ran against `postgres:18` on a disposable database.

- Functional: the four RED tests above are GREEN with the fix; the three
  controls stay GREEN on both trees. Genuine supersedes keep
  `failure_class` `projector_superseded_by_newer_generation`.
- EvalPlanQual: `TestProjectorClaimDropsFullHolderClaimedAfterSnapshot` (a
  full another worker claimed and committed between snapshot and lock is
  dropped at lock time, with no fall-through to the held delta) and
  `TestProjectorClaimFullGuardAgreesWithFenceAcrossClocks` (hold and #7115
  fence agree across clocks; scope keeps exactly one lease). `is_delta`
  is stable per generation in all current writers (the only production
  writer is the generation upsert in `ingestion_queries.go`), so there
  is no marker-vs-sweep-style interleave to race; the deterministic EPQ
  tests plus the fence cover the changed predicate.
- Mutation checks: removing the spare fails the three spare tests; removing
  the hold fails the backoff and drifted tests while the natural-order
  full test still passes on oldest-ready ordering alone. The tests pin
  behavior, not text.
- Plan and locks: `TestProjectorClaimFullGuardPlanShape[AtScale]` runs
  EXPLAIN (ANALYZE, BUFFERS) before/after on the #7115 harness shape and
  at 2,000 scopes with newer deltas. At scale the hold's probes are all
  index-backed (`fact_work_items_scope_generation_idx`, two
  `scope_generations_pkey` probes), the per-table sequential-scan set is
  identical before/after, and the LockRows node count is identical.
  `TestProjectorClaimFullGuardLockSet` probes the shipped claim in an
  open transaction: the claimed full work row and its fence row lock;
  the held delta, the spared full generation row, `ingestion_scopes`,
  and untouched fences stay free. The unit test pins identical
  locking-clause counts before/after.
- Regression: the claim/fence/supersede/ack live family green (79 tests,
  0 failures, including the #7469 guard set and the 1,000-iteration
  marker-vs-sweep race); the live-tests ledger verifies (684 rows).

No-Regression Evidence: `TestProjectorClaimFullGuardContention` (16
workers, 512 single-generation scopes, 5 interleaved rounds per variant, 0
misses, 0 deadlocks): before median 1.59 s per 512-claim window, after
median 1.63 s, delta +2.32% (per-claim mean 11.20 ms before, 15.32 ms
after; cold-start rounds inflate the means, medians reported). Same-machine
relative comparison only: contributor hardware on a host shared with peer
sessions, not a quiet host or the reference profile, so no absolute target
is claimed. The test asserts correctness only, never timing; +2.32% is
inside run-to-run noise and under the 10% stop-and-profile threshold, so
no profile gate trips. EXPLAIN shows no plan flip, no new scans, no new
locks.

No-Observability-Change: no new metric, span, log key, or status. A held
claim returns `ok=false`, indistinguishable from "nothing ready", which
is correct transient queue state bounded by the retry backoff; any
counter would need a second statement or a RETURNING change on the hot
claim. The state stays diagnosable from row shape (full generation with
waiting work plus a pending newer delta).

## Residual: the post-snapshot retry corner (accepted)

One corner keeps status-quo behavior: the full's Fail-to-retrying commits
after the delta's claim snapshot, the delta claims (the snapshot saw no
waiting full), and Ack's full-blind sweep retires the full when the delta
Acks. The heal is deferred to the next reconcile decision per the
throttle instead of running now. This is the pre-fix outcome, not a new
loss, and it preserves generation order where the alternative (sparing in
Ack too) would project stale content. Closing it would need a
candidate-time re-verification of older generations; not pursued here.

## Runner enrollment

The four `projector_queue_claim_full_guard_*_live_test.go` proofs are
`postgres_ci` rows in `specs/live-tests.v1.yaml`, enrolled in the
live-postgres-readiness runner with their 13 test names pinned in
`scripts/lib/live_postgres_readiness_results.py`. They reuse the existing
`ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN` proof database (each test mints
and drops its own `claim_deadlock_proof_*` schema), already wired into
the runner script, its fail-closed test, and the workflow env. Local wall
time for the enrolled set is about 55 s on disposable PostgreSQL 18 (the
contention proof alone is about 30 s), inside the runner's 15-minute
per-package budget.
