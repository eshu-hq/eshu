# #5975 CanonicalizeOdu performance evidence

## Scope

The retained repository-dependency proof Odù, an Ifá conformance input made of
61,007 fact envelopes across eight scopes, is intentionally large enough to
match the live PostgreSQL backfill proof. Its determinism test builds two copies,
reverses one copy's facts, and canonicalizes both. The issue reported 83.41
seconds under Go's race detector for those two calls
(`ledger:5975-issue-race-baseline`).

`CanonicalizeOdu` is a contract-test helper over in-memory facts. The changed
sorter is shared more broadly by fixture recorders, API/MCP replay, input tapes,
graph dumps, synthetic data generation, and report capture. It is synchronous
and performs no I/O itself. The retained proof output is a 277,289,309-byte
canonical JSON document.

## Profile and theory

The measured-baseline profile disproved the issue's suspected super-linear
constructor cost. The normal test took 8.34 seconds
(`ledger:5975-theory-normal-base`); its CPU profile attributed
43.98% cumulatively to JSON indentation, while constructing the retained Odù
used 6.52%. Under `-race`, the test took 85.01 seconds
(`ledger:5975-theory-race-base`). Replay transformation
and array sorting each accounted for about 13%, final JSON encoding for about
18%, and the constructor stayed below 1%.

The canonical sorter eagerly encoded every array element to prepare a total-
order tie-break, even when every configured primary key was unique. The proposed
change computes that canonical tie-break only when the comparison first sees an
equal primary key. It preserves the existing tie-break for duplicate and missing
keys.

| Candidate | Cheapest proof | Before | After | Accuracy | Disposition |
| --- | --- | ---: | ---: | --- | --- |
| Lazy canonical tie-break | Exact two-call test, normal build | 8.34 s (`ledger:5975-theory-normal-base`) | 3.31 s (`ledger:5975-theory-normal-candidate`) | Same determinism test passes | Proven |
| Lazy canonical tie-break | Exact two-call test, `-race` | 85.01 s (`ledger:5975-theory-race-base`) | 32.45 s (`ledger:5975-theory-race-candidate`) | Same determinism test passes | Proven |
| Constructor optimization | CPU profile | 6.52% normal (`ledger:5975-theory-normal-base`); <1% under `-race` (`ledger:5975-theory-race-base`) | Not changed | Constructor was not the long pole | Rejected |

The synthetic scaling check used the same 3,072-byte payload shape with unique
fact keys. Three one-iteration samples at each size show linear growth, not the
suspected super-linear curve:

| Facts | Before median | After median | Delta |
| ---: | ---: | ---: | ---: |
| 1,000 | 22.058 ms (`ledger:5975-scale-1000-base`) | 12.056 ms (`ledger:5975-scale-1000-candidate`) | -45.3% |
| 5,000 | 111.763 ms (`ledger:5975-scale-5000-base`) | 56.383 ms (`ledger:5975-scale-5000-candidate`) | -49.6% |
| 10,000 | 211.241 ms (`ledger:5975-scale-10000-base`) | 101.696 ms (`ledger:5975-scale-10000-candidate`) | -51.9% |

The old and new canonical documents have the same length and SHA-256:
`277289309` bytes and
`09be4f2ed6380284a3a00f18d90a3ba10b53fb8e2aa168ce7ed69363d49979b7`.
No cassette or golden snapshot changes are required because canonical bytes do
not change.

## Before and after

The committed benchmark performs exactly two canonicalizations. It constructs
the two retained inputs before the timer starts, so the measurement covers the
profiled canonicalization path rather than fixture setup. Each cell is the
median of three one-iteration samples on the same Apple M5 Max, 18 logical CPUs,
128 GiB RAM, Darwin arm64 host.

| Build | Before | After | Delta | Allocations before → after | Bytes before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Normal | 6.499 s (`ledger:5975-two-call-normal-base`) | 2.402 s (`ledger:5975-two-call-normal-candidate`) | -63.0% | 27.21M → 12.32M | 12.67 GB → 5.50 GB |
| `-race` | 76.626 s (`ledger:5975-two-call-race-base`) | 24.543 s (`ledger:5975-two-call-race-candidate`) | -68.0% | 28.18M → 12.51M | 18.52 GB → 7.15 GB |

The comparison uses base `9379dc9c0fd84edc087953a3c92c03a1327aef67`.
The measured production sorter blobs are
`0a8a72d74c4e01529abfc04643aeff696aefedd8` before and
`b8f3427ae4eb5edf28ce89f7dad1a279f7fd0aa1` after; the reviewed feature tree
contains that exact after blob. The
benchmark file is `go/internal/ifa/odu_benchmark_test.go`; its identical Git
blob in both measured worktrees is
`1a1dd68b094ab15b6e949ed449d759920f36d5c0`. The base run copied that file
into a detached base worktree before running the command, so both results
execute the same benchmark code. Both outputs report `122014 facts/op`, the two
61,007-fact calls in one benchmark operation.

The exact benchmark commands were:

```bash
GOCACHE=$PWD/.gocache-bench go test ./internal/ifa \
  -run '^$' \
  -bench '^BenchmarkCanonicalizeRepoDependencyBackfillProofTwoCalls$' \
  -benchtime=1x -count=3 -benchmem

GOCACHE=$PWD/.gocache-bench-race go test -race ./internal/ifa \
  -run '^$' \
  -bench '^BenchmarkCanonicalizeRepoDependencyBackfillProofTwoCalls$' \
  -benchtime=1x -count=3 -benchmem

GOCACHE=$PWD/.gocache-scale go test ./internal/replay \
  -run '^$' \
  -bench '^BenchmarkCanonicalizeUniqueFactKeys$' \
  -benchtime=1x -count=3 -benchmem
```

The profile commands were:

```bash
profile_dir=$(mktemp -d)
GOCACHE=$PWD/.gocache-profile go test ./internal/ifa \
  -run '^TestRepoDependencyBackfillProofOduCanonicalBytesAreDeterministic$' \
  -count=1 -v -cpuprofile="$profile_dir/normal.cpu" \
  -memprofile="$profile_dir/normal.mem"
go tool pprof -top -nodecount=30 "$profile_dir/normal.cpu"

GOCACHE=$PWD/.gocache-profile-race go test -race ./internal/ifa \
  -run '^TestRepoDependencyBackfillProofOduCanonicalBytesAreDeterministic$' \
  -count=1 -v -cpuprofile="$profile_dir/race-clean.cpu" \
  -memprofile="$profile_dir/race-clean.mem"
go tool pprof -top -nodecount=35 "$profile_dir/race-clean.cpu"
```

The existing two-call guard
`TestRepoDependencyBackfillProofOduCanonicalBytesAreDeterministic` now requires
the recorded length and SHA-256 from its first result, without adding another
canonicalization to the package. That exact test file was copied into the
detached base tree, matching the benchmark procedure above, and both trees
passed this command with exit code 0:

```bash
GOCACHE=$PWD/.gocache-byte-guard go test ./internal/ifa \
  -run '^TestRepoDependencyBackfillProofOduCanonicalBytesAreDeterministic$' \
  -count=1 -v
```

## Correctness guard

The new unit contract fails on current main because the sorter encodes all
1,000 elements despite their unique primary keys. After the change it observes
zero tie-break encodes and the elements remain in primary-key order. A separate
collision case proves duplicate keys still encode every colliding element and
sort by canonical bytes.

Performance Evidence: the retained 61,007-fact, two-call benchmark improves
from 6.499 seconds to 2.402 seconds normally and from 76.626 seconds to 24.543
seconds under `-race` (`ledger:5975-two-call-normal-base`,
`ledger:5975-two-call-normal-candidate`, `ledger:5975-two-call-race-base`,
`ledger:5975-two-call-race-candidate`), with identical canonical bytes.

No-Observability-Change: the changed sorter is a synchronous in-memory helper
used by replay and recording callers. It performs no I/O, starts no runtime
stage, and owns no network, storage, telemetry, or shared mutable state. The
shared replay tests, benchmark, and Ifá determinism checks are its diagnostic
surface.
