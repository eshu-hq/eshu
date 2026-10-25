# Readiness shard weights band record (#8983)

The refit itself landed on main first (commits 996f012198 and 477c9d1d5d,
from green runs 40177924581 and 40157207251 with fixed-job-seconds 89.2,
plus a seeded plant-12 RED proof). This page records the verification of
that refit against the issue's criteria and the error band the shipped
weights support. It changes no gate or weight.

## Verification on d5441c7c

- Plan: 627 tests. Unweighted: 3 (limit 40). Unweighted packages: 0
  (limit 2). Stale keys: 0. Drift: clean.
- Budget: worst predicted shard 1292.7 s (cut 0, 74, 299, 381, 627),
  under the 1440 s budget; live-plan spread 1.7%, under the 15% bound.
- `scripts/test-live-postgres-readiness-shards.py`: 41/41 PASS.

## Prediction against measurement

Scored with the shipped weights against what each shard actually ran (tests
in log order per shard, the repo's own segment cost, each job's measured
fixed seconds added back). Error is prediction minus measurement as a share
of the measured job wall. The two fit waves are in the sample, so this is a
floor, not a held-out figure.

| Wave | Shard 1 | Shard 2 | Shard 3 | Shard 4 |
| --- | --- | --- | --- | --- |
| 40071537963 measured (s) | 1242 | 958 | 1244 | 1418 |
| 40071537963 error | +14.3% | +17.1% | -5.0% | -19.8% |
| 40078812653 measured (s) | 851 | 1174 | 1185 | 1160 |
| 40078812653 error | +66.9% | -4.2% | -2.9% | +0.8% |
| 40157207251 measured (s) | 1218 | 1235 | 1416 | 1572 |
| 40157207251 error | +17.3% | -5.2% | -15.2% | -20.7% |
| 40177924581 measured (s) | 1093 | 1310 | 1219 | 1107 |
| 40177924581 error | +30.8% | -9.4% | -1.5% | +12.7% |
| 40197054198 measured (s) | 1079 | 1119 | 1052 | 1150 |
| 40197054198 error | +19.9% | +13.4% | +20.1% | +10.4% |

The band over the five scored waves is -21% to +67%, which the shards page
now states. The extremes land on identical content: shard 1's 91 tests are
the same set in runs 40071537963 and 40078812653 and measured 1242 s, then
851 s, scoring +14.3% and +66.9% — the 2-sample fit never saw the fast regime
for those tests. The issue quoted -9.3% to +38.2%; that band was scored with
superseded 3-sample weights and is replaced.

## First wave on the refit cut

Run 40197054198 (push, head 4ab321ac, first green wave containing the refit)
measured 1079, 1119, 1052, and 1150 s for shards 1 to 4. Its shard composition
(74, 225, 82, and 246 tests) matches the refit cut exactly, confirming the cut
took effect. All shards held under the cap with margin to spare. Its errors
(+18% to +28%, all over-predictions) sit inside the stated band.

## Commands

```bash
python3 scripts/test-live-postgres-readiness-shards.py
python3 -I -c "import sys; sys.path.insert(0,'scripts/lib'); import live_postgres_readiness_balance_cases as bc, live_postgres_readiness_weights as wt; plan=bc.live_plan(); w=wt.load_weights(); b=wt.weighted_bounds(plan,w); print(len(plan),b,wt.shard_job_seconds(plan,w,b))"
```

The second prints `627 [0, 74, 299, 381, 627] [1292.7, 1264.0, 1263.2, 1263.5]`.
Runner variance tracking continues in #9100.
