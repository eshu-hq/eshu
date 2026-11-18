# #10901 sustained autovacuum soak: evidence note

Lane: `drive/10901-autovacuum-soak`. Closes acceptance box 4
(`goal-ops/ruling-10901-autovacuum-soak.md`, amended by
`goal-ops/ruling-10901-autovacuum-soak-r2.md`): sustained production-shape
writer waves + in-tick collapse must keep staging depth bounded at steady
state with the daemon doing the reclaiming.

## Harness

Test: `TestSupplyChainImpactWinnersStagingAutovacuumSoakLive`
(`go/internal/storage/postgres/supply_chain_impact_winners_staging_autovacuum_soak{,_support}_live_test.go`).
Manual landing-gate harness: deliberately NOT enrolled in
live-postgres-readiness (no `//eshu:readiness` marker), so shared CI
runners never schedule it.

Shape: alternating 2/4-writer production transactions (conflict-domain
lock + BatchInsertVersionedFacts + production capture + owed marker +
replace-set retract with fact_retract staging, 8 passes of 32 hot + 8 tail
keys), collapse-in-tick loop concurrent, bounded final drain per wave,
depth 0 asserted per wave. Waves paced with G = 2s sleep (ruling v2;
the p50 inter-wave clause is struck — drain-watch snapshots lack tick
granularity). Duration: max(5min, 4 observed naptime intervals).

Pre-registered bounds (frozen before measuring, never re-tuned):
in-load second-half heap max <= 128MB, series max dead <= 500k,
in-load vac delta >= 1; recovery (writers stopped, quiesce <= 120s):
depth == 0, dead <= 1000, vac advance >= 1 (total >= 2), heap <=
load-end high-water mark + max(2MB, 5%) one-sided; conservation exact;
zero 40P01; load1 < 8.0 start/end/500ms max; control canary 4-writer
p95 <= 150ms. Load or canary breach VOIDs the round; a bound breach
KILLs it. No VACUUM FULL / CLUSTER anywhere.

Backend: disposable PostgreSQL 18 on localhost (fresh database per run,
`ApplyBootstrap` over all migrations), host owned 16-CPU, quiet per the
load evidence below.

## RED run (vacuum off, ESHU_SOAK_VACUUM_OFF=1)

714 waves / 1,310,383 marks in 5m0s, conservation exact
(version growth = marks; depth growth = distinct keys), canary
49.5ms, load max 2.61, zero 40P01. Bounds bite as designed: heap
219MB (bound 128MB), dead 1.32M (bound 500k), vac_count 0 with no
last_autovacuum advance. "RED proven" logged. A RED that passes the
bounds would mean they are too loose.

## GREEN run (vacuum on)

140 waves / 259,482 marks in 5m0s, conservation exact (version growth
259,482; depth growth 3,480 distinct keys), canary 52.0ms, load
0.12 start / 0.48 end / 0.48 max, zero 40P01. In-load: heap second-half
max 9.7MB (bound 128MB), dead max 51,972 (bound 500k), vac_count 0 -> 5.
Recovery (15s): depth 0, dead 44,122 -> 0, vac_count 5 -> 6 with
last_autovacuum advancing, heap 9.5MB -> 2.4MB (within load-end
high-water mark, no growth). All bounds PASS, zero KILL/VOID.

## Verdict

Box 4 CLOSED by this evidence: the threshold-1000 tuning holds staging
bounded across sustained paced waves, and the daemon — not the test —
does the reclaiming (RED fails the bounds with it off, GREEN passes
with it on).
