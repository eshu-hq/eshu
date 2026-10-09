# #7469: the claim path must not retire a generation that started writing

## Problem

A projector generation that set `projection_write_started_at` (migration 149)
and then failed retryably was retired when a newer generation arrived. Branch 1
of `supersedable_projector_generations` (`projector_queue_claim_sql.go`) and
Ack's `supersedeProjectorObsoleteGenerationsQuery`
(`projector_queue_sql.go`) never read the marker, so the claim sweep or the
newer generation's Ack superseded the marked generation's work row and
generation. The retired retry never ran, its partial overlay stranded in the
graph, and only the collector's forced full snapshot (`graph_dirty`) healed it.

Gating the supersedes alone is insufficient: the retrying row waits on
`visible_at` (the claim gates visibility only, never `next_attempt_at`), so
without an admission guard the newer generation is claimed first anyway — even
when the retry is visible, when the retry failed after the newer row was
enqueued and sorts after it by `updated_at`.

Root-Cause Evidence: on the base tree,
`TestProjectorClaimHoldsNewerBehindInvisibleMarkedRetry`,
`TestProjectorClaimRunsVisibleMarkedRetryBeforeNewer` (both subtests),
`TestProjectorClaimClaimsNewerBehindTerminalMarkedGeneration`, and
`TestProjectorAckKeepsMarkedObsoleteGeneration` (both subtests) fail: the
newer generation is claimed first and the marked row is superseded. The
marker-vs-sweep interleave (`TestProjectorClaimMarkerSweepRace`) retires a
marked generation in 311 of 1,000 races on the base tree.

## Fix implemented

- Branch 1 of `supersedable_projector_generations` and Ack's obsolete
  supersede require `stale_generation.projection_write_started_at IS NULL`.
  Branch 2 (already-`superseded` generations) still sweeps regardless of
  marker: that retirement already happened, and gating it would stall its
  scope behind a row the pool always excludes.
- The generation lock step carries each locked row's lock-time marker truth
  as `marker_spared` (pending or failed, marker set). The work lock step
  excludes spared rows, so a marker that commits after the snapshot spares
  the row from the sweep.
- The oldest-ready subquery skips a row held behind an older same-scope
  marked generation with waiting (`pending`, `retrying`) work, read twice:
  once from the snapshot (`waiting` guard) and once from the sweep's
  lock-time `marker_spared` flag (`spared` guard). Both holds live inside
  the oldest-ready subquery: a copy in the pool's outer WHERE stalls when
  the holder is visible but sorts after the held row by `updated_at` (the
  held row drops out while the holder misses the oldest equality; measured
  with a theory shim before the change). Terminal (`failed`, `dead_letter`)
  marked rows never hold: they cannot become claimable on their own, and a
  `failed` marked generation is already an uncovered writer, so the
  `graph_dirty` full snapshot heals it instead of stalling its scope behind
  a manual replay.
- The order comparisons are row-form
  (`(ingested_at, generation_id) < (...)`), identical to the OR tiebreak in
  branch 1 (`ingested_at` is NOT NULL).

## Proof

All proof ran against `postgres:18` on a disposable database.

- Functional: `TestProjectorClaimHoldsNewerBehindInvisibleMarkedRetry`,
  `TestProjectorClaimRunsVisibleMarkedRetryBeforeNewer`,
  `TestProjectorClaimClaimsNewerBehindTerminalMarkedGeneration`,
  `TestProjectorAckKeepsMarkedObsoleteGeneration` — RED on the base tree,
  GREEN with the fix. Controls
  (`TestProjectorClaimStillSupersedesUnmarkedStaleGeneration`,
  `TestProjectorAckStillSupersedesUnmarkedObsoleteGeneration`) stay GREEN on
  both trees: genuine supersedes keep `failure_class`
  `projector_superseded_by_newer_generation`, so the `graph_dirty` residual
  watch keeps its meaning.
- EvalPlanQual: `TestProjectorClaimDropsHolderClaimedAfterSnapshot` (a row
  another worker claimed and committed between snapshot and lock is dropped
  at lock time, with no fall-through to the held row),
  `TestProjectorClaimGuardAgreesWithFenceAcrossClocks` (guard and #7115
  fence agree across clocks; scope keeps exactly one lease),
  `TestProjectorClaimSweepSkipsGenerationLockedByMarkerTxn` (the sweep never
  waits on an in-flight marker). Pausing inside the sweep UPDATE cannot stage
  a marker race — the work-lock ORDER BY sort eagerly consumes the
  generation-lock CTE, so the claim already holds the generation row there
  (measured: the marker then times out with 55P03) — hence the interleave.
- Interleaves: `TestProjectorClaimRetryAgainstNewerInterleave` runs 1,000
  retry-claim-vs-newer-claim races across clocks: retry won 1000,
  newer won 0, deadlocks 0, errors 0.
  `TestProjectorClaimMarkerSweepRace` runs 1,000 marker-vs-sweep races with
  forced oldest-ready order parity: double wins (sweep retired a marked
  generation) 0 of 1,000 on both parities, deadlocks 0, errors 0.
- Mutation checks: removing the work-lock `NOT marker_spared` exclusion
  yields 43 double wins (FAIL); removing the lock-time oldest-ready guard
  removes the drifted held class (6 held trials become splits). The test
  pins the behavior, not the text.
- Plan and locks: `TestProjectorClaimMarkedGuardPlanShape[AtScale]` runs
  EXPLAIN (ANALYZE, BUFFERS) before/after on the #7115 harness shape and at
  2,000 scopes. At scale the guard's probes are all index-backed
  (`fact_work_items_scope_generation_idx`, two `scope_generations_pkey`
  probes), the per-table sequential-scan set is identical before/after, and
  the LockRows node count is identical. `TestProjectorClaimMarkedGuardLockSet`
  probes the shipped claim in an open transaction: the claimed work row and
  its fence row lock; the held row, the spared generation row,
  `ingestion_scopes`, and untouched fences stay free. The unit test pins
  identical locking-clause counts before/after.
- Regression: full `go/internal/storage/postgres` unit suite green; the
  claim/fence/supersede/heartbeat/marker/reclaim live set green (76 s);
  the wider projector/ack/heartbeat/replay live net green; `go vet` clean;
  `golangci-lint run ./internal/storage/postgres/` 0 issues.

No-Regression Evidence: `TestProjectorClaimMarkedGuardContention` (16
workers, 512 single-generation scopes, 5 interleaved rounds per variant, 0
misses, 0 deadlocks, two samples): sample 1 before median 1.94 s per
512-claim window, after median 2.13 s, delta +9.78%; sample 2 before median
1.75 s, after median 1.96 s, delta +12.06% (+0.4 ms per claim) on this
adversarial all-ready shape; per-claim means are noisier under shared-box
load (medians reported). Same-machine relative comparison only: contributor
hardware on a host shared with two peer sessions, not a quiet host or the
reference profile, so no absolute target is claimed. The delta is fully
attributed, not a mystery: each claim evaluates the full 512-row pool, and
each oldest-ready evaluation runs the two index-backed guard subplans
(~1,024 subplan evaluations per claim at ~0.4 us each ≈ +0.4 ms), amplified
by 16-way CPU contention; EXPLAIN shows no plan flip, no new scans, no new
locks. Realistic shapes stay sub-millisecond in both variants with no
measurable delta. Steady-state guard cost at 2,000 adversarially ready
scopes is ~10 ms of probes on a ~555 ms baseline (~2%); the remainder of a
single-run wall delta there is one-time JIT emission from crossing
`jit_above_cost`, amortized because pgx prepares and caches the claim per
pooled connection.

No-Observability-Change: no new metric, span, log key, or status. A held
claim returns `ok=false`, indistinguishable from "nothing ready", which is
correct transient queue state bounded by the retry backoff; any counter
would need a second statement or a RETURNING change on the hot claim. The
state stays diagnosable from row shape (marked generation with retrying work
plus a pending newer row). A `marker_spared` sweep exclusion is visible in
`failure_details` only when the sweep takes other rows.

## Residual: the skip-blind split (follow-up)

The race measures a residual split decision, drifted parity only (186 of 500
drifted trials in one run; 0 of 500 natural): the sweep spares the marked
retry while the same statement claims the newer row. It needs a three-way
conjunction: the holder unmarked in the snapshot, the marker transaction
in flight (and so SKIP-invisible) during the sweep's generation pull, and
the newer row sorting oldest by `updated_at`. Predicting the in-flight
transaction's future would need serialization (forbidden: #7108 deadlocks)
or a fence-bump-on-marker protocol change (the fence invariant currently
allows only lease creation to bump it). The consequence is bounded and
strictly milder than the pre-fix always-retire: a temporary backward scope
pointer — delta projections are still refused by the #7319 fence, and the
next sync heals automatically. Follow-up: close the skip-blindness (fence
bump on marker, or candidate-time re-verification of older generations).

## Runner enrollment

The five `projector_queue_claim_marked_guard_*_live_test.go` proofs are
`postgres_ci` rows in `specs/live-tests.v1.yaml`, enrolled in the
live-postgres-readiness runner with their 15 test names pinned in
`scripts/lib/live_postgres_readiness_results.py`. They reuse the existing
`ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN` proof database (each test mints
and drops its own `claim_deadlock_proof_*` schema), now wired into the
runner script, its fail-closed test, and the workflow env alongside its
`_DISPOSABLE` opt-in. Local wall time for the enrolled set is about 96s on
disposable PostgreSQL 18 (the contention proof alone is about 60s), inside
the runner's 15-minute per-package budget.
