# Readiness weights pooled refit (#9723)

Post-merge main (`a371d1e21f`, the #9544 merge) plans 779 tests with 40 on
default prices (the drift cap) and one defaulted package
(`./internal/storage/postgres/infra/inventory`), so the #9723 re-enroll
(+3 tests, +1 package) cannot land: its GO gate needs 37 or fewer defaulted
tests. This refit prices the plan from green main runs. It changes no gate,
test (except the one band premise below), timeout, or refresher, and it
restates the shards page window figures for the new prices. It enrolls
nothing: no markers, no 07b rows, no ledger re-pin (the ledger verifies
clean and unchanged).

## The pool

The pool is the eight newest green main push runs of
`live-postgres-readiness.yml`, consecutive in run-ID order, all attempt 1,
all six shards green. No run is skipped by outcome.

| Run | Head | Shards | Inventory |
| --- | --- | --- | --- |
| 41237451303 | a371d1e21f | 6 | 779 |
| 41228158841 | cf357fcf47 | 6 | 774 |
| 41213445478 | 25b357cc53 | 6 | 774 |
| 41181296111 | 17db234576 | 6 | 766 |
| 41181281573 | 13a0e535c5 | 6 | 766 |
| 41181264126 | 6d240e13bd | 6 | 766 |
| 41174543661 | ed1a8b1048 | 6 | 766 |
| 41168142712 | 3f44ad2ebf | 6 | 766 |

- One profile: ubuntu-24.04 image 20261025.353.1 on every shard, Go 1.26.10,
  Postgres `ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c03...`.
  Cache era warm, 48/48 shard hits. The refresher gates all of this and
  exited 0.
- 41158954953 (the ninth newest green run) is excluded by the refresher's
  own validity check, not by outcome: its shard 6 carries the retention
  package close 1 ms after the previous stamp with 51.88 s of test elapsed,
  so the residual is negative and the run cannot price residuals
  (`source row timestamps are batched (log flush)`, #9543). A mixed
  inventory pool needs at least four runs; eight remain.

## The refit

The committed v2 refresher ran unchanged, twice, into separate files; both
JSON outputs were byte-identical (`cmp -s`). Its stdout lines, in full:

- `weights refresh v2: 763 priced tests from runs [...]` (the eight IDs).
- `weights refresh v2: pricing p75 (linear inclusive) of per-test,
  first-use and repeat samples over a 8-run pool ... (6 shards each);
  cold, fixed and defaults keep their committed rules`.
- `weights refresh v2: runner image ubuntu-24.04 versions ... distinct
  20261025.353.1` (every shard of every run).
- `weights refresh v2: cache era warm ("Restore Go build cache" in 48
  shard jobs: 48 hit, 0 missed; 0 shard jobs without the step)`.
- `weights refresh v2: inventories 766 tests (runs 41181296111,
  41181281573, 41181264126, 41174543661, 41168142712), 774 tests (runs
  41228158841, 41213445478), 779 tests (runs 41237451303); output
  inventory 779 tests from run 41237451303; 16 tests priced at
  default_test_seconds (fewer than 4 samples): [8 with 1 sample, all
  enrolled by the #9544 merge, plus 8 with 3 samples enrolled days
  earlier]`.
- Cold lines: `./internal/query/admin/store diff=25.7s n_first_row=7
  n_first_later=7 variance=6.92s2 weight=0.8797`;
  `./internal/storage/postgres diff=15.8s n_first_row=32 n_first_later=8
  variance=50.62s2 weight=0.1203`.

| Constant (v2) | Before (#9519) | This refit |
| --- | --- | --- |
| `cold_cache_seconds` | 11.0 | 24.5 |
| `fixed_job_seconds` | 83.5 | 126.6 |
| `default_test_seconds` | 5.1 | 4.9 |
| `default_package_first_seconds` | 42.9 | 11.9 |
| `default_package_repeat_seconds` | 13.8 | 16.6 |
| priced tests / packages | 739 / 27 | 763 / 29 |

The two added packages are `./internal/query/admin/store` (1.8 s,
repriced from pre-merge samples; the three #9723 tests themselves stay
unpriced until re-enrolled) and
`./internal/storage/postgres/infra/inventory` (4.9 s). No package dropped.

The v1 fallback used `--fixed-job-seconds 126.6`, the `fixed` subcommand
output (mean of all 48 shard overheads), over the same eight samples:
`weights refresh: 779 tests from runs [...]`. Deterministic across two runs.

## Holdout gate

Fit the oldest six, score the newest two (a pool of five or more holds out
its two newest). Bar: all-shard maximum of observed job wall over
`v2.Scorer` job seconds at most 1.15.

`score 41213445478,...,41168142712 --weights holdout.v2.json --runs
41237451303 41228158841`:
`holdout.v2 | 2 | 0.751 | 0.873 | 0.994 | 0 | 0 | 12 | 0.834 | 1.033 |
1.081 | 0 | 1 | 1.179` — all-shard max 1.081, 0 shards over 1.15. PASS.

## Main's plan on the refit

`plan` subcommand on the committed files at `29ca2e70d5` (plan identical
at `a371d1e21f`, the pool's newest run; the two newer main commits touch
only the frontend E2E pin):

- plan 779; defaulted tests 16 (was 40); defaulted packages 0 (was 1);
  stale 0; drift errors []; budget errors [].
- jobs 924.3 / 929.1 / 932.5 / 917.5 / 930.3 / 930.3 s; max deviation
  1.06% (spread bar 15%). Largest job 932.5 s (bar 1440 s).
- rows 36; max row 416.8 s (`supply/chain/impact`, 22 tests; bar 608 s).
- window: no failing k in 0..120 (bar: at least 38). The doc snippet
  prints `779 tests; no failing k in 0..120`.
- The 16 defaulted tests: 8 with one sample (all added by the #9544
  merge, incl. the infra/inventory role-attestation proof) and 8 with
  three samples. Re-enrolling the three admin/store proofs adds 3 more:
  19 of 40, 0 packages over the 2 cap. The PR 1 GO gate (37 or fewer,
  1 or fewer) is green.

## Contract-test change

`test_seeded_count_split_already_fails_the_gate_on_the_unchanged_plan`
asserted the naive count split already exceeds the 1440 s budget. With
every package priced, the naive split scores 1326.1 s (old weights on the
same plan: 1737.5 s), so the budget-only premise forbids a correct refit.
Per the #9371 doctrine stated in that file (band premises do not belong
on the live plan), the case now fails the naive split through the budget
or the 15% spread — the same two gates the weighted fit case must pass.
The new assertion passes under both the old weights (budget) and the new
weights (spread 46%), and the full 77-test shards suite is green.

## Checks

- `generate-live-postgres-readiness.py --check`: registration current.
- `verify-live-tests-ledger.sh`: ok, 1031 rows (unchanged, no re-pin).
- `test-live-postgres-readiness-shards.py`: 77 tests OK.
- `make pre-push`: green (recorded in the PR body).
