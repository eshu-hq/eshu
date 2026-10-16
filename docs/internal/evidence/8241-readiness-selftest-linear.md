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
O(N^2). At this head, `list-packages` enrolls 314 proofs in 15 packages,
which is 942 rebuilds and 942 runner calls.

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

| Seed | Broken property | First failure line at this head |
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
  `origin/main` self-test fails under it (exit 1), as does this head.
- deep-proof: a scratch copy of the first revision with the direct check
  removed let the deep seed through (`seed deep-proof: self-test passed`).

## Performance Evidence

Performance Evidence: all timings below come from the shared 16-core
development host while peer agents loaded it. Compare within a row only.
The profile's candidate run (115.3 s whole self-test) and the projected
~2,693 s old loop come from different load and are not comparable to each
other or to the rows below.

Interleaved in one process at this head (load average 22 to 31):

| Measurement | Old loop | New `check_each_proof` |
| --- | ---: | ---: |
| Round 1 | 72.2 s for 10 proofs (7.2 s/proof, ~2,266 s projected for 314) | 87.9 s for 314 proofs |
| Round 2 | 102.7 s for 10 proofs (10.3 s/proof, ~3,224 s projected for 314) | 117.8 s for 314 proofs |
| Fixture rebuild (`write_events`) | 900 ms | 16 ms |

The rounds give about 26x and 27x for the per-proof phase. At this head the
whole new self-test passed on the clean tree in 171.4 s (load average 20 to
57), and the seeded-violation test caught all eight seeds in 134.2 s (load
average 20 to 51). The deep seed walks almost every proof, so it uses most
of the seeds time.

No-Observability-Change: this is a CI self-test fixture change. It adds no
runtime code, worker, queue, metric, span, log key, or route. Operators keep
reading the existing job step durations and the self-test failure lines.

## NOT_CHECKED

- CI wall time of the hermetic job after this change. The coordinator
  records it from the first CI run on the pushed head.
- `shellcheck` (not installed on the host); `bash -n` passed on all three
  scripts.
