# Readiness refresher: cross-inventory pools and one cache era (#9566)

Classification: offline-tooling contract change. The v2 weights refresher
(`scripts/lib/live_postgres_readiness_refresh_v2.py` and the new
`scripts/lib/live_postgres_readiness_refresh_v2_pool.py`) runs on a
maintainer's machine against saved CI logs. It changes no committed weights
file, workflow, planner, or gate, and no service runtime.

Performance Evidence: no runtime path changes. The equal-inventory purity check
below shows the refresher's output file is byte-identical to the committed
weights. The CI wall-time effect arrives only with a later refit PR, which
carries its own holdout and planner evidence.

No-Observability-Change: the refresher is offline tooling with no service,
metric, span, or log pipeline. Its operator signal is its stdout: the new
`cache era` line on every v2 fit and the `inventories` line on a mixed pool.

## Rule

The rule is the arbiter ruling for #9473 on the warm Go build cache, item 3
(2026-10-29). Its words:

> a pool may hold runs of different inventories if every run is a green main
> push with one profile and one cache era; each test is priced from the runs
> that contain it, at the pool rule (p75 when that test has >=
> QUANTILE_MIN_POOL samples, else `default_test_seconds`, and the provenance
> line counts the defaulted tests); first-use, repeat, cold and fixed come
> from every run; `source_rows` is proven per run at its own SHA as today;
> the output's inventory is the newest run's.

Item 2 adds: "Warm and cold runs are never pooled together: their residual
samples measure different things." The rule is one era per pool, so a cold-era
pool (such as the nine-run 739-test pool behind #9564) stays valid.

The implementation decisions are:

- "Newest run" is the highest run ID.
- A mixed pool needs at least `QUANTILE_MIN_POOL` (4) runs, so it always
  prices at the p75. A two- or three-run pool keeps the one-inventory
  refusal, which now names the pool size.
- Era is read per shard job, under the arbiter ruling on the cache era
  (2026-10-30). No `Restore Go build cache` step is cold. The step must have
  completed with success; any other state (failure, skipped, cancelled,
  duplicated) is refused. `actions/cache/restore` also succeeds on a miss, so
  the restore step's section of the shard log decides, from the
  `##[group]Run actions/cache/restore@` line to the next step: `Cache restored
  from key:` is warm, `Cache not found for input keys` is cold (a miss
  compiles what a cold run compiles), and neither is refused. Only that
  section is read, because `setup-go` prints its own `Cache restored from
  key:` later in the job. A run whose shards differ in era is refused with
  `cache era differs across shards: <shard>=<era>, ...`.

## Cache census

`actions/cache/restore` reported success on every one of these shard jobs.
The restore step's log line, attempt 1 of each main push run since #9462:

| Run | Head | Shards 1-6 | Result |
|---|---|---|---|
| 40966563533 | `9a79ef1f8d` | hit on all 6 | green |
| 40971336832 | `a2d69c98c2` | hit on all 6 | green |
| 40976622916 | `6fa5a4dd6e` | miss on all 6 | shard 1 cancelled at the cap |
| 40979148939 | `38f6cdf1da` | miss on all 6 | hermetic job cancelled at the cap |
| 40980521969 | `e0093550e8` | hit on all 6 | green |
| 40980751566 | `2fad8ecdab` | miss on all 6 | green |
| 40984917038 | `6168c668eb` | hit on 1-3, 5, 6; miss on 4 | green |
| 40988811685 | `ca2fcbe4ab` | hit on all 6 | green |

Four green runs hit on every shard, all at 749 tests: 40966563533,
40971336832, 40980521969 and 40988811685. With the fourth, trigger 2(a) (four
such runs at one inventory) fired on 2026-10-30. A fit on the first three
alone fails with `no measured repeat row category`: main's cut before #9564
has no repeat row, and #9564's cut does (`./internal/storage/postgres` takes
two rows in each of shards 3 to 5). 40988811685 ran #9564's cut, so the
four-run fit succeeds: `749 priced tests`, p75 over a 4-run pool, `cache era
warm ("Restore Go build cache" in 24 shard jobs: 24 hit, 0 missed; 0 shard
jobs without the step)`. That fit is evidence for the era rule, not a refit;
the refit PR carries its own gates. The refresher is unchanged for the repeat
rows.

## Proof

Commands ran at the #9566 commit rebased on origin/main, which holds #9543
(#9577) and #9564; the PR body names the base commit. "Before" is that base
in a private detached worktree.

| Check | Command | Result |
|---|---|---|
| Unit contract | `python3 -I -B scripts/test-live-postgres-readiness-refresh-v2.py` | 60 tests OK. The pooling cases were RED first (3 failures, 4 errors on the 7 initial cases). The era-ruling cases (a miss is cold, a run with mixed shards is refused, setup-go's line is ignored, a log with neither line is refused) were RED on the step-status rule: 4 failures, 1 error |
| Boundary mutant | `>=` to `>` on the sample threshold | `test_newer_test_with_exactly_four_samples_is_priced` fails |
| Step mutant | drop the `conclusion != "success"` check | `test_refuses_failed_restore_and_unknown_outcome` fails |
| Newest mutant | `max` to `min` run ID | three cases fail |
| Log-text mutant | every restore section reads as a hit | three cases fail |
| Scope mutant | keep reading after the restore step ends | `test_era_reads_only_the_restore_step_section` fails |
| Purity, nine-run 739 pool | refresher before and after, same `--sample` list | v2 file `cmp`-identical, and equal to main's committed v2 (#9564). Stdout gains one line, `cache era cold ("Restore Go build cache" in 0 shard jobs: 0 hit, 0 missed; 46 shard jobs without the step)`, and its first line reads `739 priced tests` where it read `739 tests` |
| Real mixed pool | the nine runs plus 741-test runs 40913641758, 40927154134, 40951251097 | before: `source inventories differ`. After: a 12-run fit, output inventory 741 from run 40951251097, two tests defaulted and named, `TestECSRuntimeIndexFreshBootstrapLive` and `TestECSRuntimeIndexUpgradeLive` (3 samples each) |
| Real mixed era | the nine runs plus warm run 40966563533 | `source runs mix cache eras: 40867705415 is cold and 40966563533 is warm; a pool holds one era` |
| Real miss is cold | the nine runs plus miss run 40980751566 | a 10-run cold fit: `in 6 shard jobs: 0 hit, 6 missed; 46 shard jobs without the step`, output inventory 749, 10 tests defaulted |
| Real run with mixed shards | the nine runs plus 40984917038 | `source 40984917038 cache era differs across shards: 1=warm, 2=warm, 3=warm, 4=cold, 5=warm, 6=warm` |

The real mixed pool is an illustration, not a proposed refit: it is cold-era,
and the next pool is warm-era only. Its constants moved within normal pool
variation from the nine-run fit: fixed 83.5 to 82.3 s, cold 11.0 to 8.2 s,
default first use 42.9 to 44.3 s, and default test (5.1 s) and default repeat
(13.8 s) unchanged. 338 of the 739 priced tests changed price with the three
added samples.

## Not checked

- The warm refit itself (its holdout and planner gates): that is the refit
  PR's work, not this one's.
- `scripts/test-live-postgres-readiness-calibration.py` fails 1 and errors 2
  on the base exactly as on this branch. It does not import the
  refresher, and no gate runs it.
