# Readiness Self-Test Linear Cost (#8241)

The CI job `live-postgres-readiness hermetic` runs
`scripts/test-run-live-postgres-readiness-tests.sh`. The job reached its
30-minute cap: the profile gave a median step time of about 1,490 s against
the 1,800 s budget. This change makes the per-proof fail-closed checks linear
in the enrolled proof count. It keeps every assertion those checks made.

## Profile (before)

The old per-proof loop did this for each of the 308 enrolled proofs and for
each of skip, fail, and missing: rebuild every fake event file, then run the
full runner. That is 924 rebuilds and 924 runner calls. Each rebuild was
O(N) because it forked about five processes for each entry (`$(pkg_key ...)`
and `$(events_of ...)`), so the loop cost O(N^2).

Phase seconds on the shared development host, from the read-only profile
(the host was loaded, so only the shares and ratios carry over to CI):

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
  events once. For each proof and each of skip, fail, and missing, it copies
  that package's stream aside, applies the same `sed` seed, requires the
  stream to change, runs `live_postgres_readiness_results.py verify-results
  <stream> <package>` (the exact call the unsharded runner makes for each
  package), requires a nonzero exit and the `<name>: SKIP|FAIL|missing`
  line, and then restores the clean stream. For the first proof of each
  package it also runs the full runner and requires a nonzero exit and the
  same line. That is 45 runner calls, not 924.
- The self-test shrinks from 580 to 554 lines.

## Retained-Assertion Proof

- Fixture bytes: the old and the new `write_events` were run on the same
  proof list (308 proofs, 15 packages). `diff -r` of the two fake
  directories exited 0 over 30 files. Old rebuild 1,027 ms, new 15 ms.
- Every proof still gets skip, fail, and missing seeds, checked against the
  production verifier with the runner's arguments. The runner's handling of
  the verifier result is checked for each package and each seed.
- `scripts/test-run-live-postgres-readiness-selftest-seeds.sh` (new, wired
  as the registry `test_command` and as a hermetic-job step) plants six
  violations in a throwaway tree and requires the self-test to fail with the
  named message:

| Seed | Broken property | Required first failure line |
| --- | --- | --- |
| skip-as-pass | verifier accepts a skip | `<first proof> skip event passed` |
| missing-ignored | verifier ignores a test with no events | `missing <first proof> events passed` |
| first-name-only | verifier checks only the sorted-first test | `<first unsorted proof> skip event passed` |
| unnamed | verifier stops naming the test | `<first proof> skip not named: ...` |
| runner-ignores-results | runner ignores the verifier exit | `<first proof> skip event passed the runner` |
| deep-proof | verifier skips the sorted-last test of the last package whose first proof differs | `<that test> skip event passed` |

The runner never sees the deep-proof test, so only the per-proof direct
check catches it. This is the assertion that the old per-proof loop gave
and that a runner-only sample would lose.

## Performance Evidence

Performance Evidence: the candidate script ran the whole self-test in
115.3 s, against a projected ~2,693 s for the old script on the same host
(about 23x). It passed on the clean tree and failed on all six seeds. In the
profile run against the old script, the deep seed hit the 900 s timeout.

This branch was measured on the same host while peer agents drove the
16-core load average between 100 and 145. Absolute seconds are therefore
noisy; compare within each row only:

| Measurement (same process, same load) | Old | New |
| --- | ---: | ---: |
| Per-proof checks, round 1 | 172.9 s for 10 proofs (17.3 s/proof, ~5,326 s projected for 308) | 111.1 s for 308 proofs |
| Per-proof checks, round 2 | 112.1 s for 10 proofs (11.2 s/proof, ~3,452 s projected for 308) | 265.8 s for 308 proofs |
| Fixture rebuild (`write_events`) | 1,027 ms | 15 ms |

The rounds give a 13x to 48x improvement for the per-proof phase. The new
full self-test passed on the clean tree in 400.8 s at load average 19 to 106.
The seeded-violation test passed (six seeds caught) in 120.2 s at load
average 55 to 111. The deep seed walks almost every proof, so it uses most
of that time.

The seeded-violation test is not vacuous. A scratch copy of the self-test
that checks only the runner sample (the direct verifier check removed) let
the deep seed through: `seed deep-proof: self-test passed`, exit 1.

No-Observability-Change: this is a CI self-test fixture change. It adds no
runtime code, worker, queue, metric, span, log key, or route. Operators keep
reading the existing job step durations and the self-test failure lines.

## NOT_CHECKED

- CI wall time of the hermetic job after this change. The coordinator
  records it from the first CI run on the pushed head.
- `shellcheck` (not installed on the host); `bash -n` passed on all three
  scripts.
