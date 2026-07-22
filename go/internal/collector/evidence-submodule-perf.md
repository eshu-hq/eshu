# Evidence: submodule collector no-regression + gitlink SHA resolution cost (#5420 Phase 6)

The submodule collector (".gitmodules") hook (Phase 2a-2b) adds three costs to
the git collector's existing content stream (`streamFacts` in
`git_fact_builder.go`):

1. An O(1) candidate-path check (`noteSubmoduleCandidate`, wrapping
   `submodule.IsGitmodulesPath`) run once per content file, regardless of
   whether that file is ".gitmodules".
2. A single `submodule.Emit` parse+emit pass
   (`emitSubmoduleFactsForCandidates`), run once per repository generation,
   only when a ".gitmodules" file was actually present among the streamed
   content files.
3. One `git ls-tree HEAD` subprocess (`gitSubmoduleGitlinkSHA`) per declared
   submodule entry, run only when ".gitmodules" is present with entries,
   called from inside `Emit` via `FixtureContext.PinnedSHAResolver` (issue
   #5420 Phase 2b).

`BenchmarkNoteSubmoduleCandidate`, `BenchmarkSubmoduleContentStreamHook`, and
`BenchmarkGitSubmoduleGitlinkSHA` (`git_submodule_facts_bench_test.go`)
isolate all three costs against the same input shape:
`submoduleBenchFileCount` (400, matching the existing
`codeownersBenchFileCount`/`deltaSnapshotBenchmarkFileCount` convention)
repo-relative file paths, with the "WithSubmodule" variant substituting one
of them for a ".gitmodules" body matching the golden-corpus fixture
(`scripts/verify-golden-corpus-gate.sh`, issue #5420 Phase 5) that declares
one submodule at `vendor/deployable-source`, backed by a real committed
gitlink tree entry (mode 160000, via `git update-index --cacheinfo`, the
same fixture technique
`TestEmitSubmoduleFactsForCandidatesResolvesPinnedSHA` uses) instead of an
invented shape.

## Benchmark command

```bash
env -u GOROOT go test ./internal/collector/ -run '^$' \
  -bench 'BenchmarkNoteSubmoduleCandidate|BenchmarkSubmoduleContentStreamHook|BenchmarkGitSubmoduleGitlinkSHA' \
  -benchmem -count=6
```

Apple M5 Max (`darwin/arm64`), 2026-07-21.

## Results (6 runs each, range across runs)

| Metric | Before (no `.gitmodules`) | After (`.gitmodules` present, 1 submodule) | Input shape |
| --- | ---: | ---: | --- |
| Per-content-file candidate check (`BenchmarkNoteSubmoduleCandidate`, 400 files/op) | n/a -- same function runs unconditionally | 217.6-241.5 ns/op, 0 B/op, 0 allocs/op | 400 relative paths, 1 real `.gitmodules` path + body among them |
| Content-stream hook, 400 files (`BenchmarkSubmoduleContentStreamHook`) | 198.9-235.9 ns/op, 0 B/op, 0 allocs/op ("NoSubmodule") | 13.48-24.02 ms/op, 61.4-61.7 KB/op, 223-224 allocs/op ("WithSubmodule") | same 400-path set; "WithSubmodule" swaps path index 0 for `.gitmodules` declaring 1 submodule backed by a real committed gitlink |
| Isolated `git ls-tree HEAD` subprocess, per submodule (`BenchmarkGitSubmoduleGitlinkSHA`) | n/a -- only called when `.gitmodules` has entries | 15.50-20.96 ms/op, 55.0-55.3 KB/op, 115 allocs/op | one real committed gitlink tree entry; one `git ls-tree HEAD -- <path>` call per op |

## Interpretation

### Common case (no `.gitmodules`): negligible

The per-file check is negligible: well under 250 nanoseconds total across
400 files, whether or not a `.gitmodules` file is present, with zero heap
allocations. It is a single string-equality comparison
(`submodule.IsGitmodulesPath`) per file -- no regex, no I/O, no git
subprocess, no graph query. This lands in the same cost class as the
CODEOWNERS precedent's per-file check
(`evidence-5419-codeowners-perf.md`: 584.7-607.0 ns/op for 400 files there,
versus 217.6-241.5 ns/op here) and is in fact cheaper, since
`IsGitmodulesPath` is one exact-match comparison against a single fixed
location instead of CODEOWNERS' three-location precedence check.

Most repositories carry no `.gitmodules` file. For that common case the
collector pays only this sub-microsecond, zero-allocation cost -- no
`ls-tree` subprocess ever runs.

### With-submodules case: NOT negligible, dominated by the git subprocess

Unlike CODEOWNERS' one-time parse+emit cost (roughly 10 microseconds,
`evidence-5419-codeowners-perf.md`), the submodule collector's one-time
per-repository-generation cost when `.gitmodules` IS present is
13.48-24.02 milliseconds for a single declared submodule -- about three
orders of magnitude more expensive than CODEOWNERS' equivalent hook. This is
a real, measurable cost and is reported here as such rather than rounded
down to "negligible."

The dominant cost is the `git ls-tree HEAD -- <path>` subprocess
`gitSubmoduleGitlinkSHA` shells out to resolve each submodule's pinned
commit SHA (issue #5420 Phase 2b). `BenchmarkGitSubmoduleGitlinkSHA`
isolates that single subprocess call at 15.50-20.96 ms/op, which accounts
for essentially all of the "WithSubmodule" content-stream hook's added cost
(13.48-24.02 ms/op); the two ranges overlap, consistent with the parse+emit
pass itself contributing negligible incremental cost on top of the dominant
subprocess spawn (the parser's own no-regression evidence in
`go/internal/collector/submodule/README.md` already establishes `Parse` and
`Emit` as sub-microsecond pure functions).

This cost is a `fork`+`exec` of the `git` binary -- process creation,
dynamic-linker startup, git's own internal init -- not an I/O-bound tree
walk or CPU-bound parse: `gitSubmoduleGitlinkSHA`'s own doc comment notes it
reads a single committed tree entry, which is cheap once the process is
already running. Subprocess-spawn overhead is what this benchmark measures,
and it should not be reported as negligible the way the per-file check
above is.

### Bound and gate

This cost is:

- **Gated**: it runs only when a repository has a `.gitmodules` file with
  parsed entries. `emitSubmoduleFactsForCandidates` returns before ever
  touching `repoPath` when no `.gitmodules` body was accumulated during
  content streaming -- see the "NoSubmodule" row above, which pays none of
  this cost.
- **Bounded by submodule count, linearly**: N declared submodules in one
  repository's `.gitmodules` cost roughly N x [15.50, 20.96] ms, since
  `gitSubmoduleGitlinkSHA` runs once per entry (`submodule.Emit`'s doc
  comment) with no batching across entries. A repository declaring 10
  submodules would add on the order of 155-210 ms to that repository's
  fact-emission phase from this cost alone.
- **Bounded relative to a repository's overall snapshot cost for the common
  case**: the collector's own benchmarks
  (`BenchmarkNativeRepositorySnapshotterFullFixture`) measure full-repository
  snapshotting in the tens-to-hundreds-of-milliseconds-or-more range for
  real repositories with more than a handful of files, so a few tens of
  milliseconds for a repository's (uncommon) submodule declarations adds a
  real but bounded amount, not a dominant one -- unless a repository
  declares a large number of submodules (tens or more), in which case this
  cost should be revisited before shipping such a repository through Eshu.

## No-Observability-Change

This is a benchmark-only addition. It adds no metric, span, log, worker,
queue domain, or runtime knob; it exercises functions that already exist and
are already covered by `internal/collector/submodule`'s own parser and
emitter unit tests (`parser_test.go`, `emitter_test.go`),
`git_submodule_facts_test.go`, `git_submodule_pinned_sha_test.go`
(issue #5420 Phase 2b), and `internal/collector`'s existing content-stream
integration tests.
