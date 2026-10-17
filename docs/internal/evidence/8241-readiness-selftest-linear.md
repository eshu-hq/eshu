# Readiness Self-Test Linear Cost (#8241)

The CI job `live-postgres-readiness hermetic` runs
`scripts/test-run-live-postgres-readiness-tests.sh`. The job reached its
30-minute cap: the profile gave a median step time of about 1,490 s against
the 1,800 s budget. This change makes the per-proof fail-closed checks linear
in the enrolled proof count. It keeps every assertion those checks made.

## Profile (before)

The old per-proof loop did this for each enrolled proof and for each of
skip, fail, and missing: rebuild every fake event file, then run the full
runner. Each rebuild was O(N) because it forked about five processes for
each entry (`$(pkg_key ...)` and `$(events_of ...)`), so the loop cost
O(N^2). At the head of the first revision, `list-packages` enrolled 314 proofs in 15
packages, which is 942 rebuilds and 942 runner calls. Later rebases changed the
enrolled count; the follow-up section uses the current figures. Derive them with
`python3 scripts/lib/live_postgres_readiness_results.py list-packages` (one row
per package, proofs in the third tab field; 459 proofs in 16 packages when this
note was updated).

Phase seconds on the shared development host, from the read-only profile
taken when 308 proofs were enrolled (the host was loaded, so only the
shares carry over to CI):

| Phase | Seconds | Share |
| --- | ---: | ---: |
| Per-proof loop, 924 rebuilds and runner calls (projected) | ~2,693 | ~93% |
| Per-package loops, 45 rebuilds and runner calls | 157.5 | ~5% |
| Setup, ledger, verifier tree, shard, degrade, rg probe | ~49 | ~2% |

The 112 sampled per-proof iterations had a mean of 2.24 s for the rebuild
and 1.13 s for the runner call. The rest of the script, measured with the
loop removed, took 206.7 s.

## Change

- `write_events` moved to `scripts/lib/test-run-live-postgres-readiness-proofs.sh`.
  It now computes the file key inline (`${pkg//[^a-z]/_}`, the `pkg_key`
  mapping) and writes each run/pass pair with one `printf`. It forks nothing
  per entry.
- `check_each_proof` (same lib) replaces the per-proof loop. It builds the
  events once and runs two phases:
  - Direct phase: for each proof and each of skip, fail, and missing, it
    copies that package's stream aside, applies the same `sed` seed,
    requires the stream to change, runs `live_postgres_readiness_results.py
    verify-results <stream> <package>` (the exact call the unsharded runner
    makes for each package), requires a nonzero exit and the
    `<name>: SKIP|FAIL|missing` line, and restores the clean stream.
  - Runner phase: for each package and each seed, it seeds every proof of
    that package at once, runs the full runner, and requires a nonzero exit
    and a `<name>: SKIP|FAIL|missing` line for every proof of the package.
    That is 45 runner calls, not 942.
- The self-test shrinks from 580 to 554 lines.

## Retained-Assertion Proof

- Fixture bytes: the old (`origin/main`) and the new `write_events` were run
  on the same proof list (314 proofs, 15 packages). `diff -r` of the two
  fake directories exited 0 over 30 files. Old rebuild 900 ms, new 16 ms.
- The old loop required, for every proof and seed, that the verifier and the
  full runner fail and name that proof. The direct phase keeps the verifier
  half for every proof. The runner phase keeps the runner half for every
  proof, with all proofs of a package seeded together.
- `scripts/test-run-live-postgres-readiness-selftest-seeds.sh` (new, wired
  as the registry `test_command` and as a hermetic-job step pinned by
  `go/internal/cigates/readiness_job_split_test.go`) plants eight violations
  in a throwaway tree and requires the self-test to fail with the named
  first failure line:

| Seed | Broken property | First failure line at the first-revision head |
| --- | --- | --- |
| skip-as-pass | verifier accepts a skip | `<first proof> skip event passed` |
| missing-ignored | verifier ignores a test with no events | `missing <first proof> events passed` |
| first-name-only | verifier checks only the sorted-first test | `<first unsorted proof> skip event passed` |
| unnamed | verifier stops naming the test | `<first proof> skip not named: ...` |
| runner-ignores-results | runner ignores the verifier exit | `./internal/query/supply/chain/impact skip events passed the runner` |
| runner-truncates | runner keeps only the last 60 verifier lines | `./internal/storage/postgres skip events: TestProjectorClaimMarkedGuardContention not named by the runner: ...` |
| fail-as-pass | verifier accepts a failure | `<first proof> fail event passed` |
| deep-proof | verifier skips the sorted-last test of the last package whose first proof differs | `TestRowKeyRejectsADuplicateRowLive skip event passed` |

The first proof is `TestSupplyChainImpactReadinessPackageManifestRepoScopeQueryPlanLive`.

- runner-truncates is the review finding on the first revision, which
  checked runner naming only for the first proof of each package. That
  revision passed the self-test under this seed (exit 0). The old
  `origin/main` self-test fails under it (exit 1), as does the first-revision head.
- deep-proof: a scratch copy of the first revision with the direct check
  removed let the deep seed through (`seed deep-proof: self-test passed`).

## Performance Evidence

Performance Evidence: all timings below come from the shared 16-core
development host while peer agents loaded it. Compare within a row only.
The profile's candidate run (115.3 s whole self-test) and the projected
~2,693 s old loop come from different load and are not comparable to each
other or to the rows below.

Interleaved in one process at the first-revision head (load average 22 to 31):

| Measurement | Old loop | New `check_each_proof` |
| --- | ---: | ---: |
| Round 1 | 72.2 s for 10 proofs (7.2 s/proof, ~2,266 s projected for 314) | 87.9 s for 314 proofs |
| Round 2 | 102.7 s for 10 proofs (10.3 s/proof, ~3,224 s projected for 314) | 117.8 s for 314 proofs |
| Fixture rebuild (`write_events`) | 900 ms | 16 ms |

The rounds give about 26x and 27x for the per-proof phase. At the first-revision head the
whole new self-test passed on the clean tree in 171.4 s (load average 20 to
57), and the seeded-violation test caught all eight seeds in 105.1 s (load
average 12 to 29). The deep seed walks almost every proof, so it uses most
of the seeds time.

No-Observability-Change: this is a CI self-test fixture change. It adds no
runtime code, worker, queue, metric, span, log key, or route. Operators keep
reading the existing job step durations and the self-test failure lines.

## Follow-up: one-process direct phase and cap-cancel summary

After #8263 merged, the hermetic job ran in about 5 minutes against the
30-minute cap. Two items stayed open on #8241.

### Direct phase in one process

The direct phase of `check_each_proof` called
`live_postgres_readiness_results.py verify-results` as a python subprocess
for each proof and seed: 1,377 cold interpreter starts at 459 proofs. It is now
`check_proofs_direct`, which pipes one record per proof to
`scripts/lib/live_postgres_readiness_proof_cases.py`. That helper plants the
same three faults (skip, fail, missing) in memory and calls `verify_results`
with the unsharded runner's arguments in one process. It exits 3 with the
former loop's message at the first case that passes or goes unnamed, and
prints the case count so the caller refuses a vacuous pass. The all-names
runner phase (`check_proofs_runner`) and the seeds test are unchanged.

Direct phase only, same 459-proof, 16-package fixture (`list-packages` when
this note was updated), same host in the same minutes (load average 130 to 145, so
compare the CPU seconds as well):

| Direct phase | Real | User | Sys |
| --- | ---: | ---: | ---: |
| Before (1,377 subprocess calls) | 182.7 s | 49.4 s | 33.2 s |
| After, run 1 | 3.25 s | 0.96 s | 0.17 s |
| After, run 2 | 2.70 s | 0.86 s | 0.11 s |
| After, run 3 | 4.23 s | 0.98 s | 0.15 s |

That is about 56x on real time (about 70x on CPU) for the phase. The whole
self-test, before and after, on the same host load:

| Whole self-test | Real | User | Sys | CPU (user + sys) |
| --- | ---: | ---: | ---: | ---: |
| Before (`origin/main` snapshot), load average 150 to 290 | 1,301.2 s | 277.8 s | 81.7 s | 359.5 s |
| After, load average 140 to 166 | 564.8 s | 220.9 s | 53.7 s | 274.6 s |

Both runs exited 0. The two runs did not see the same load, so only the CPU
column compares: the after run used 84.9 s less CPU, which matches the
direct phase's 82.6 s before and 1.1 s after (user + sys). The real-time
column mostly reflects the host. In CI, where the job ran about 5 minutes
after #8263, the removed CPU is the saving. The seeds test passed all eight
seeds against the new helper in 153.5 s real (59.6 s user, 12.4 s sys).

### Cap-cancel summary

GitHub marks a job that reaches `timeout-minutes` as cancelled, not failed,
so no check turns red and the run reads like a concurrency cancel.
`scripts/ci/live-postgres-readiness-job-summary.sh` closes that gap. The
hermetic job records its start epoch in its first step, inline
(`echo "READINESS_JOB_STARTED_EPOCH=$(date +%s)" >> "$GITHUB_ENV"`), because
that step runs before checkout and no repo file exists yet. Its last step runs
with `if: always()` and reads `${{ job.status }}` through `env`, then writes
one verdict to the job summary. That step checks that the script exists and
skips with a warning when checkout did not run (a merge-group run that did not
select the leg), and it turns a classifier failure into a warning, so the
summary cannot fail a healthy job:

| Status | Elapsed | Verdict |
| --- | --- | --- |
| success | any | PASSED with the share of the cap; a warning at 80% or more |
| failure | any | FAILED, "a failure, not a timeout" |
| cancelled | at least cap minus 90 s | CANCELLED BY THE TIME CAP, plus an error annotation |
| cancelled | less than that | CANCELLED BEFORE THE CAP (concurrency group or manual cancel) |

The 90 s grace covers runner setup before the first step. A concurrency
cancel that lands inside the last 90 s reads as a cap cancel; the platform
annotation on the job stays the authority. `CAP_MINUTES` on the summary step
must equal `timeout-minutes`: `go/internal/cigates/readiness_job_split_test.go`
pins it. It also pins that the start record is inline and that no step runs
the repo script before checkout. Its seeded mutations cover a drifted cap, a
summary that does not run on cancel, a summary that ignores the status, a
missing or skipped start record, the start script called before checkout
(the shape the first revision had, which fails on every run), a summary that
fails when the script is absent, and a summary that fails the job when the
classifier fails. `scripts/ci/test-live-postgres-readiness-job-summary.sh` checks the
classification and breaks the classifier four ways in a throwaway copy
(cancel read as failure, failure read as cap, every cancel read as cap, grace
dropped); each seed must fail the cases.

### Cap-cancel step semantics (live experiment)

Whether `if: always()` steps run after `timeout-minutes` cancels a job, and
what `job.status` reads then, is not stated in the GitHub documentation. A
throwaway workflow in `eshu-hq/eshu` (run id 39077917141, 2026-10-17, branch
deleted) set `timeout-minutes: 1` and ran a 150 s sleep step. The job
concluded `cancelled` and the sleep step was `cancelled`. An `if: always()`
step and an `if: cancelled()` step both ran afterwards, and `job.status` read
`cancelled` in them.

This shows the step-order semantics the summary relies on: a cap cancel is a
cancelled job, and the `always()` step runs with `job.status` equal to
`cancelled`. It does not run the Eshu readiness job, so it does not show the
summary text for a real cap hit. The classifier is proven on its inputs by
`scripts/ci/test-live-postgres-readiness-job-summary.sh`. The workflow keeps
`if: always()`. A `run:` text cannot call `cancelled()`, so the step passes
`job.status` through `env`.

## NOT_CHECKED

- CI wall time of the hermetic job after this change. The coordinator
  records it from the first CI run on the pushed head.
- The summary text for a real cap hit of the Eshu hermetic job. Only the step
  semantics are shown live (above); the Eshu job itself has not hit its cap.
- `shellcheck` (not installed on the host); `bash -n` passed on all three
  scripts.
