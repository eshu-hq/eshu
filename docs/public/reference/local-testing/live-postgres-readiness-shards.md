# Live PostgreSQL readiness shards

The `live-postgres-readiness` workflow runs the enrolled PostgreSQL proofs in
four parallel jobs. Each job has a 30-minute cap (1800 s). This page explains
how the plan is cut into four shards, how to keep every shard well under the
cap, and how to refresh the numbers the cut depends on.

## How the cut works

The plan is the flat `package|test` list from
`live_postgres_readiness_results.py list-packages`, in canonical inventory
order. `scripts/lib/live_postgres_readiness_weights.py` cuts it into four
contiguous slices so the longest predicted shard is as short as possible, then
balances the other three. Every proof is in exactly one slice. A new test
enters the plan at its inventory position and the cut moves with it, so no one
edits a pinned table.

The runner (`plan` and `report`) and the aggregate (`verify` and `select`) all
call the same cut function with the same weights file from the same checkout.
The aggregate still requires the four shard reports to be the exact union of
the plan. A report cut under different weights fails with `plan mismatch`.

## What the prediction counts

`specs/live-postgres-readiness-weights.v1.json` holds the fitted cost model,
in tenths of a second so the cut is identical on every machine:

| Field | Meaning |
| --- | --- |
| `test_seconds` | Measured elapsed seconds per `package\|test`, averaged over the source runs. |
| `package_seconds` | Compile and link cost paid each time a package appears in a shard. `./internal/storage/postgres` costs about 427 s per appearance, so it should span at most three shards. |
| `cold_cache_seconds` | Extra build-cache cost paid by the first package of each shard. |
| `fixed_job_seconds` | Container start, checkout, and toolchain setup per job. |
| `default_test_seconds`, `default_package_seconds` | Used for a test or package with no measured entry (the mean of the measured values). |
| `source_runs` | The CI run ids the numbers came from. |

Predicted job seconds for a shard are the sum of its tests, the package costs of
its packages, the cold-cache constant, and the fixed job overhead.

## Gates

`python3 scripts/test-live-postgres-readiness-shards.py` runs in the hermetic
job and fails when:

- any shard's predicted job seconds pass 80% of the 1800 s cap (1440 s). Adding
  tests without refreshing the weights is caught here once the defaults stop
  covering them. The 20% headroom is the model's error band. Replayed on the
  retired count cut, the weights file's prediction missed the eight measured
  shard jobs of source runs 40047410741 and 40049588589 by -15% to +24%
  (prediction minus measurement, as a share of the measurement; shards 1 to 3
  of 4 by -6% to +11%). Those runs also fitted the weights, so
  treat the band as a floor. At the -15% end a 1440 s prediction runs about
  1700 s, still under the cap. Runner speed adds noise on top. On one layout
  and plan, shard 1 took 1129 s in run 40057201268 and 1568 s in run
  40057373740 (+39%), and its summed test seconds rose from 598 to 841 (+41%),
  which fits a slower runner. A 1299 s prediction reaches about 1800 s at
  +39%. The 80% rule keeps 1.25x for model error and runner noise combined, so
  read the first CI wave on the new cut before relaxing it. Predicted figures
  are model output until a CI run on the new cut records the measured job
  seconds;
- more than 40 planned tests, or more than 2 planned packages, have no measured
  weight;
- the shard job's `timeout-minutes` stops matching the 1800 s cap the gate
  uses;
- a weights file is malformed, has duplicate keys, or a non-finite or negative
  number;
- a shard report cut under other weights passes the aggregate.

## Refresh the weights

Refresh after a batch of new tests, when the gate fails, or when a shard job
runs near its cap. Take two or more green full runs of the workflow, download
each shard job's log, put each run's four logs in its own directory, and run:

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
refresh. Commit the regenerated file with the change that needed it, then run
the shard contract test again.

### A PR that enrolls more than 40 unweighted tests

The hermetic job fails the drift check, and no green full run has logs for the
new tests. The shard jobs do not wait on the hermetic job and run with
`fail-fast: false`, so they still finish on the PR head with default prices for
the new tests. Take the logs from that run:

```bash
gh api repos/eshu-hq/eshu/actions/runs/<RUN_ID>/jobs \
  --jq '.jobs[] | select(.name | test("shard")) | [.id, .name] | @tsv'
gh api repos/eshu-hq/eshu/actions/jobs/<JOB_ID>/logs > <RUN_DIR>/shard<N>.log   # per shard job
python3 scripts/lib/live_postgres_readiness_weights_refresh.py \
  --out specs/live-postgres-readiness-weights.v1.json \
  --fixed-job-seconds <SECONDS> --sample <RUN_ID>=<RUN_DIR>
```

Use this run only when all four shard jobs passed, since a failed shard log
misses its package groups and the refresher fails closed. Add it as an extra
`--sample` next to the earlier runs, then commit the regenerated file and push.

## When a shard is still too slow

If the gate fails after a refresh, the plan no longer fits in four shards at
80% of the cap. Reduce the cost instead of raising the cap: split the slowest
package's proofs, cache the Go build for the storage package, or add a shard.
Raising `timeout-minutes` only hides a hang.
