# Live PostgreSQL readiness hermetic job

The `live-postgres-readiness hermetic` job checks the readiness runner against
a fake `go`, with no database. It has its own 30-minute cap (1800 s), separate
from the [readiness shards](live-postgres-readiness-shards.md).

## What takes the time

The step `Check runner fail-closed behavior`
(`scripts/test-run-live-postgres-readiness-tests.sh`) is about 80% of the job.
It runs the full runner once per package for each of three seeds (skip, fail,
missing) and once more per package with a nonzero `go` exit. Each of those runs
calls the fake `go` and the results verifier once per package, so the step
grows with packages times proofs.

Until #9582 these runs went one at a time on a 4-vCPU runner. With identical
inventory the step took 721 s to 1267 s, because the hosted VM speed varies. The
slow end then reached the cap (1816 s, cancelled, in run 40979148939).

## The case pool

The per-package runner cases now run in a bounded pool
(`scripts/lib/test-run-live-postgres-readiness-pool.sh`). The width is
`min(nproc, 4)`. Set `LIVE_READINESS_SELFTEST_JOBS` to force a width;
`LIVE_READINESS_SELFTEST_JOBS=1` runs the cases one at a time.

The pool keeps the step fail closed and deterministic:

- Each case runs on its own copy of the fake event streams, with its own
  `LIVE_READINESS_DEBUG_DIR`. The runner clears its debug dir on every run, and
  its default debug dir is shared, so the cases cannot use the default. The
  runner's scratch dir comes from `mktemp -d`. The runner opens no ports and
  starts no containers.
- A case records its exit status only after the runner returns. A case that
  crashes or is killed leaves no status, and the drain fails and names it.
- The drain waits for every case. A hung case holds the step until the job cap
  cancels it, so it can never pass.
- The self-test checks each status and output in package and seed order, as
  the cases settle. At the first broken case it starts nothing more, waits for
  the cases already running, and fails naming that case. Every case before it
  was checked and passed, so the first failure it reports does not depend on
  which case finished first. A clean run still checks every case.

`bash scripts/test-live-postgres-readiness-pool.sh` proves these properties
with stand-in cases. It runs a planted pass among cases that are still
running, a killed case, and a hung case. It also checks seeded copies of the
pool that drop a status, skip the wait, ignore the width, share one output
file, never stop early, check settled cases out of order, or skip the cases
left after the drain. The seeds step runs it once per job, beside the seeds.

## The seeded violations

The step `Check self-test seeded violations`
(`scripts/test-run-live-postgres-readiness-selftest-seeds.sh`) breaks one
fail-closed property per seed and requires the runner self-test to fail with
that property's first failure line.

Under the #9582 pool this step took about 18 minutes on a hosted runner. It
had taken under 4 minutes on main before that. The pool checked runner cases
only after all of them ran, so the two seeds that break the runner
(`runner-ignores-results` and `runner-truncates`) ran every runner case before
the first one was read. Every seed also ran the 35-second pool self-test
again. The early stop above and the single pool self-test run remove both
costs (#9629).

The seeds also run in the case pool
(`scripts/lib/test-run-live-postgres-readiness-seed-pool.sh`):

- Each seed gets a private copy of the pristine `scripts/` and `specs/` tree,
  so no seed sees another seed's mutation. The runner self-test in a seed tree
  keeps its fake `go` and streams in its own `mktemp -d` dir.
- The two runner seeds start first, so they do not finish last.
- Every seed is checked after the drain in its declared order, so the report
  is the same at any width and in any finish order.
- `LIVE_READINESS_SEED_JOBS` sets the width. It defaults to the case pool's
  width, and `LIVE_READINESS_SEED_JOBS=1` runs the seeds one at a time.

`bash scripts/test-live-postgres-readiness-seed-pool.sh` proves the order and
first-failure rules with stub seeds at width 1 and width 4. The step prints
each seed's seconds and writes them to the job summary.

## Job summary

The hermetic job and each shard leg end with
`scripts/ci/live-postgres-readiness-job-summary.sh finish`. Each one writes a
block to the run summary with the job name, the wall in seconds, the percent of
the cap, and the runner CPU model from `lscpu`. Use the blocks to compare the
hermetic wall with the shard walls of the same run. Use the CPU model to tell
a slow VM from a slower test.

Keep `CAP_MINUTES` on each summary step equal to that job's `timeout-minutes`.
`go/internal/cigates/readiness_job_split_test.go` pins both.
