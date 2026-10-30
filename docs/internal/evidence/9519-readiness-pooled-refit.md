# Readiness weights pooled refit (#9519, #9473)

The two-run refits (#9436, #9495) priced each test from the two draws its
shard got. Job time on the same shard slice varies by about 1.4x between
runs (a runner draw is the working hypothesis, #9532), so a shard that ran
fast in both fit runs was priced low and later ran over its price. Shard 1
did that on main. This refit prices each test and package residual at the p75 of a
declared pool of nine green main runs, with the refresher from #9534. It
changes no gate, test, timeout, or refresher. It also restates the shards
page's window figure for the new prices.

## The pool

The pool was declared by property before the fit (arbiter ruling for #9473,
blocked refit; pool-inventory ruling of 2026-10-29). It takes every green
main push run of `live-postgres-readiness.yml` with the 739-test inventory
and one runner profile, on a five- or six-shard cut, completed before the
fit, in run-ID order, with a minimum of 4 runs. No run is skipped by
outcome.

| Run | Head | Shards | Attempt |
| --- | --- | --- | --- |
| 40867705415 | b69532e3d2 | 5 | 1 |
| 40878710102 | ba9919e3c6 | 5 | 1 |
| 40879134083 | 7ef08c6566 | 5 | 1 |
| 40885865002 | 21546477b8 | 5 | 1 |
| 40887740898 | a44f262b62 | 5 | 1 |
| 40888833062 | d3a1990e53 | 5 | 1 |
| 40894770170 | 37e0578cc4 | 5 | 1 |
| 40899888098 | 9da719cf9f | 5 | 1 |
| 40900152112 | 73418463ed | 6 | 1 |

- All nine are green, have equal 739-test inventories, and share one profile:
  ubuntu-24.04, Go 1.26.10, Postgres image digest `afc7e2d441324c03...`.
  Every shard ran runner image 20261025.353.1. The image version is recorded
  as provenance only.
- 40882084309 (ee2c47e96b) is excluded by the refresher's own validity check,
  not by outcome. It went green on attempt 2 by rerunning shard 1 only, so
  shards 2 to 5 carry attempt-1 artifact names, and the provenance check
  refuses the run (`weights refresh v2: source 40882084309 shard 2 log is
  from attempt 1, the run is attempt 2`).
- Green runs after 40900152112 have 741 tests, and main plans 749 since #9541
  (merge f6e093ceac). The refresher refuses mixed inventories, so those runs
  are outside this pool (see the defaulted tests below).

## The refit

The merged refresher (main at 4f3e4d862c) ran unchanged, twice, into
separate files. Both JSON outputs and the v2 stdout were byte-identical. Its
provenance line, abbreviated: `pricing p75 (linear inclusive) of per-test, first-use and
repeat samples over a 9-run pool ... (5 shards) ... 40900152112 (6 shards);
cold, fixed and defaults keep their committed rules`.

| Constant (v2) | Main before (#9495) | Pooled refit |
| --- | --- | --- |
| `cold_cache_seconds` | 60.2 | 11.0 |
| `fixed_job_seconds` | 74.0 | 83.5 |
| `default_test_seconds` | 4.2 | 5.1 |
| `default_package_first_seconds` | 37.2 | 42.9 |
| `default_package_repeat_seconds` | 20.7 | 13.8 |

The cold fit has three per-package terms:

| Package | Diff | First-row samples | Later samples | Variance | Weight |
| --- | --- | --- | --- | --- | --- |
| `./internal/query/supply/chain/impact` | 11.6 s | 2 | 9 | 1.00 s², the floor | 0.8577 |
| `./internal/reducer/freshness/links` | 8.1 s | 7 | 9 | 6.08 s² | 0.1410 |
| `./internal/storage/postgres` | -25.6 s | 28 | 9 | 668.96 s² | 0.0013 |

The first term carries the weight on two first-row samples, and its variance
sits at the refresher's 1.0 s² floor. So the cold constant is less certain
than its formal standard error (0.93 s) suggests. The holdout and
out-of-sample scores below price the whole job, cold included.

The v1 fallback used `--fixed-job-seconds 83.5`, the mean of all 46 shard
overheads in the pool. Its constants: cold 191.3 to 74.2, default test 4.5
to 4.8, default package 23.4 to 31.3.

## Holdout gate

The holdout fit uses the pool minus its two newest runs and scores those two
at the job level (observed job wall over `v2.Scorer` job seconds, on the cut
each run ran):

| Run | Shards | Shard-1 job ratio | All-shard max job ratio |
| --- | --- | --- | --- |
| 40899888098 | 5 | 0.957 | 0.994 |
| 40900152112 | 6 | 1.023 | 1.023 |

The bar is an all-shard maximum of at most 1.15. The result is 1.023: PASS.

The holdout gate is necessary but not sufficient. With the mean in place of
p75 on the same pool, the holdout maximum is 1.133 and also passes. On the
16 older runs the mean then reaches 1.311, with 24 of 80 shards over 1.15.
Acceptance is therefore the holdout bar together with the out-of-sample
table below.

## Prediction against measurement

All ratios here are job level: observed job wall over predicted job seconds,
on the cut each run actually ran. Percentiles are linear interpolation,
inclusive method (`statistics.quantiles`). The 16 older runs are the green
five-shard main runs before the pool, 40672571941 through 40859217483. They
include main's own fit pair, so main's prices are scored on the other 14.

| Prices | Population | Shard 1 p50 / max | All-shard p50 / p95 / max | Over 1.15 |
| --- | --- | --- | --- | --- |
| Main (#9495) | pool (9 runs, outside main's fit) | 1.376 / 1.464 | 0.947 / 1.406 / 1.464 | 9 of 46 |
| Pooled p75 | pool (in-sample) | 0.973 / 1.024 | 0.971 / 1.024 / 1.072 | 0 of 46 |
| Main (#9495) | 14 older runs | 1.397 / 1.500 | 1.044 / 1.435 / 1.500 | 15 of 70 |
| Pooled mean | 16 older runs | 1.008 / 1.099 | 1.067 / 1.232 / 1.311 | 24 of 80 |
| Pooled p75 | 16 older runs | 0.978 / 1.065 | 1.015 / 1.128 / 1.223 | 2 of 80 |

## Shard 1 was priced from two fast runs

On main's prices, shard 1 ran over 1.30 times its price in 18 of 28 runs,
and shards 2 and later in 0 of 116 jobs. The 28 runs are the 16 older runs,
the nine pool runs, and the three green six-shard 741-test runs (40913641758,
40927154134, and 40916025838 attempt 2). Each is scored with `v2.Scorer` on
main's committed v2 file at 4f3e4d862c, on the cut it ran.

The [shard-1 diagnosis on #9473](https://github.com/eshu-hq/eshu/issues/9473#issuecomment-6506418266) scored each job's speed against each
test's own typical time (the per-job median of test elapsed over that test's
median across all jobs). On that measure, slow jobs (ratio above 0.93)
landed on shard 1 at the same rate as on shards 2 to 6: 14 of 22 shard-1
jobs against 60 of 93.
What differed was the price. In both runs main's prices came from
(40833905620, 40847362740), the job holding most of today's shard-1 tests ran
fast, so those tests were priced low: on the old six-shard shard-1 slice,
main's prices are 0.667 of the pooled prices in test seconds (723.4 s against
1084.5 s) and 0.699 for the whole job (1219.0 s against 1743.5 s). The cap
cancellations fell on shard 1 because it was the shard priced lowest relative
to its typical time.

This was checked at the job level with leave-one-run-out: fit on eight pool
runs, score the ninth. Extra runs are scored on the full-pool fit: 19 green
runs outside the pool (the 16 older runs and the three six-shard 741-test
runs named above) plus five shard-1 jobs cancelled at the 30-minute cap in
three runs through run 40941475925, as of the fit. Those are 40915510776
attempts 1 and 2, 40916025838 attempt 1, and 40933609276 attempts 1 and 2.
They are censored: their true ratios are unknown, so the shard-1 cells count
them separately and use them for no upper bound. Every run is scored on the
cut it actually ran. Two more cancellations followed on the old prices after
the fit, both in run 40951175891 (head e400c15d3a): shard 1 attempt 1 at
about 1819 s and attempt 2 at about 1806 s (read from the attempts API on
2026-10-29).

| Prices | Set | Shard 1 > 1.30 | Shards 2+ > 1.30 | Shard 1 > 1.15 | Shards 2+ > 1.15 | Max shard 1 / others |
| --- | --- | --- | --- | --- | --- | --- |
| Pooled p75 | leave one out | 0 of 9 | 0 of 37 | 0 of 9 | 0 of 37 | 1.026 / 1.076 |
| Pooled p75 | extra runs | 0 of 19 completed; 5 cancelled, at least 1.043 | 0 of 79 | 0 of 19 completed; 5 cancelled, at least 1.043 | 4 of 79 | 1.065 completed / 1.223 |
| Pooled mean | leave one out | 0 of 9 | 0 of 37 | 0 of 9 | 1 of 37 | 1.060 / 1.186 |
| Pooled mean | extra runs | 0 of 19 completed; 5 cancelled, at least 1.075 | 1 of 79 | 0 of 19 completed; 5 cancelled, at least 1.075 | 30 of 79 | 1.099 completed / 1.311 |

On completed jobs, pooling removes the shard-1 excess under either
statistic; the five cancelled jobs stay unresolved. On the later shards, 4 of
79 jobs exceed 1.15 at p75, against 30 of 79 at the mean. When job time on
a slice varies by about 1.4x between runs, p75 prices near the slower runs,
which are the ones the cap has to survive.

The pooled prices put main's old six-shard shard-1 slice at 1743.5 s (main
priced it at 1219.0 s). The five cancelled jobs (1819 to 1822 s) are lower
bounds, so they score at least 1.043 to 1.045 (true ratio unknown). The three green six-shard
741-test runs score 0.721, 0.715 and 0.950 on that slice. The newest green
run, 40941475925, is scored separately because the refresher's parser refuses
its shard-6 log (#9543). Its shard 1 ran 1772 s, 1.016 on the pooled slice.
The planner re-cuts on the new prices (next section). Whether runs on the new
cut stay under the 30-minute cap is NOT_CHECKED until runs after the merge.
Runner variance is tracked in #9532.

## Main's plan (749 tests, six shards, `9ffd76a581`, 2026-10-29)

This branch was rebased onto origin/main `9ffd76a581`; main's live plan there
has 749 tests. The holdout and out-of-sample figures above do not depend on
main's plan: each run is scored on the cut it ran. The rebase changed no pool
input, refresher file or weights byte. At `9ffd76a581` the refresher run over
the nine pool runs reproduces both committed files and the original stdout
(`cmp`-identical). The plan figures below are from this snippet, run from the
repository root with the committed weights (pass `Path(...)` to
`wt.load_weights` for another file):

```bash
python3 -I -B - <<'EOF'
import sys
sys.path.insert(0, "scripts/lib")
import live_postgres_readiness_v2 as v2
import live_postgres_readiness_weights as wt
from live_postgres_readiness_balance_cases import live_plan

w, plan = wt.load_weights(), live_plan()
bounds, scorer = wt.weighted_bounds(plan, w), v2.Scorer(plan, w)
jobs = [scorer.cost(lo, hi) / wt.UNIT for lo, hi in zip(bounds, bounds[1:])]
rows = [r for lo, hi in zip(bounds, bounds[1:]) for r in v2.split_rows(plan[lo:hi], w)]
mean = sum(jobs) / len(jobs)
print(len(plan), "tests; jobs", " / ".join(f"{j:.1f}" for j in jobs),
      f"; spread {max(abs(j - mean) for j in jobs) / mean:.2%}; rows {len(rows)};",
      f"largest row {max(v2.row_cost_tenths(r, w) for r in rows) / wt.UNIT:.1f} s")
EOF
```

| Check | Main before (#9495 prices) | Pooled refit | Bar |
| --- | --- | --- | --- |
| Predicted jobs (s) | 1227.3 / 1194.6 / 1193.0 / 1211.8 / 1213.5 / 1214.4 | 1305.2 / 1304.1 / 1275.6 / 1267.4 / 1268.8 / 1272.9 | largest at most 1440 |
| Largest deviation from the mean job | 1.51% | 1.78% | at most 15% |
| Rows; largest row | 33; 535.2 s | 36; 450.9 s (s4 `./internal/storage/postgres`, 47 tests) | row budget 608 unchanged |
| Defaulted tests / packages (drift limit 40) | 11 / 1 | 10 / 1 | |
| Appended-test window | fails k=68..98 | fails k=43..51 and 61..69 (accepts 42) | at least 38 |

Drift and budget errors: none on either. Usable headroom is the lower of the
fit window (42) and the drift room (40 minus 10 defaulted), so it is 30. The
window line on the shards page prints `fails k=43..69`, the first and last
failing `k`.

The ten defaulted tests were enrolled after the 739-test pool closed, so they
price at `default_test_seconds` (5.1 s). The defaulted package
`./internal/query/admin/store` is new and prices at
`default_package_first_seconds` (42.9 s).

- `TestECSRuntimeIndexFreshBootstrapLive` and `TestECSRuntimeIndexUpgradeLive`
  (`./internal/storage/postgres`). Measured in shard 2 of the green 741-test
  runs 40913641758 and 40927154134, FreshBootstrap took 1.23 s and 1.18 s, and
  Upgrade took 3.60 s and 3.09 s.
- Eight tests from #9541 (merge f6e093ceac), not yet measured on main:
  `./internal/query/admin/store` `TestAdminReopenWritersStampReopenedAtLive`,
  `TestSharedIntentReopenAgeThroughStatusLive` and
  `TestSharedIntentReopenRacesLive`; `./internal/query/admin`
  `TestAdminReplayAfterCommittedWakeIsANoOpLive`; and
  `./internal/storage/postgres` `TestDeadLetterReplayGetsBoundedFreshReadinessGraceLive`,
  `TestReopenStampConcurrentWritersLive`,
  `TestSharedIntentPendingAgeIndexOnlyScanLive` and
  `TestTerminalReopenWritersStampReopenedAtLive`.

They ride default prices until the next refit, which declares its pool under
the arbiter's pool rule (every accepted green main push run on one inventory,
minimum four).

## Row tail (#9348 method)

This table is row level: rows predicted at 150 s or more, measured row
seconds over predicted row seconds.

| Population | Rows | p50 | p95 | p99 | Max |
| --- | --- | --- | --- | --- | --- |
| Pool (in-sample) | 80 | 0.98 | 1.04 | 1.14 | 1.29 |
| 16 older runs | 124 | 0.95 | 1.06 | 1.14 | 1.16 |

The worst in-sample row is 40887740898 shard 5 (46 tests, 177.7 s predicted,
228.8 s measured). The worst row in the 16 older runs is 40854348722 shard 3 (28
tests, 432.8 s predicted, 502.7 s measured). The row budget stays 608 s. The
largest row the planner cuts on the new prices at `9ffd76a581` is 450.9 s.

## Shards page

"Where the window is" on
`docs/public/reference/local-testing/live-postgres-readiness-shards.md` now
gives, at `9ffd76a581`, 749 tests, 10 defaulted, fails k=43..51 and 61..69,
and usable headroom 30. The page's own two snippets print
`749 tests; fails k=43..69` and `10` on this branch at that base.

## Checks

- `python3 -I scripts/test-live-postgres-readiness-shards.py`: 77 tests, OK.
- `python3 -I scripts/test-live-postgres-readiness-refresh-v2.py`: 44 tests, OK.

## Pending

Still pending are this PR's own six-shard run and the main runs after it
merges. The #9473 close bar is the arbiter's replacement bar. Over at least
three green main push runs after the merge, the median of shard 1's tests
ratio (measured test seconds over priced test seconds) must be at most 1.15,
with each run's tests ratio and job ratio reported. A median that stays
above 1.15 reopens the refit. The arbiter withdrew the two-consecutive
job-ratio bar, ruling that a per-test price cannot absorb a whole-runner
draw. Runner variance and the
cap are #9532.

## How it was measured

Run data came from `gh api` (`run.json`, `jobs.json`, shard logs). The
pipeline script (pool check, refresher twice with `cmp`, holdout, planner
bars, out-of-sample ratios, tails, suites) and the leave-one-run-out script
are in the executor's scratchpad and are not committed. They reuse the
refresher's parsers and the planner's `v2.Scorer` on the committed weights
file. The leave-one-run-out full-pool fit is byte-identical to the committed
v2 file.
