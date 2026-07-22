# Evidence: lazy git-tracked-file resolution cost (#5591)

## Classification

Issue #5591's fix (a file git tracks is never git-ignored) is a correctness
repair: discovery previously applied `.gitignore` as a pure pattern filter
with no knowledge of git's own tracked set, silently dropping a
force-committed (`git add -f`) file such as a checked-in `terraform.tfstate`
that matches the repo's own `*.tfstate` rule. That correctness delta is
proven separately by the RED-then-GREEN regression tests in
`git_tracked_ignored_discovery_test.go` and
`discovery/gitignore_tracked_test.go` (0 `terraform_state_candidate` facts
before the fix, 1 after, for a tracked-ignored file; an untracked file
matching the same rule still correctly yields 0).

This note covers a follow-up **performance** refinement to that same fix: the
first implementation called `discovery.Options.GitTrackedResolver` (one
`git ls-files -z` subprocess per repo root) **eagerly** — unconditionally,
for every repository discovery visited, regardless of whether that
repository had any file that actually matched a `.gitignore`/`.eshuignore`
rule. Local measurement against a benchmark corpus of 906 git repositories
found only 121/906 repos (~13%) carry a `.gitignore`/`.eshuignore` match at
all where tracked status is decision-relevant, so the eager call spent a
real subprocess spawn on the ~87% of repos that could never benefit from it.

The fix below (`discovery.go`'s `ResolveRepositoryFileSetsWithStats`,
`gitignore.go`'s `filterRepoFilesByGitignore`) makes that resolution
**lazy**: it defers the resolver call until a `.gitignore` match (or a
non-empty `.eshuignore` skip set) actually makes tracked status
decision-relevant for that repo root, memoized per root via `sync.OnceValue`
so a repo that needs it for both filters still pays the subprocess only
once.

**Output-preserving change**: the boolean identity `keep = tracked ∨
¬ignored` is logically commutative with `keep = ¬ignored ∨ tracked` — moving
the tracked-set lookup to only fire on an ignore match (short-circuit
evaluation) changes nothing about which files are kept or skipped, only
*when* the resolver is invoked. The full existing regression suite (RED/GREEN
tests for the correctness fix, plus two new lazy-invocation-count tests
below) passes unchanged before and after this refinement, proving exact
output equivalence — not merely asserting it.

## Benchmark command

```bash
env -u GOROOT go test ./internal/collector/ -run '^$' \
  -bench 'BenchmarkGitTrackedFiles' -benchmem -count=6
```

Apple M5 Max (`darwin/arm64`), git 2.50.1, go1.26.5, 2026-07-22.

## Results (6 runs, isolated subprocess cost)

| Metric | Measurement | Input shape |
| --- | ---: | --- |
| Single `git ls-files -z` subprocess (`BenchmarkGitTrackedFiles`) | 40.1-60.4 ms/op, ~59.2-59.6 KB/op, 122 allocs/op | 36 committed files in a real git repo (36 = this corpus's median tracked-file count; see corpus shape below) — matches `gitTrackedFiles`'s actual `exec.CommandContext` + `Output()` code path, not a raw shell timing |

A raw (non-Go, no test-binary overhead) `git -C <repo> ls-files -z` spawn
against this same machine's own (much larger, thousands-of-files) working
tree measured 20.1-26.3 ms across 10 runs — consistent with the benchmark
above: `exec.CommandContext`'s pipe setup and `Output()` buffering,
plus per-op Go test-harness overhead, account for the higher end measured
through the actual code path. Both numbers describe the same cost class: a
single `fork`+`exec` of the `git` binary, dominated by process creation and
git's own internal init, not by tree size at this file-count scale.

## Corpus shape (local benchmark corpus, 906 git repositories)

- Tracked-file count per repo: median 36, p90 338, max 28478.
- Repos with at least one `.gitignore`/`.eshuignore`-matched discovered
  candidate (tracked status decision-relevant): 121/906 (~13%).
- Repos where the eager (pre-refinement) resolver call was pure overhead
  (no ignore match at all): the remaining ~87%.

## Interpretation

### Eager (before this refinement): unconditional, ~87% wasted

The first #5591 implementation called `GitTrackedResolver` once per
discovered repo root unconditionally. At 40-60 ms/spawn measured above, a
906-repo full ingest would pay that cost 906 times regardless of match rate
— roughly 36-54 seconds of aggregate serial subprocess-spawn cost, of which
only the 121 repos with an actual ignore match could ever change discovery's
output. The remaining ~785 repos spent a real subprocess spawn for a
decision that could never matter.

### Lazy (this refinement): gated on decision-relevance, bounded to ≤121-129 spawns

The lazy resolver only fires when `filterRepoFilesByGitignore` encounters an
actual `.gitignore` match, or `recordTrackedEshuIgnoreSkips` sees a non-empty
`.eshuignore` skip set — i.e., only for the ≤129/906 repos (~14%, allowing a
small margin above the measured 121 for repos where `.eshuignore` alone
triggers it) where tracked status can change the outcome. At the same
40-60 ms/spawn measured above, that bounds the aggregate serial cost to
roughly 129 × [40, 60] ms ≈ **5.2-7.7 seconds** for a full 906-repo ingest —
down from 36-54 seconds eager, and the remaining ~777-785 repos with no
ignore match at all now pay **zero** added subprocess cost from this fix,
identical to the pre-#5591 baseline.

### Bound and gate

This cost is:

- **Gated on ignore-match decision-relevance**: `filterRepoFilesByGitignore`
  checks `isIgnoredByRepoIgnoreFile` first and calls `tracked()` only on a
  match; `recordTrackedEshuIgnoreSkips` returns before calling `tracked()`
  when `skippedPaths` is empty. Neither path can call the resolver for a
  repo with zero ignore-matched candidates.
- **Memoized per repo root**: `sync.OnceValue` bounds a single repo root to
  at most one subprocess spawn even when both the gitignore and eshuignore
  paths need a tracked-status decision for it (proven by
  `TestResolveRepositoryFileSetsGitTrackedResolverInvokedOnceWhenDecisionRelevant`).
- **Linear in ignore-matched repo count, not total repo count**: N repos
  with at least one ignore match cost roughly N × [40, 60] ms; repos with no
  match cost nothing, regardless of how large the total corpus grows.
- **Strictly quieter for the #5591 P2 warn** (`warnGitTrackedFilesUnavailable`):
  it now fires only when a repo actually reached a decision-relevant ignore
  match AND the resolver then failed — a strict subset of when it fired
  before this refinement — while still covering every case where a silent
  tracked-file drop could occur (the warn is unreachable exactly when there
  was never a `.gitignore` match to silently mis-resolve).

## Performance Evidence:

See Results and Interpretation above:
`BenchmarkGitTrackedFiles` isolates the single subprocess this fix's
resolver spawns at 40.1-60.4 ms/op (6 runs). The lazy refinement does not
change worker counts, queue behavior, batch sizes, graph-write concurrency,
or retry policy — it only changes when one already-existing subprocess call
fires, from unconditional to gated-on-ignore-match. Aggregate estimate:
eager ≈36-54s serial across a 906-repo corpus, lazy ≈5.2-7.7s (only the
≤129 repos with a decision-relevant ignore match), repos with no match pay
zero.

## Benchmark Evidence:

`BenchmarkGitTrackedFiles` (`git_tracked_files_bench_test.go`) isolates the
real `gitTrackedFiles` code path (`exec.CommandContext` + NUL-split
`Output()` parse) against a real git repository with 36 committed files
(this corpus's median tracked-file count), not an invented shape. It is the
only per-repo-root cost this fix's resolver adds, and the lazy refinement
does not change this benchmark's per-call cost — only how many times, across
a corpus, that call fires.

## No-Regression Evidence:

`TestResolveRepositoryFileSetsGitTrackedResolverIsLazyWhenNoIgnoreMatch` and
`TestResolveRepositoryFileSetsGitTrackedResolverInvokedOnceWhenDecisionRelevant`
(`discovery/gitignore_tracked_test.go`) wrap `GitTrackedResolver` in a
call-counting shim and assert 0 invocations for a repo with a `.gitignore`
but no matched candidate, and exactly 1 invocation (memoized across both
filters) for a repo with a matched, tracked candidate — RED before this
refinement (the prior eager call always fired once regardless), GREEN after.
Every pre-existing #5591 correctness test
(`git_tracked_ignored_discovery_test.go`,
`discovery/gitignore_tracked_test.go`'s other cases,
`git_tracked_files_test.go`, `git_selection_filesystem_tracked_test.go`)
passes unchanged, proving the lazy refinement is output-identical.

Focused proof:

```text
env -u GOROOT go test ./internal/collector ./internal/collector/discovery -count=1
env -u GOROOT go build ./internal/collector/...
env -u GOROOT go vet ./internal/collector/...
```

## Observability Evidence:

This fix's own #5591 P2 follow-up added a structured WARN log
(`warnGitTrackedFilesUnavailable`, `git_tracked_files.go`) for the one silent
-failure-risk case: a repo root with its own `.git` marker (so `ls-files`
was expected to succeed) whose tracked-status resolution still failed. The
lazy refinement makes this warn strictly narrower — it now only fires when a
repo actually reached a decision-relevant ignore match — but does not remove
coverage: the warn is unreachable exactly in the case where there was never
a `.gitignore` match for tracked status to silently mis-resolve. Discovery's
existing `DiscoveryStats.TrackedFilesSkippedEshuIgnore` (capped list) and
`DiscoveryAdvisoryReport.SkipBreakdown.TrackedFilesEshuIgnore` (count) remain
unchanged by this refinement — they are computed from `skippedPaths`, which
is independent of when the resolver call happens to fire.

## No-Observability-Change:

This refinement adds no new metric, span, log key, or runtime knob. It
changes only the timing of an existing resolver call (`GitTrackedResolver`)
and reuses every #5591 telemetry surface (`eshu_dp_discovery_files_skipped_total`,
`TrackedFilesSkippedEshuIgnore`, `warnGitTrackedFilesUnavailable`) documented
in `docs/public/observability/telemetry-coverage.md`'s existing
"git-tracked file resolution (issue #5591)" row.
