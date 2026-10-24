# Readiness shard weights refresh (#8930)

The weighted cut of `live-postgres-readiness` (#8971) shipped with weights fitted on two runs of the retired count cut. This page records the refresh from the first CI wave on the weighted cut. It is a refresh of measured inputs. It changes no cut code, gate, or cap.

## Source

| Run | Cut | Shard job seconds, shards 1 to 4 | Fixed seconds per job (job minus proofs step) |
| --- | --- | --- | --- |
| [40047410741](https://github.com/eshu-hq/eshu/actions/runs/40047410741) | count cut | 1716, 1327, 1212, 477 | 77, 64, 105, 83 |
| [40049588589](https://github.com/eshu-hq/eshu/actions/runs/40049588589) | count cut | 1768, 1398, 1437, 693 | 81, 77, 85, 85 |
| [40071537963](https://github.com/eshu-hq/eshu/actions/runs/40071537963) (head `fa21806288`, attempt 1) | weighted cut | 1242, 958, 1244, 1418 | 79, 106, 95, 92 |

All 12 shard jobs passed. The weighted-cut wave used the 581-test plan of its head. Its slices, rebuilt from the old weights, match the tests in each shard log exactly (91, 190, 61, and 239 tests). The mean fixed seconds over the 12 jobs is 85.75 s (it was 82 s on the first eight). The command was:

```bash
python3 scripts/lib/live_postgres_readiness_weights_refresh.py \
  --out specs/live-postgres-readiness-weights.v1.json --fixed-job-seconds 85.75 \
  --sample 40047410741=DIR_A --sample 40049588589=DIR_B --sample 40071537963=DIR_C
```

## What changed in the weights

| Field | Before | After |
| --- | --- | --- |
| `source_runs` | 2 runs | 3 runs (adds 40071537963) |
| `fixed_job_seconds` | 82.0 | 85.8 |
| `cold_cache_seconds` | 101.7 | 86.8 |
| `default_package_seconds` | 29.0 | 26.6 |
| `default_test_seconds` | 5.0 | 5.0 |
| `./internal/storage/postgres` package cost | 427.4 | 427.4 |
| tests with a measured weight | 567 | 581 |
| packages with a measured cost | 19 | 21 |

The 14 new weights are the two packages the first cut priced at the default: `./internal/query/supply/chain` (4 tests, 3.9 s package cost, `TestSupplyChainImpactRuntimeFilterPlansLive` at 51.4 s) and `./internal/storage/postgres/retention/work/items` (10 tests, 1.7 s package cost). No weight was dropped. Existing test weights moved by a mean over three runs instead of two. The largest moves are the contention proofs in `./internal/storage/postgres`: `TestProjectorClaimFullGuardContention` 221.1 to 201.0 s, `TestProjectorAckSupersedeConcurrentWithReplaysNeverDeadlockLive` 79.5 to 71.1 s, and `TestProjectorAckJournalContention` 45.5 to 40.0 s. The summed test weight went from 2839.7 to 2881.8 s. The package cost of `./internal/storage/postgres` stayed at 427.4 s because the wave added no later appearance of it in a shard.

One test is still unweighted: `./internal/storage/postgres|TestSweeperActivationSurvivesRetentionPrune`. It was enrolled after the wave's head, so no log has it. Unweighted planned tests fell from 15 to 1 and unweighted packages from 2 to 0 on the 582-test plan at `c3fa6ed1ec`. Both are inside the drift limits (40 tests, 2 packages).

## Prediction against measurement

Replayed on the wave's own plan and cut, the refreshed weights predict the wave as follows. This is in-sample: run 40071537963 is one of the three fitting runs, so the error is a floor, not a held-out figure.

| Shard | Predicted, old weights (s) | Predicted, refreshed weights (s) | Measured (s) | Refreshed error |
| --- | --- | --- | --- | --- |
| 1 | 1166.9 | 1176.3 | 1242 | -5.3% |
| 2 | 1273.6 | 1199.3 | 958 | +25.2% |
| 3 | 1343.6 | 1323.1 | 1244 | +6.4% |
| 4 | 1324.4 | 1286.2 | 1418 | -9.3% |

Shard 2 is the outlier. Its 190 proofs were all in `./internal/storage/postgres`. Their measured sum was 473.1 s against 599.3 s of weight, and the package compile took 378 s against 427.4 + 86.8 s modeled. The proofs ran faster in this run than their fitted weights, and the cause is NOT_PROVEN. This is one wave on one runner mix, so read the +25.2% as unexplained variance until a second wave shows whether it repeats. It is just outside the earlier -15% to +24% band.

## The new cut

On the 582-test plan at `c3fa6ed1ec`, the refreshed weights cut at test indexes 0, 91, 282, 342, and 582 (the old weights cut at 0, 91, 282, 343, and 582):

| | Shard 1 | Shard 2 | Shard 3 | Shard 4 | Worst | Mean | Largest deviation from mean |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Predicted, old weights (s) | 1166.9 | 1278.6 | 1343.6 | 1324.4 | 1343.6 | 1278.4 | 8.7% |
| Predicted, refreshed weights (s) | 1176.3 | 1204.3 | 1291.2 | 1318.1 | 1318.1 | 1247.5 | 5.7% |
| Measured on the wave (s) | 1242 | 958 | 1244 | 1418 | 1418 | 1215.5 | 21.2% |

The worst predicted shard is 1318.1 s, under the 1440 s budget (80% of 1800 s) and under the 15% spread bound. It still sits above the roughly 1296 s line the shards page names, so a slow runner at +39% could reach the cap. The measured spread of the wave (21.2%) is wider than the prediction because of shard 2. The 80% budget gate and the drift gates pass unchanged. A second wave on this refreshed cut is NOT_CHECKED.

## Commands

```bash
python3 scripts/test-live-postgres-readiness-shards.py
python3 -I -c "import sys; sys.path.insert(0,'scripts/lib'); import live_postgres_readiness_balance_cases as bc, live_postgres_readiness_weights as wt; plan=bc.live_plan(); w=wt.load_weights(); b=wt.weighted_bounds(plan,w); print(len(plan),b,wt.shard_job_seconds(plan,w,b))"
```

The second prints `582 [0, 91, 282, 342, 582] [1176.3, 1204.3, 1291.2, 1318.1]`.
