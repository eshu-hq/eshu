# #7820: Ack's obsolete supersede reads the marker at lock time

## Problem

`supersedeProjectorObsoleteGenerationsQuery` (Ack's obsolete-generation
supersede) reads `projection_write_started_at` from the statement
snapshot only: the UPDATE locks just the work rows, and EvalPlanQual
rechecks only the updated rows. A marker that commits after the
statement's snapshot but before the scan reaches the row is missed,
and the Ack retires a generation whose writer is in flight.

Root-Cause Evidence: on the base tree,
`TestProjectorAckSeesLockTimeMarkerTruth/marker_commits_after_ack_snapshot`
stages the alignment deterministically (Ack provably blocked inside
the obsolete supersede before the marker commits) and gen-am1 lands
`superseded` with the marker set. The statistical race retires a
marked generation in 209 of 300 trials on the base tree. The sibling
subtest shows the in-flight case defers the whole Ack instead: the
generation UPDATE's plain row lock waits behind the marker's
uncommitted generation write until the lock timeout. (Neither UPDATE
modifies a key column, so no foreign-key check is involved:
PostgreSQL RI triggers skip unchanged keys.)

## Design decision (arbiter: Muse Spark)

The issue offers two outs: restructure the supersede
lock-then-update, or accept the window as residual. Verdict:
restructure. The window is not theoretical (the race hits it in most
trials), and the claim sweep already proves the pattern: lock the
stale generation rows `FOR NO KEY UPDATE ... SKIP LOCKED` in
generation order, then update only work rows whose generation the
lock step holds, re-applying the generation predicate for the
recheck. A marker committed before the lock attempt is dropped by
the lock's EvalPlanQual recheck; a marker still in flight is skipped
without waiting (waiting would only reach the lock timeout and
defer the Ack, today's behavior). The reverse race — a marker that
commits after Ack's locks — waits on the held generation row and is
then refused by its own status predicate (`ErrWorkSuperseded`),
exactly like the claim-sweep reverse race.

Deadlock review: the new locks are all SKIP LOCKED, so Ack gains no
wait edge; its documented blocking order (scope, work, other
generations, target generation) is unchanged. A concurrent marker
either commits first (then the lock recheck spares) or waits on the
held generation row (then its status predicate refuses). No cycle.

## Fix implemented

`supersedeProjectorObsoleteGenerationsQuery`
(`go/internal/storage/postgres/projector_queue_sql.go`) gains a
`locked_obsolete_stale_generations` lock CTE ahead of
`superseded_work`; the work UPDATE joins the locked set and repeats
the status/marker/ordering predicate. The `markGuardAckGate` text
now appears twice (lock step plus re-applied predicate), pinned by
`TestAckObsoleteSupersedeLocksStaleGenerations` along with the lock
clause, the deterministic lock order, and the absence of any new
work-row locking clause. No other statement changes; the second
UPDATE (generation rows to `superseded`) re-locks rows this
statement already holds, so it never waits.

## Proof

All proof ran against `postgres:18` on a disposable database.

- Functional: `TestProjectorAckSeesLockTimeMarkerTruth` — both
  subtests RED on the base tree (superseded-despite-marker;
  Ack-deferred-on-in-flight-marker) and GREEN with the fix.
  `TestProjectorAckMarkerCommitRace`: 0 violations and 0 Ack errors
  in 300 trials with the fix, against 209 violations on the base
  tree (same seed, same fixture).
- EvalPlanQual: the #7469 EPQ set stays GREEN
  (`TestProjectorClaimDropsHolderClaimedAfterSnapshot`,
  `TestProjectorClaimGuardAgreesWithFenceAcrossClocks`,
  `TestProjectorClaimSweepSkipsGenerationLockedByMarkerTxn`,
  `TestProjectorClaimMarkerSweepRace` with its measured residual),
  as do the #7473 EPQ tests, the `TestProjectorAck*` family
  (including the committed-marker and unmarked controls), and the
  supersede prior-failure fidelity tests that execute the changed
  statement directly.
- Plan: EXPLAIN (ANALYZE, BUFFERS) on a 60-generation obsolete
  scope, before text vs after text derived from the shipped
  constants. Before: hash-join shape, 402 buffers hit, 0.857 ms.
  After: same updated row set (59 rows) plus LockRows over the
  ordered lock step, 507 buffers hit (+26%), 1.066 ms (+0.2 ms).
  No new scan types; the shape mirrors the claim sweep's accepted
  lock CTE.
- Regression: the full `go/internal/storage/postgres` unit suite
  GREEN (live tests skip without DSNs); the live-tests ledger
  verifies.

No-Regression Evidence: `TestProjectorClaimMarkedGuardContention`
(16 workers, 512 scopes, 5 rounds per variant, 2,560 claims):
before median 2.013 s, after median 2.080 s, delta +3.36%.
`TestProjectorClaimFullGuardContention`: before median 2.021 s,
after median 2.060 s, delta +1.94%. Both deltas sit inside
run-to-run noise and under the 10% stop-and-profile threshold.
Same-machine relative comparison only: contributor hardware on a
host shared with peer sessions, not a quiet host or the reference
profile, so no absolute target is claimed. The claim hot path is
byte-identical; the added work lands on the Ack path (once per
Ack, +0.2 ms at 60 obsolete generations).

Observability Evidence: no new metric, span, log key, or status.
The failure classes are unchanged (`obsolete` /
`projector_superseded_by_newer_generation`); an Ack that skips a
busy generation succeeds instead of deferring, so
`ErrWorkAckDeferred` fires less often under marker contention —
fewer deferral log lines, same signals. A skipped generation stays
pending/retrying and is visible in the existing queue-depth and
fence diagnostics until the next Ack or claim retires it.

## Runner enrollment

`projector_queue_ack_obsolete_marker_live_test.go` is a
`postgres_ci` row in `specs/live-tests.v1.yaml`, enrolled in the
live-postgres-readiness runner with both test names pinned in
`scripts/lib/live_postgres_readiness_results.py`. It reuses the
existing `ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN` proof database
(each test mints and drops its own schema), already wired into the
runner. Local wall time is about 8 s (race included).
