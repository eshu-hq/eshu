# Live PostgreSQL readiness shards

The `live-postgres-readiness` workflow runs the enrolled PostgreSQL proofs in
five parallel jobs, cut from `specs/live-postgres-readiness-weights.v2.json`
(present since #9240). The four-job v1 cut is the fallback, used only when the
v2 file is absent. Each job keeps the 30-minute cap (1800 s). This page
explains the cut and calibration.

## How the cut works

The trusted PR planner reads the frozen inventory on the default branch.
During the matrix helper rollout, that inventory can select
`live-postgres-readiness-shards` without naming the new
`readiness-plan` helper. The helper therefore runs on a PR when either
its own job ID or its dependent shard job ID is selected. The caller
still skips the workflow in reuse mode, and a smoke plan that excludes
the shards does not start the helper. The aggregate fails if a selected
plan or shard fails or skips.

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

`specs/live-postgres-readiness-weights.v1.json` holds the v1 fallback cost model,
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
every further row of that package. The model gate keeps each predicted job at
or below 1440 s, spread within 15%, and defaults within 40 tests and 2
packages. These modeled bounds do not guarantee an observed 1800 s wall time.

Each runner row is one `go test -json -count=1 -timeout=15m` invocation, and
the splitter admits a row by what that 900 s clock times. `go help testflag`
says the timeout panics "if a test binary runs longer than duration d", so
compile and link finish before the clock starts: a cold-cache shim with
`-timeout=3s` and a 2.4 s test took 10.2 s of wall time and passed. A row's
admission price is therefore its test prices plus the package repeat price
(the in-binary link, start, `TestMain`, and teardown residual), with no cold
or first-use cost; the job score charges those build costs once (#9348).
Before #9348 the splitter charged cold, first-use, and test prices to every
row, so a row could fail on compile time the clock never sees. The row budget
is 675 s, 75% of the clock. Measured over 17 green five-shard main runs, the
repeat residual has a median of 11.5 s (n=157) while a shard's first
`./internal/storage/postgres` row pays 336 to 655 s of build before its first
test. Rows predicted at 150 s or more ran at a measured/predicted test-time
ratio of p50 1.00, p95 1.16, p99 1.26, and max 1.33 (n=188). A 675 s row
prices about 663.6 s of tests plus the repeat residual (max measured 19.1 s).
The worst observed row ran 744.1 s of tests, about 763 s with the largest
residual, inside the clock. The joint worst case, the maximum ratio and the
maximum residual on one row, is about 901 s. It was not observed; if it
happens, the 900 s clock fails that shard loudly and the next refit adjusts the
prices. A single test priced above the budget cannot be split and fails the
gate.

## Gates

`python3 scripts/test-live-postgres-readiness-shards.py` runs in the hermetic
job. The budget, drift, seeded-overflow, and appended-tail cases
(`scripts/lib/live_postgres_readiness_weights_budget_cases.py`) load the
model the way the workflow's `matrix` step does: the committed default path,
v2 and five shards when the v2 file is present, v1 and four shards only when
it is absent. Their first case fails if the contract's model or shard count
differs from the `matrix` output or from the committed v2 `shard_count`. Before
#9100 these cases ran on the v1 four-shard cut while CI ran five shards.

The statements below about five jobs, five-shard cuts, and the "no shard
partition fits the job budget" error describe the v2 model. Three cases
assert v2-only results and skip, with a stated reason, when the v2 file is
absent and the loader returns v1: the drift-survives-a-rejected-plan case and
the appended-tail case here, and the planted 800 s lone-test case in
`scripts/lib/live_postgres_readiness_row_clock_cases.py`. They never skip while
the v2 file exists: a loader that returned v1 then fails them. Every other case
runs on both models.

The planted cases do not plant a fixed size on the live plan (#9371). A fixed
plant there measures the live plan's remaining headroom and its package lumps,
not the model. The old 10 x 20 s appended tail landed where no five-shard cut
meets both the 1440 s budget and the 15% spread: shard 5 holding one
`./internal/storage/postgres` row cost 1447.0 s under one correct refit and
1421.3 s under the committed weights. A PR that added eight live tests also
failed the old seeded case with "no shard partition fits the job budget",
while the live plan itself still fit. Each case now asserts what it is named for:

- The appended-tail case prices ten tail tests to fill the last shard's own
  headroom on the live cut. It then requires a clean budget and every job at
  or under 1440 s. A tail whose test seconds pass the five jobs' combined
  budget must still fail with "no shard partition fits the job budget".
- The seeded case runs on a fixed synthetic plan of 20 packages x 20 tests
  with round prices: 15 tests at 60 s seeded into the first package push the
  count split to 1930 s, past the 1800 s cap, and the weighted cut holds
  every job at or under 1440 s. The live count split and the live fit are
  still asserted, unseeded, on the live plan.
- The drift case adds 41 unpriced tail tests and requires the
  "planned tests have no weight (limit 40)" error. `drift_report` no longer
  raises when the budget rejects the cut: it prices repeat rows on the whole
  plan, which covers every cut's needs. A PR that both adds too many
  unpriced tests and breaks the budget now sees both errors.

The live plan can still fail the 15% spread at some sizes. Under the committed
weights, the budget gate fails once 45 to 60 default-priced tests are added to
the last package (the range swept). That failure is the live-plan fit case
reporting a real limit. The appended-tail case fails with it and names the same
spread error. The
v1 cut mechanics (fixed-plan balance, cut cache, malformed files, union
under changed weights) stay in `live_postgres_readiness_balance_cases.py`.
The suite fails when:

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
  weight in the active model, which is
  `specs/live-postgres-readiness-weights.v2.json` while that file is present
  (the drift error names the file). New tests run on the model's default
  prices, so a feature PR must not edit either weights file: the gate stays
  green while the defaulted count is at or under the cap, and review — not the
  gate — flags a weights edit inside a feature PR. Only a standalone refit PR
  (see below) touches the weights files;
- a v2 runner row's clock-timed price (tests plus the package repeat price)
  passes 675 s, 75% of the 900 s `go test -timeout`. A splittable row is cut
  at its weight midpoint until it fits, so only a single test priced above
  675 s trips this. The clock and the 675 s budget are pinned in
  `scripts/lib/live_postgres_readiness_row_clock_cases.py`;
- the shard job's `timeout-minutes` stops matching the 1800 s cap the gate
  uses;
- a weights file is malformed, has duplicate keys, or a non-finite or negative
  number;
- a shard report cut under other weights passes the aggregate.

## Move to five shards

The move landed in #9240: `specs/live-postgres-readiness-weights.v2.json` is
present, so the default matrix is `1/5` through `5/5`. The plan job computes it
from the same checkout used by the runner and aggregate. Without the v2 file
the matrix falls back to `1/4` through `4/4`.
A malformed present v2 file fails the plan and the selected aggregate; it never
falls back to v1. A present valid v2 file changes all five check names to `/5`
and requires five matching reports. The selected aggregate fails if the plan,
a shard, or report selection fails.

To produce a v2 file (the first one, or a refit), collect at least two
complete, successful `main` workflow runs with the same inventory, profile,
image, toolchain, and proof boundaries. Save every shard job's metadata and log.
Use the v2 refresher with both runs and an output outside the repository:

```bash
python3 scripts/lib/live_postgres_readiness_weights_refresh.py \
  --schema-version 2 --out /tmp/readiness-weights.v2.json \
  --sample RUN_ID_1=RUN_DIR_1 --sample RUN_ID_2=RUN_DIR_2
```

The v2 output sorts top-level keys and places two sorted table entries per
line. Re-run the command and compare the proposed file byte-for-byte before
committing it. The v1 output retains its one-entry-per-line layout.

Review the source run and job IDs, successful terminal states, complete rows,
first-use/repeat identification, predicted row and job maxima, defaults, and
exact ordered test coverage. Then add only the sorted
`specs/live-postgres-readiness-weights.v2.json` file in a standalone PR. Its
five-way CI run is the first observed wall-time proof; diagnose a timeout or
spread failure before merging. The five-leg model uses one more SQL runner and may consume more aggregate
runner seconds; the plan job also adds setup time. No speedup or fixed
wall-time result is promised.

## Refit the weights

A refit is a standalone bot-or-owner PR that touches only the weights files.
While `specs/live-postgres-readiness-weights.v2.json` is present, the budget
and drift gates run on it, so a refit must regenerate it with the v2
refresher (see [Move to five shards](#move-to-five-shards)); a v1-only refit
cannot clear a tripped gate. Regenerating
`specs/live-postgres-readiness-weights.v1.json` in the same PR keeps the
fallback current; no gate checks the v1 file against the live plan while v2
is present. A refit is built from green `main` shard logs — never from a
feature branch, and never bundled with a feature change — so it cannot
conflict with feature work, because no feature PR touches the files. Sorted
entries keep the diff a local hunk instead of a whole-file rewrite.

Trigger a refit when the drift count passes 20 defaulted tests (half the
40-test gate cap), when the gate fails, when a shard job runs near its cap, or
on a regular schedule. Either trigger shape is acceptable; the choice is the
reviewer's call on the refit PR.

For the v1 fallback file, take two or more green full runs of the workflow on
`main`, download each shard job's log, put each run's shard logs in its own
directory, and run:

```bash
gh api repos/eshu-hq/eshu/actions/jobs/<JOB_ID>/logs > <RUN_DIR>/shard<N>.log   # per shard job
python3 scripts/lib/live_postgres_readiness_weights_refresh.py \
  --out specs/live-postgres-readiness-weights.v1.json \
  --fixed-job-seconds 82 \
  --sample <RUN_ID_1>=<RUN_DIR_1> --sample <RUN_ID_2>=<RUN_DIR_2>
```

`--fixed-job-seconds` is the mean of the job duration minus the "Run live
readiness proofs" step duration (v1 only; the v2 refresher infers it from the
source jobs). Read both from
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
GitHub Actions logs, so refit before they expire. Use only runs whose shard
jobs all passed, since a failed shard log misses its package groups
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

## Race detector

The shards run without `-race`: the runner invokes `go test` with no `-race` flag
and the workflow sets no `GOFLAGS`. Concurrency proofs are race-checked
locally only — the pre-push changed-package race lane, the `pre-pr-full`
whole-module race, and author-run `-race` — never by the required gate.

That is a measured decision (#9026), not an oversight. A local probe of
three live proofs (two concurrency plus one guard) showed `-race` slowing
test time 2.55x to 4.85x; even the smallest multiplier pushes every shard
past the 1800 s cap (the tightest no-race shard samples leave only 198 s of
headroom), and enlarged
rows would breach the 900 s per-invocation `go test` timeout. Enabling
`-race` later needs a re-split (more shards or a dedicated race lane) plus a
weights refit from `-race` measurements.
