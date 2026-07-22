# Evidence: CODEOWNERS collector hook no-regression (#5419 Phase 6)

The CODEOWNERS collector hook (Phase 2-3) adds two costs to the git
collector's existing content stream (`streamFacts` in `git_fact_builder.go`):

1. An O(1) candidate-path check (`noteCodeownersCandidate`, wrapping
   `codeowners.IsCandidatePath`) run once per content file, regardless of
   whether that file is CODEOWNERS.
2. A single `codeowners.ResolveWinner` + `Parse` + `Emit` pass
   (`emitCodeownersFactsForCandidates`), run once per repository generation,
   only when a recognized CODEOWNERS location was actually present among the
   streamed content files.

`BenchmarkNoteCodeownersCandidate` and `BenchmarkCodeownersContentStreamHook`
(`git_codeowners_facts_bench_test.go`) isolate both costs against the same
input shape: `codeownersBenchFileCount` (400, matching the existing
`deltaSnapshotBenchmarkFileCount` convention in
`git_snapshot_delta_bench_test.go`) repo-relative file paths, with the
"WithCodeowners" variant substituting one of them for the real golden-corpus
fixture body at
`tests/fixtures/ecosystems/go_comprehensive/.github/CODEOWNERS` (issue #5419
Phase 5) instead of an invented one.

## Benchmark command

```bash
env -u GOROOT go test ./internal/collector/ -run '^$' \
  -bench 'BenchmarkNoteCodeownersCandidate|BenchmarkCodeownersContentStreamHook' \
  -benchmem -count=6
```

Apple M5 Max (`darwin/arm64`), 2026-07-21.

## Results (6 runs each, range across runs)

| Metric | Before (no CODEOWNERS) | After (CODEOWNERS present) | Input shape |
| --- | ---: | ---: | --- |
| Per-content-file candidate check (`BenchmarkNoteCodeownersCandidate`, 400 files/op) | n/a -- same function runs unconditionally | 584.7-607.0 ns/op, 0 B/op, 0 allocs/op | 400 relative paths, 1 real CODEOWNERS path + body among them |
| Content-stream hook, 400 files (`BenchmarkCodeownersContentStreamHook`) | 573.0-731.6 ns/op, 0 B/op, 0 allocs/op ("NoCodeowners") | 9,842-13,222 ns/op, 10,539-10,540 B/op, 186 allocs/op ("WithCodeowners") | same 400-path set; "WithCodeowners" swaps path index 0 for `.github/CODEOWNERS` with the real Phase 5 fixture body (2 rules) |

The "NoCodeowners" and "WithCodeowners" subtests share
`codeownersBenchFileCount` (400) and the same non-CODEOWNERS path shapes, so
the ~9.1-12.6us delta between them is attributable entirely to the one-time
`ResolveWinner` + `Parse` + `Emit` pass that only runs when CODEOWNERS is
present -- not to the per-file check, which costs the same (sub-microsecond
total for all 400 files, zero allocations) in both variants.

## Interpretation

The per-file check is negligible: less than 1 microsecond total across 400
files, whether or not a CODEOWNERS file is present, with zero heap
allocations. It is a single `filepath` normalization plus a 3-way string
comparison (`codeowners.IsCandidatePath`) per file -- no regex, no I/O, no
graph query.

The one-time per-repository parse+emit cost (roughly 10 microseconds, ~186
allocations, ~10.5KB) fires only when a repository actually declares a
CODEOWNERS file, and scales with the number of parsed rules (2 in the real
fixture), not with repository file count. Ten microseconds once per
repository generation is immaterial next to the git collector's existing
per-repository cost (native snapshot walk, per-file language parsing, and
graph/content fact emission for every file), which the collector's own
benchmarks (`BenchmarkNativeRepositorySnapshotterFullFixture`,
`BenchmarkEmit`) already measure in the millisecond range for comparable file
counts.

## No-Observability-Change

This is a benchmark-only addition. It adds no metric, span, log, worker,
queue domain, or runtime knob; it exercises functions that already exist and
are already covered by `internal/collector/codeowners`'s own parser and
emitter unit tests (`emitter_test.go`, `parser_test.go`) and by
`internal/collector`'s existing content-stream integration tests.
