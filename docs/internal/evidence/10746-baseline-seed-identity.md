# NVD baseline: seed identity behind the stored position (#10746)

The independent review of #10835 found that a newer-seed chunk committing behind
the stored position wrote its facts but left the stored seed identity on the
older seed, because the sha and stamp CASEs applied only at an equal or greater
position. A later older-seed chunk at or past that position was then accepted,
which is the rollback the guard exists to refuse. In
`go/internal/storage/postgres/vulnerability/nvd_baseline_checkpoint.go` the
sha and stamp pair now also follows a strictly newer stamp at any position, and
the forward probe drops its position clause so the rotation report matches the
identity move. The position, last chunk identity, and record counters stay
position-gated. The lane has no production caller until the rest of #10746
lands.

## Performance Evidence

Performance Evidence: the statement count is unchanged. A stamped advance runs
one keyed no-op probe and one upsert, as before; the probe has four operands
instead of six and the two identity CASEs gain one `OR` arm over two integers.
Backend PostgreSQL 18.6 (a throwaway local instance), input a 100-row checkpoint
table (4 runs by 25 years, one page) with the measured row, year 2025, holding
an older seed at a stored position. `EXPLAIN (ANALYZE, BUFFERS)`, eighth run of
each statement inside a rolled-back transaction, base is `origin/main`
`ea7e3f91e9`:

| Newer seed | Statement | Base | After |
| --- | --- | --- | --- |
| ahead of the stored position | probe | 1 row, 3 buffers scanned, 0.048 ms | same plan, 0.055 ms |
| ahead of the stored position | upsert | primary key arbiter, 8 buffers, 0.054 ms | same plan, 0.040 ms |
| behind the stored position | probe | 0 rows, 3 buffers, 0.025 ms | 1 row, 9 buffers, 0.020 ms |
| behind the stored position | upsert | primary key arbiter, 8 buffers, 0.042 ms | same plan, 0.029 ms |

The conflict filter, the arbiter index, and the buffer count of the upsert are
identical. The times are inside the noise of a shared host, so no speedup or
slowdown is claimed. The one added piece of work is that the probe now also
matches a behind-position newer seed: one keyed no-op update, the same one the
old probe already did for an ahead-position rotation. That only happens when two
claims were planned from one checkpoint and the newer seed lands second.

## No-Regression Evidence

No-Regression Evidence: the conflict domain is one row, keyed
`(baseline_run_id, year)`, in `vulnerability_nvd_baseline_checkpoints`. Both
statements touch only that row, in the same order as before (probe, then
upsert), so no lock, index, or round trip is added and no lock-order edge
exists. The row lock the upsert waits on is released by the commit that holds
it, and the guard and the identity CASEs read the locked, latest row version.
`TestNVDBaselineCheckpointBehindPositionNewerSeedRacesOlderSeed` proves both
commit orders with two open transactions: when the older seed lands first the
newer seed waits on the row, then adopts its identity over the older position;
when the newer seed lands first the older seed waits, then is refused with a
seed rollback. `TestNVDBaselineCheckpointSeedIdentityMatrix` pins all 48 cells of
stored stamp (none or known), candidate stamp (none, older, equal, newer), sha
(same or other), and position (behind, behind inside one instant, equal, ahead),
checking the refusal, the rotation report, the identity pair, the position, and
that the chunk identity follows only a write at or past the stored position.
Six SQL mutants each fail a live test: position-only identity, a probe that
keeps its position clause, a split sha and stamp pair, a chunk identity that
follows the newer stamp, a misplaced parenthesis that lets an unstamped
candidate clear the identity, and a strict `>` on the position arm. Three of
them also fail the hermetic unit assertions.

## Observability Evidence

Observability Evidence: no new signal is needed, and the existing ones stay
accurate. `eshu_dp_nvd_baseline_seed_rotations_total{direction="forward"}` now
also counts a behind-position adoption, after the commit lands, because the
probe reports exactly the write that moves the stored identity. A refused older
seed after an adoption still counts `direction="rollback"`, logs WARN
`nvd baseline checkpoint refused a seed rollback` with failure class
`nvd_baseline_seed_rollback`, and lands on the claim failure row under that
class. The final positions differ between the two commit orders by design (a
refused generation moves nothing), so an operator reads the identity, not the
position, to see which seed the year is on.

## Not checked

- Behavior on a deployed host; no production code sets the baseline request
  fields yet.
- The live lane (`live-postgres-readiness`) does not gate merges; its result on
  the pushed head must be read directly.
