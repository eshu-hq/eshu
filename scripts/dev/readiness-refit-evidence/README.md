# Readiness refit evidence

These scripts and archived inputs re-derive the evidence tables of the pooled
readiness weights refit (#9519, #9473, PR #9564). The tables are in
`docs/internal/evidence/9519-readiness-pooled-refit.md`. They exist so a
reader can check those figures after GitHub's run-log retention ends (#9570).

## Reproduce

From a checkout of this repository with full history, plus `python3` and
`git`:

```bash
scripts/dev/readiness-refit-evidence/reproduce.sh
```

It unpacks the archived inputs, makes a temporary detached worktree of the
evaluated commit, runs every step, compares each output with the recorded
outputs in `testdata/readiness-refit-evidence/expected/`, and
removes the worktree. It takes several minutes on a laptop. The last lines are:

```text
holdout all-shard max: 1.023 (bar 1.15)
out-of-sample all-shard max, shards over 1.15: 1.223, 2 of 80
ALL MATCH
```

It exits 0 only when all 19 checks match:

- the refit over the nine pool runs is byte-identical to the committed
  `specs/live-postgres-readiness-weights.v2.json` and `.v1.json`. The v1 fit
  uses the computed fixed job seconds, 83.5;
- the leave-one-run-out full-pool fit is byte-identical to the committed v2;
- the pool check, refresher stdout, holdout fit and holdout table, the
  out-of-sample table, per-shard ratios, both row tails, main's plan, and the
  leave-one-run-out tables priced at p75 and at the mean match `expected/`
  byte for byte;
- the Prediction table's remaining rows match `expected/` byte for byte:
  main's previous weights (#9495) scored on the pool and on the 14 older
  runs outside its fit, and the pooled-mean full-pool fit scored on the 16
  older runs;
- the later cap-cancellation walls match `expected/cancelled-later.txt`
  byte for byte.

`EVAL_REF` selects the commit whose `scripts/lib` and weights are evaluated.
The default, `3c92eed021`, is the #9564 refit commit. Its parent is main at
`9ffd76a581`, where the plan figures (749 tests) come from. The checkout
must hold `EVAL_REF` and the head SHA of every source run, so a shallow clone
fails; `reproduce.sh` prints the git error and exits 1. It needs a git
checkout because the refresher reads each source run's test inventory from
git at that run's head SHA.

To check the archive against freshly fetched data, pass a directory of run
directories: `reproduce.sh RUNS_DIR`.

## Runs

| Role | Run IDs |
| --- | --- |
| Pool (fit), in run-ID order | 40867705415 40878710102 40879134083 40885865002 40887740898 40888833062 40894770170 40899888098 40900152112 |
| Holdout (fit on the first seven, score these) | 40899888098 40900152112 |
| 16 older runs (out of sample) | 40672571941 40687668044 40753558584 40777558802 40780015081 40784467495 40784513987 40798562144 40800579562 40812860062 40816391526 40818333400 40833905620 40847362740 40854348722 40859217483 |
| Six-shard 741-test runs (extra runs in leave-one-run-out) | 40913641758 40927154134 40916025838 (attempt 2) |

Four shard-1 jobs cancelled at the 30-minute cap are scored as lower bounds
on run 40913641758's shard-1 slice: 40915510776 attempts 1 and 2 (1822 s,
1821 s), 40916025838 attempt 1 (1819 s) and 40933609276 attempt 1 (1819 s).
The walls come from the jobs API, so their logs are not needed.

Three more shard-1 cap cancellations followed after the leave-one-run-out
tables were recorded: 40933609276 attempt 2 (1821 s, job 123678429637) and
40951175891 attempts 1 and 2 (about 1819 s and 1806 s; job 123743298597 is
the attempt-2 job).
`reproduce.sh` re-derives the two attempt-2 walls from the archived jobs API
listings with `refit_evidence.py walls` and checks them against
`expected/cancelled-later.txt`. Attempt 1 of 40951175891 is recorded from the
evidence note: it was read from the attempts API on 2026-10-29 and the jobs
API now lists only the later attempt, so it cannot be re-fetched.

## Inputs and retention

The data lives in `testdata/readiness-refit-evidence/`, because it is
fixture data rather than tooling. `inputs.tar.gz` there (about 570 KB, 3.3 MB
unpacked) holds `runs/<run id>/` for
the 28 runs above plus the two later-cancellation runs described above,
trimmed by `trim_inputs.py`:

- `jobs.json` keeps job identity, timing and step outcomes only.
- `run.json` keeps the fields the refresher's provenance check reads. Nine
  older runs were fetched without it (40753558584, 40777558802, 40780015081,
  40784467495, 40784513987, 40798562144, 40800579562, 40812860062,
  40816391526); they are only scored, which needs `jobs.json` alone.
- The two later-cancellation runs (40933609276, 40951175891) archive only
  `run.json` and `jobs.json`: their cancelled shard-1 jobs carry no usable
  shard log, and only the jobs API walls are re-derived from them.
- `shardN.log` keeps only the lines that the refresher's parser, its
  runner-profile check and its provenance check match. The raw data for
  these runs, about 288 MB, is not archived.

`reproduce.sh RAW_RUNS_DIR` gave the same matches on the raw data as on
the archive when the archive was built (14 checks then; the mean table was
added later), so the trimmed copy loses nothing
these figures use. Anything else in the raw logs is gone once GitHub's log
retention (90 days by default) passes.

`fetch.sh` runs these `gh api` calls for each run ID, writing one log per
shard job whose name matches
`live-postgres-readiness (postgres18) shard (N/T)`:

```bash
gh api repos/eshu-hq/eshu/actions/runs/RUN_ID > run.json
gh api "repos/eshu-hq/eshu/actions/runs/RUN_ID/jobs?per_page=100" > jobs.json
gh api --allow-escape-sequences repos/eshu-hq/eshu/actions/jobs/JOB_ID/logs > shardN.log
```

To rebuild the archive from fresh fetches while the logs still exist:

```bash
scripts/dev/readiness-refit-evidence/fetch.sh RAW_DIR 40867705415 ...
python3 -I scripts/dev/readiness-refit-evidence/trim_inputs.py \
  --lib scripts/lib --out PACK/runs RAW_DIR/*
tar -C PACK -cf - runs | gzip -n -9 > testdata/readiness-refit-evidence/inputs.tar.gz
```

The two later-cancellation runs archive only the jobs API listings (no
shard logs to trim), so refetch just those two files per run:

```bash
gh api repos/eshu-hq/eshu/actions/runs/40933609276 > RAW_DIR/40933609276/run.json
gh api "repos/eshu-hq/eshu/actions/runs/40933609276/jobs?per_page=100" > RAW_DIR/40933609276/jobs.json
```

## Files

| File | What it does |
| --- | --- |
| `reproduce.sh` | Runs every step and compares with `expected/` |
| `refit_evidence.py` | Subcommands `poolcheck`, `score`, `shards`, `tail`, `loo`, `plan`, `fixed`, `walls` |
| `fetch.sh` | Fetches `run.json`, `jobs.json` and shard logs with `gh api` |
| `trim_inputs.py` | Reduces fetched runs to the archived form |
| `testdata/readiness-refit-evidence/expected/` | Outputs recorded when the refit was made |
| `testdata/readiness-refit-evidence/inputs.tar.gz` | Trimmed inputs of the 28 runs |

## Not covered

`reproduce.sh` checks the pooled p75 prices, the rows priced with main's
previous weights (#9495), the out-of-sample "Pooled mean" row, and the two
re-fetchable later-cancellation walls. These figures in the note are still
prose only:

- the shard-1 speed diagnosis linked from the note;
- 40951175891 attempt 1 (about 1819 s): superseded in the jobs API, recorded
  from the attempts API read of 2026-10-29 (see above).

The summary lines of both leave-one-run-out tables count the four recorded
cancelled jobs inside their shard-1 denominators (`0/23` for the extra runs,
`0/32` for all), where the note reports 19 completed jobs and 5 cancelled
ones separately. Those tables record the fit-time state and are intentionally
unchanged; the later cancellations are covered by `cancelled-later.txt`
instead.
