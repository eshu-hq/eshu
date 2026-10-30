# Live PostgreSQL readiness shards

The `live-postgres-readiness` workflow runs the enrolled PostgreSQL proofs in
six parallel jobs, cut from `specs/live-postgres-readiness-weights.v2.json`
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
longest-leg objective. With v2, it cuts into six contiguous slices using
measured first-use and repeat-row costs and the guard-constrained objective. Every proof is in exactly one slice. A new test
enters the plan at its inventory position and the cut moves with it, so no one
edits a pinned table.

The runner (`plan` and `report`) and the aggregate (`verify` and `select`) all
call the same cut function with the same weights file from the same checkout.
The aggregate requires exactly the active model's four or six reports to
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
is the clock divided by the largest measured/predicted test-time ratio, on rows
predicted at 150 s or more. #9348 measured 17 green five-shard main runs on the
weights before the #9436 refit: the repeat residual had a median of 11.5 s
(n=157), a shard's first `./internal/storage/postgres` row paid 336 to 655 s of
build before its first test, and the ratio was p50 1.00, p95 1.16, p99 1.26,
and max 1.33 (n=188). That set the budget to 675 s, 75% of the clock. #9469
re-measured the ratio on the refit weights with the same method:

| Runs (refit weights) | Rows | p50 | p95 | p99 | Max |
| --- | --- | --- | --- | --- | --- |
| Refit pair 40672571941 and 40687668044 (in-sample) | 16 | 1.00 | 1.23 | 1.28 | 1.29 |
| 10 green main runs, 40753558584 to 40818333400 | 61 | 1.06 | 1.43 | 1.46 | 1.48 |

The worst row, 13 `./internal/query/admin` tests in shard 1, was predicted at
196.1 s and ran 289.6 s. The row budget is now 608 s: 900 / 1.48, rounded down.
A 608 s row prices about 593.6 s of tests plus the 14.4 s repeat price. The
joint worst case, the maximum ratio and the largest residual #9348 measured
(19.1 s) on one row, is about 898 s. If a row still overruns, the 900 s clock
fails that shard loudly and the next refit adjusts the prices. A single test
priced above the budget cannot be split and fails the gate.

## Gates

`python3 scripts/test-live-postgres-readiness-shards.py` runs in the hermetic
job. The budget, drift, seeded-overflow, and appended-tail cases
(`scripts/lib/live_postgres_readiness_weights_budget_cases.py`) load the
model the way the workflow's `matrix` step does: the committed default path,
v2 and six shards when the v2 file is present, v1 and four shards only when
it is absent. Their first case fails if the contract's model or shard count
differs from the `matrix` output or from the committed v2 `shard_count`. Before
#9100 these cases ran on the v1 four-shard cut while CI ran five shards.

The statements below about six jobs, six-shard cuts, and the "no shard
partition fits the job budget" error describe the v2 model. Three cases
assert v2-only results and skip, with a stated reason, when the v2 file is
absent and the loader returns v1: the drift-survives-a-rejected-plan case and
the appended-tail case here, and the planted 800 s lone-test case in
`scripts/lib/live_postgres_readiness_row_clock_cases.py`. They never skip while
the v2 file exists: a loader that returned v1 then fails them. Every other case
runs on both models.

The planted cases do not plant a fixed size on the live plan (#9371). A fixed
plant there measures the live plan's remaining headroom and its package lumps,
not the model. When #9371 landed, the old 10 x 20 s appended tail sat in a
window where, under a refit candidate that was later rejected, no five-shard
cut met both the 1440 s budget and the 15% spread: shard 5 holding one
`./internal/storage/postgres` row, the last storage test through the end of
the plan, cost 1447.0 s there, over the budget. Under the weights committed
then, the same row cost 1421.3 s and fit, so that tail passed. Every weight
set has such a window, at its own sizes
([The live-plan spread window](#the-live-plan-spread-window)).
A PR that added eight live tests also
failed the old seeded case with "no shard partition fits the job budget",
while the live plan itself still fit. Each case now asserts what it is named for:

- The appended-tail case prices ten tail tests to fill the last shard's own
  headroom on the live cut. It then requires a clean budget and every job at
  or under 1440 s. A tail whose test seconds pass the six jobs' combined
  budget must still fail with "no shard partition fits the job budget".
- The seeded case runs on a fixed synthetic plan of 20 packages x 20 tests
  with round prices: 20 tests at 60 s seeded into the first package push the
  six-way count split to 2030 s, past the 1800 s cap, and the weighted cut holds
  every job at or under 1440 s. The live count split and the live fit are
  still asserted, unseeded, on the live plan.
- The drift case adds 41 unpriced tail tests and requires the
  "planned tests have no weight (limit 40)" error. `drift_report` no longer
  raises when the budget rejects the cut: it prices repeat rows on the whole
  plan, which covers every cut's needs. A PR that both adds too many
  unpriced tests and breaks the budget now sees both errors.

The live plan can still fail the 15% spread at some sizes. The live-plan fit
case reports it. When the live plan is inside the window, both the live-fit
case and the appended-tail case fail with the same spread error.
[The live-plan spread window](#the-live-plan-spread-window)
gives the sizes, the cause, and what to do. The v1 cut mechanics (fixed-plan balance, cut cache, malformed files, union
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
  passes 608 s, the 900 s `go test -timeout` over the measured 1.48 tail. A
  splittable row is cut at its weight midpoint until it fits, so only a single
  test priced above 608 s trips this. The clock and the 608 s budget are pinned in
  `scripts/lib/live_postgres_readiness_row_clock_cases.py`;
- the shard job's `timeout-minutes` stops matching the 1800 s cap the gate
  uses;
- a weights file is malformed, has duplicate keys, or a non-finite or negative
  number;
- a shard report cut under other weights passes the aggregate.

## The live-plan spread window

A PR that only appends tests can trip the live-plan fit case with
`v2 predicted job spread exceeds 15% of mean`. The 1440 s budget, the 15%
spread, and the 608 s row budget are unchanged by this section. The failure
comes from the plan's package lumps, and it shows up as a window of plan sizes:
the case passes, fails for a range of appended tests, then passes again.

**Where the window is.** The snippet below counts default-priced tests
appended to the last package and prints the sizes the live-plan fit case
rejects. Every enrollment and every refit moves the window, so run it rather
than trusting a figure on this page. At `9ffd76a581` (2026-10-29), on the
pooled refit (#9519: p75 prices from nine main runs, 40867705415 through
40900152112), main plans 749 tests, 10 of them on default prices. With the
608 s row budget (#9469) the fit case accepts 42 appended tests, fails from
43 through 51 and from 61 through 69, and passes from 52 through 60 and from
70.
At `d363d77b85` (the #9436 weights, 738 tests) it failed from 33 through 71.

Run this
from the repository root to print the window for the checkout you are on. It
appends `k` default-priced tests to the last package, the way the appended-tail
helpers do, and asks the same `budget_errors` the live-plan fit case asks. On
your branch it counts from your PR's plan, so it gives your own headroom. To
price a candidate file, pass `Path(...)` to `wt.load_weights`. It takes about
half a minute.

```bash
python3 -I -B - <<'EOF'
import sys
sys.path.insert(0, "scripts/lib")
import live_postgres_readiness_weights as wt
from live_postgres_readiness_balance_cases import live_plan, slow_runs

weights, plan = wt.load_weights(), live_plan()
package = plan[-1].split("|")[0]
failing = [k for k in range(121) if wt.budget_errors(
    [*plan, *slow_runs(k, "TestGrow", package)], weights)]
print(f"{len(plan)} tests;",
      f"fails k={failing[0]}..{failing[-1]}" if failing else "no failing k in 0..120")
EOF
```

On that plan with the 608 s row budget it prints `749 tests; fails k=43..69`,
so main takes 42 appended tests before the first failure. The line prints only
the first and last failing `k`; on that plan `k` from 52 through 60 also
passes.

**Why the window exists.** This explanation was not re-derived on the
six-shard cut; its figures are from the five-shard cut at `d363d77b85`. While shard 5 can hold at least one
`./internal/storage/postgres` test (411 tests on main at `d363d77b85`), the
fourth cut sits inside that package, and shard 5 pays the package's first-use
price a second time. As the appended tail grows, shard 5 reaches the 1440 s
budget, so the cut must move to the package boundary. At `d363d77b85` that
drops 537.4 s: the second first-use charge (`package_first_seconds` for
`./internal/storage/postgres` in the v2 file, 536.0 s) plus the one test that
leaves the shard. Shard 5 then falls below 85% of the mean job. It cannot grow back: any earlier fourth cut re-adds the
first-use charge and breaks the budget. The window closes when the appended
tail alone lifts shard 5 over the 85% floor.

**Whether a valid cut exists.** An exhaustive search of every contiguous
five-shard cut, run before the #9436 refit, found that the planner accepted
exactly the plans that had a valid cut, except at the edge where a window is
about to close: there the planner can raise on a cut that another cut would
fix (#9390). The method and figures are in
`docs/internal/evidence/9390-five-shard-cut-enumeration.md`. Until #9390
lands, a spread raise at the edge of a window is not proof that no cut exists.

**What to do, in order.**

1. Keep the PR's appended tail under the headroom above, and batch the
   enrollment across PRs when it is larger. Run the snippet on your branch to
   see where you stand. The drift gate also caps the defaulted tests at 40, so
   the usable headroom is the lower of the fit window and 40 minus the tests
   the plan already prices at defaults. At `9ffd76a581` on the pooled refit
   that is 30 (10 of 40 defaulted). Print the count with:

   ```bash
   python3 -I -B -c 'import sys; sys.path.insert(0, "scripts/lib")
   import live_postgres_readiness_weights as wt
   from live_postgres_readiness_balance_cases import live_plan
   print(len(wt.drift_report(live_plan(), wt.load_weights()).defaulted_tests))'
   ```
2. Land the refit if one is due. A refit moves the window; it does not remove
   it, so check the new headroom with the snippet.
3. If your plan fails at its enrolled size, file a model-change issue. Attach
   the raise message and the plan size (the number of planned tests). A
   maintainer runs the exact search for your plan; no committed tool produces
   it yet. Do not change the budget, the spread, or the weights in a feature
   PR.

## Move to six shards

The v2 model landed in #9240 with five shards; #9460 moved it to six when the
five-leg cut saturated at about 1270 s predicted mean and shard 1 started
hitting the 30-minute cap on `main` itself (see
[When a shard is still too slow](#when-a-shard-is-still-too-slow)).
`specs/live-postgres-readiness-weights.v2.json` is present, so the default
matrix is `1/6` through `6/6`. The plan job computes it
from the same checkout used by the runner and aggregate. Without the v2 file
the matrix falls back to `1/4` through `4/4`.
A malformed present v2 file fails the plan and the selected aggregate; it never
falls back to v1. A present valid v2 file changes all six check names to `/6`
and requires six matching reports. The selected aggregate fails if the plan,
a shard, or report selection fails.

To produce a v2 file (the first one, or a refit), collect at least three
complete, successful `main` workflow runs with the same runner image
name, Go toolchain, Postgres image digest, proof boundaries, and cache era.
The refresher refuses a pool of fewer than three runs, so a pair of fast
draws cannot set the prices (#9470).
Runs of different inventories pool only from four runs up (see
[Inventories and cache eras](#inventories-and-cache-eras)). Save every
shard job's metadata and log. Mixed hosted-runner image versions are accepted,
and the refresher prints the version it saw on each shard. GitHub rolls hosted
images over several days and standard runners cannot pin a version. The
version is never used in pricing (#9411). Each shard job's first step prints
`lscpu | head -20 || true; nproc` so a job wall can be read against the CPU it drew
(#9532); it is output only, no gate, and the refresher ignores it. The next
refit's fixed job overhead includes the step, about 1 s (roughly 1%). Three
runs price at the
mean; four or more price at the pooled p75 (see
[Pooled refit](#pooled-refit-four-or-more-runs)).
Use the v2 refresher with the runs and an output outside the repository:

```bash
python3 scripts/lib/live_postgres_readiness_weights_refresh.py \
  --schema-version 2 --out /tmp/readiness-weights.v2.json \
  --sample RUN_ID_1=RUN_DIR_1 --sample RUN_ID_2=RUN_DIR_2 --sample RUN_ID_3=RUN_DIR_3
```

The v2 output sorts top-level keys and places two sorted table entries per
line. Re-run the command and compare the proposed file byte-for-byte before
committing it. The v1 output retains its one-entry-per-line layout.

Review the source run and job IDs, successful terminal states, complete rows,
first-use/repeat identification, predicted row and job maxima, defaults, and
exact ordered test coverage. Then add only the sorted
`specs/live-postgres-readiness-weights.v2.json` file in a standalone PR. Its
six-way CI run is the first observed wall-time proof; diagnose a timeout or
spread failure before merging. The six-leg model uses one more SQL runner and may consume more aggregate
runner seconds; the plan job also adds setup time. No speedup or fixed
wall-time result is promised.

## Refit the weights

A refit is a standalone bot-or-owner PR that touches only the weights files.
While `specs/live-postgres-readiness-weights.v2.json` is present, the budget
and drift gates run on it, so a refit must regenerate it with the v2
refresher (see [Move to six shards](#move-to-six-shards)); a v1-only refit
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

### Pooled refit (four or more runs)

A two-run mean under-prices any test or package that drew fast in one run, and
the job that holds it then runs over its prediction (#9519). A pair of fast
draws is worse: two fast shard-1 runs priced shard 1's packages 1.4 to 1.6x
low, and the shard job reached 1797 s against an 1800 s timeout (#9470). The
refresher refuses pools of fewer than three runs, so a pair cannot set the
prices. With four or more
`--sample` runs the v2 refresher prices each per-test, first-use, and repeat
price at the p75 of the pooled samples: linear inclusive, which is
`statistics.quantiles(samples, n=100, method="inclusive")[74]`. A single sample
prices at itself. With three runs it keeps the mean. The cold-cache fit
(inverse-variance weighted), the fixed job overhead, and the defaults keep
their rules in every pool. The refresher prints the rule, the pool size, and
the pool run IDs on one `weights refresh v2: pricing ...` line; copy it into
the refit PR body.

Source history (#9473): each source run is read at its own commit. A tree
without a v2 weights file ran the four-shard v1 model. A tree with one ran the
`shard_count` that file declares, which must be 5 or 6 and pass every other v2
schema check. The run's shard jobs must report that same count, and its rows
are re-planned at that count. A pool may mix five- and six-shard runs, because
test prices do not depend on the cut and row kinds are classified per run. The
refit output is always six-shard, and the committed file, the planner, the CI
reader and the drift gates still accept only six. The pricing line names each
source's shard count.

### Inventories and cache eras

Each enrolled live test changes main's inventory, so a pool that needed one
inventory could close before it reached four runs (#9566). A pool of four or
more runs may now mix inventories. Each test of the newest run's inventory is
priced at the p75 of the runs that contain it when it has at least four
samples. A test with fewer samples is left out of `test_seconds`, so it is
priced at `default_test_seconds` and counts toward the drift gate. Tests that
the newest run no longer has are dropped. First-use, repeat, cold, and fixed
seconds still come from every run, and each run's rows are still checked
against its own commit. A mixed pool prints a
`weights refresh v2: inventories ...` line that names each defaulted test and
its sample count; copy it into the refit PR body. A three-run pool
still needs one inventory.

A pool also holds one cache era, read per shard job. A job without a
`Restore Go build cache` step is cold. The step must succeed, but
`actions/cache/restore` succeeds on a miss too, so the restore step's section
of the shard log decides: `Cache restored from key:` is warm (#9462), and
`Cache not found for input keys` is cold, because a miss compiles what a cold
run compiles. Cold residuals carry the compile time that the cache removes, so
the refresher refuses by name a pool that mixes eras, a run whose shards
differ in era, and any other restore state. It prints the era and the hit,
miss, and no-step counts on a `weights refresh v2: cache era ...` line. The
next pool is warm-era only.

A pooled refit PR must meet these gates:

- Declare the pool before the fit: every green `main` push run of
  `live-postgres-readiness.yml` in one cache era with one runner profile, at
  any inventory, five- or six-shard, in run-ID order. The minimum is
  4 runs, there is no maximum, and no run is skipped for its outcome. If the
  pool fails the cold fit, do not re-select runs; take the failure to the
  issue.
- Time-ordered holdout: fit the pool minus its newest runs and score the
  held-out runs. The holdout fit is a refit and also needs at least three
  runs (#9470): a pool of five or more holds out its two newest runs, while
  a four-run pool holds out its single newest run and fits the oldest three.
  The all-shard maximum of observed job wall over `v2.Scorer` job
  seconds must be at most 1.15.
- Full-pool planner on main's live plan: largest predicted job at most
  1440 s, spread within 15%, and an appended-test window of at least 38
  tests (see [The live-plan spread window](#the-live-plan-spread-window)).
  Record the measured window on this page in the refit PR. The 608 s row
  budget does not change.
- No timeout, skip, or serialization change rides along. The p90 and max were
  measured and rejected: they leave too little of the 1440 s budget.

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
Raising `timeout-minutes` only hides a hang. The five-to-six move in #9460 is
the recorded precedent: five legs saturated at about 1270 s predicted mean
with no guard-legal contiguous re-cut, so the relief was a sixth shard at
unchanged prices, cap, and guards.

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
