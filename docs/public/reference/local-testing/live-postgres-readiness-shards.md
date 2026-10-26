# Live PostgreSQL readiness shards

The `live-postgres-readiness` workflow runs the enrolled PostgreSQL proofs in
four parallel jobs while only the v1 weights file is present. A later standalone
v2 weights file activates five jobs through the same workflow. Each job keeps
the 30-minute cap (1800 s). This page explains the cut and calibration.

## How the cut works

The plan is the flat `package|test` list from
`live_postgres_readiness_results.py list-packages`, in canonical inventory
order. `scripts/lib/live_postgres_readiness_weights.py` reads the committed model.
With v1, it cuts the plan into four contiguous slices using the existing
longest-leg objective. With v2, it cuts into five contiguous slices using
measured first-use and repeat-row costs and the guard-constrained objective. Every proof is in exactly one slice. A new test
enters the plan at its inventory position and the cut moves with it, so no one
edits a pinned table.

The runner (`plan` and `report`) and the aggregate (`verify` and `select`) all
call the same cut function with the same weights file from the same checkout.
The aggregate requires exactly the active model's four or five reports to
be the exact union of the plan. A report cut under different weights fails with `plan mismatch`.

## What the prediction counts

`specs/live-postgres-readiness-weights.v1.json` holds the current fitted cost model,
in tenths of a second so the cut is identical on every machine:

| Field | Meaning |
| --- | --- |
| `test_seconds` | Measured elapsed seconds per `package\|test`, averaged over the source runs. |
| `package_seconds` | Compile and link cost paid each time a package appears in a shard. `./internal/storage/postgres` costs about 427 s per appearance, so it should span at most three shards. |
| `cold_cache_seconds` | Extra build-cache cost paid by the first package of each shard. |
| `fixed_job_seconds` | Container start, checkout, and toolchain setup per job. |
| `default_test_seconds`, `default_package_seconds` | Used for a test or package with no measured entry (the mean of the measured values). |
| `source_runs` | The CI run ids the numbers came from. |

Predicted v1 job seconds are the sum of its tests, package costs, cold-cache
constant, and fixed job overhead. A v2 model distinguishes the first use of a
package from each later runner row. Its job score adds fixed and cold costs,
every test price, one first-use price per package, and one repeat price for
every further row of that package. For the 720 s row admission budget, the
splitter charges cold cost, test prices, and the greater of first-use and
repeat cost to every row. This bounds either row kind even when repeat cost
exceeds first-use cost; the job score still charges the measured price for
each actual row kind. The model gate keeps each predicted job at or below 1440 s,
spread within 15%, and defaults within 40 tests and 2 packages. These modeled
bounds do not guarantee an observed 1800 s wall time.

## Gates

`python3 scripts/test-live-postgres-readiness-shards.py` runs in the hermetic
job and fails when:

- any shard's predicted job seconds pass 80% of the 1800 s cap (1440 s). Adding
  tests without refreshing the weights is caught here once the defaults stop
  covering them. The 20% headroom is the model's error band. Replayed with the
  shipped weights against what each shard actually ran, five weighted-cut
  waves miss by -21% to +67% (prediction minus measurement, as a share of the
  measurement): run 40071537963 by +14.3%, +17.1%, -5.0%, and -19.8%; run
  40078812653 by +66.9%, -4.2%, -2.9%, and +0.8%; run 40157207251 by +17.3%,
  -5.2%, -15.2%, and -20.7%; run 40177924581 by +30.8%, -9.4%, -1.5%, and
  +12.7%; and the first wave on the refit cut, run 40197054198, by +19.9%,
  +13.4%, +20.1%, and +10.4% (shards 1 to 4). The two fit waves are in the
  sample, so treat the
  band as a floor. The extremes land on identical content: shard 1's 91 tests
  score +14.3% on one wave and +66.9% on another (1242 s, then 851 s
  measured), so the band is runner variance the fit never saw, not model bias.
  At the -21% end a 1440 s prediction runs about 1740 s, still under the cap.
  Runner speed adds noise on top. On one layout
  and plan, shard 1 took 1129 s in run 40057201268 and 1568 s in run
  40057373740 (+39%), and its summed test seconds rose from 598 to 841 (+41%),
  which fits a slower runner. At +39% a shard predicted above about 1296 s
  crosses the 1800 s cap, so the 80% rule (1.25x) does not cover that spread by
  itself. The cut's worst predicted shard can sit above that line, so read the
  first CI wave on the new cut, and refresh the weights from it, before
  relaxing anything. The weighted-cut waves measured (shards 1 to 4)
  1242, 958, 1244, and 1418 s, then 851, 1174, 1185, and 1160 s, then 1218,
  1235, 1416, and 1572 s, then 1093, 1310, 1219, and 1107 s, so every shard
  stayed under the cap. The first wave on the refit cut (run 40197054198)
  measured 1079, 1119, 1052, and 1150 s, also under the cap. The 20%
  headroom, not the fit, is what keeps a slow runner under the cap;
- the live plan's shard spread passes 15% of the mean (the acceptance bound of
  #8930), or the cut on a fixed synthetic plan passes 1%. The fixed plan guards
  the cut itself and does not change when tests are enrolled, so an unrelated
  enrollment cannot trip it. If the live-plan bound trips, open a refit;
- more than 40 planned tests, or more than 2 planned packages, have no measured
  weight. New tests run on `default_test_seconds` / `default_package_seconds`,
  so a feature PR must not edit `specs/live-postgres-readiness-weights.v1.json`:
  the gate stays green while the defaulted count is at or under the cap, and
  review — not the gate — flags a weights edit inside a feature PR. Only a
  standalone refit PR (see below) touches the file;
- the shard job's `timeout-minutes` stops matching the 1800 s cap the gate
  uses;
- a weights file is malformed, has duplicate keys, or a non-finite or negative
  number;
- a shard report cut under other weights passes the aggregate.

## Move to five shards

This infrastructure change keeps `specs/live-postgres-readiness-weights.v2.json`
absent. The default matrix therefore remains exactly `1/4` through `4/4`; the
plan job computes it from the same checkout used by the runner and aggregate.
A malformed present v2 file fails the plan and the selected aggregate; it never
falls back to v1. A present valid v2 file changes all five check names to `/5`
and requires five matching reports. The selected aggregate fails if the plan,
a shard, or report selection fails.

After the final Phase A code and this infrastructure merge, collect at least two
complete, successful `main` workflow runs with the same inventory, profile,
image, toolchain, and proof boundaries. Save every shard job's metadata and log.
Use the v2 refresher with both runs and an output outside the repository:

```bash
python3 scripts/lib/live_postgres_readiness_weights_refresh.py \
  --schema-version 2 --out /tmp/readiness-weights.v2.json \
  --sample RUN_ID_1=RUN_DIR_1 --sample RUN_ID_2=RUN_DIR_2
```

Review the source run and job IDs, successful terminal states, complete rows,
first-use/repeat identification, predicted row and job maxima, defaults, and
exact ordered test coverage. Then add only the sorted
`specs/live-postgres-readiness-weights.v2.json` file in a standalone PR. Its
five-way CI run is the first observed wall-time proof; diagnose a timeout or
spread failure before merging. The five-leg model uses one more SQL runner and may consume more aggregate
runner seconds; the plan job also adds setup time. No speedup or fixed
wall-time result is promised.

## Refit the v1 weights

A refit is a standalone bot-or-owner PR that touches only
`specs/live-postgres-readiness-weights.v1.json`. It is built from green `main`
shard logs — never from a feature branch, and never bundled with a feature
change — so it cannot conflict with feature work, because no feature PR
touches the file. One entry per line keeps the diff a local hunk instead of a
whole-file rewrite.

Trigger a refit when the drift count passes 20 defaulted tests (half the
40-test gate cap), when the gate fails, when a shard job runs near its cap, or
on a regular schedule. Either trigger shape is acceptable; the choice is the
reviewer's call on the refit PR.

Take two or more green full runs of the workflow on `main`, download each
shard job's log, put each run's four logs in its own directory, and run:

```bash
gh api repos/eshu-hq/eshu/actions/jobs/<JOB_ID>/logs > <RUN_DIR>/shard<N>.log   # per shard job
python3 scripts/lib/live_postgres_readiness_weights_refresh.py \
  --out specs/live-postgres-readiness-weights.v1.json \
  --fixed-job-seconds 82 \
  --sample <RUN_ID_1>=<RUN_DIR_1> --sample <RUN_ID_2>=<RUN_DIR_2>
```

`--fixed-job-seconds` is the mean of the job duration minus the "Run live
readiness proofs" step duration. Read both from
`gh api repos/eshu-hq/eshu/actions/runs/<RUN_ID>/jobs`: the job's
`completed_at - started_at`, and the matching entry under `steps`. The refresher keeps only tests in the current
inventory, so a test missing from every log uses the default until the next
refit. Commit only the regenerated weights file in the refit PR, then run
the shard contract test again on it.

Keep the earlier runs as extra `--sample` arguments, each in its own directory,
and pass `--fixed-job-seconds` as the mean over every shard job of every
sample. The refresh after the first wave on the weighted cut used runs
40047410741, 40049588589, and 40071537963 (12 shard jobs, mean 85.75 s). The
shard job logs of a run stay downloadable for the retention window of
GitHub Actions logs, so refit before they expire. Use only runs whose four
shard jobs all passed, since a failed shard log misses its package groups
and the refresher fails closed.

### A feature PR that trips the drift gate

The hermetic job fails the drift check, and the shard jobs do not wait on the
hermetic job: they run with `fail-fast: false`, so they still finish on the PR
head with default prices for the new tests. Do not commit a regenerated
weights file to the feature PR — review flags that edit, and the gate cannot
tell who made it. Instead, land the tests first in batches that keep the total
defaulted count at or under 40, then open the standalone refit above from
green `main` logs. The feature run's shard logs still prove the new tests
pass; they just cannot feed the refit, because the refit reads only green
`main` runs.

## When a shard is still too slow

If the gate fails after a refresh, the plan no longer fits in the active shard count at
80% of the cap. Reduce the cost instead of raising the cap: split the slowest
package's proofs, cache the Go build for the storage package, or add a shard.
Raising `timeout-minutes` only hides a hang.
