# #10901 quiet-host absolute re-prove: evidence note

Lane: `drive/10901-quiet-absolute` in
`/home/ubuntu/personal-repos/eshu-10901-absolute`. Adjudication condition 3
(`goal-ops/ruling-10837-adjudication.md`): landing needs a quiet-host
absolute re-prove (owned host, load1 < half the CPUs, control canary per
round, pre-registered bound, n >> 60) as a hard `t.Errorf` kill gate.

## Harness

Test: `TestSupplyChainImpactWinnersQuietAbsoluteReproveLive`
(`go/internal/storage/postgres/supply_chain_impact_winners_quiet_absolute_reprove_live_test.go`).
Committed shim shape, 4-writer stratum primary: shared 162 hot keys (+18
private tails), staging plain-INSERT cell vs frozen contended-upsert cell,
plus GROUP BY collapse over a 162x150 staged wave amortized over the 150
statements of one wave. Every gated cell at n=200 (4 writers x 50 iters;
nearest-rank p95 of 60 is the fragile 4th order statistic, of 200 the
11th). Conservation asserted (24,300 marks, 162 groups; distinct wave
24,300 groups). Manual landing-gate harness: deliberately NOT enrolled in
live-postgres-readiness (no `//eshu:readiness` marker), so shared CI
runners never schedule it.

Pre-registered bounds (fixed in the harness before measuring, never
re-tuned): effective p95 <= 3.41ms (`hotkeyShimStagingEnvelope`);
staging/upsert ratio <= 0.80; control canary contended-upsert 4-writer p95
<= 150ms (2x the 72.5ms shared-host shim measurement: a quiet host must
beat the noisy-host number, a breach voids the round); load1 < 8.0 (half
of 16 CPUs) at start, end, and 500ms-sampled in-run maximum. Load or
canary breach VOIDs the round (no verdict numbers published); envelope or
ratio breach KILLs via `t.Errorf`.

Backend: disposable PostgreSQL 18.6 on localhost (local cluster, database
created from `template0` per proof, `ApplyBootstrap` over all 337
migrations), host owned and quiet per the load evidence below.

## Prior estimator spread (context, not gates)

Coordinator rerun effective p95 0.362ms; shim-run effectives
0.45/1.93/2.14/3.41ms. The re-prove lands at 0.356-0.361ms, reproducing
the 0.362ms coordinator point.

## Round 1: PASS (all gates)

- load1 start 1.40, end 1.37, in-run max 1.40 (ceil 8.0).
- canary upsert4 n=200 p95 48.701827ms <= 150ms PASS.
- staging4 p95 322.834us, ratio 0.007 <= 0.80 PASS.
- collapse p95 4.907257ms, amortized 32.715us,
  effective 355.549us (0.000356s) <= 3.41ms PASS.
- worst-partition (distinct) collapse p95 31.097954ms (context note).
- `--- PASS: TestSupplyChainImpactWinnersQuietAbsoluteReproveLive (9.86s)`.

## Round 2: PASS (all gates)

- load1 start 1.38, end 1.32, in-run max 1.38 (ceil 8.0).
- canary upsert4 n=200 p95 49.289883ms <= 150ms PASS.
- staging4 p95 328.012us, ratio 0.007 <= 0.80 PASS.
- collapse p95 4.884796ms, amortized 32.565us,
  effective 360.577us (0.000361s) <= 3.41ms PASS.
- worst-partition (distinct) collapse p95 31.161267ms (context note).
- `--- PASS: TestSupplyChainImpactWinnersQuietAbsoluteReproveLive (9.89s)`.

## Verdict

Two full rounds passed every gate on a quiet host. Adjudication condition
3 satisfied: the absolute arm holds under the pre-registered 3.41ms
envelope with the collapse cost included.
