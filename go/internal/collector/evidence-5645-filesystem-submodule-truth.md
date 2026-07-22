# Filesystem Submodule Truth Evidence (#5645)

## Classification and invariant

This is a correctness-blocker repair, not an optimization. Filesystem source
mode must preserve a repository's root `.gitmodules` content in its managed
copy and must read each declared submodule's gitlink SHA from the original Git
checkout. The managed copy intentionally excludes `.git`, so using it for
`git ls-tree` silently loses pinned-commit truth.

The correctness delta is explicit: the same B-7 corpus changes from zero
`submodule.pin` facts and zero `PINS_SUBMODULE` edges to a non-vacuous
`PINS_SUBMODULE` edge with `rc-161` green. Other graph and query assertions
must remain unchanged.

## Performance Evidence:

The candidate adds one bounded map construction over the selected repository
set, up to four canonical local-path normalizations per selected repository
(source, managed target, selected-path lookup, and the snapshot's non-empty
`GitTreePath`), and copies one small root control file when it exists. It does
not change worker counts, queue behavior, batch sizes, graph-write concurrency,
or retry policy.

Comparable local B-7 runs used clean volumes, the same 25-repository corpus,
the `local_full_stack` profile, Postgres `18-alpine`, and NornicDB source commit
`1492458852588c884c32f70d27ea2ee07086769c`. Both ran on an arm64 Mac16,11
with 12 logical CPUs, 64 GiB memory, and Docker 29.4.0.

| Metric | Before (`2a2bcadae`) | After (`828235eaa`) | Delta |
| --- | ---: | ---: | ---: |
| B-7 collector phase (`collect` start to collector settle) | 20s | 20s | 0s (0%) |
| Bootstrap phase | 3s | 3s | 0s (0%) |
| First queue drain | 6s | 6s | 0s (0%) |
| Maintenance drains | 7s | 6s | -1s (timing noise; no speedup claimed) |
| Graph/query phase | 2s | 3s | +1s (within the gate's +5s advisory band) |

Target contribution budget: the accepted collector-phase baseline is 20s;
this repair targets no speedup and allows no regression larger than 10% (2s)
or 60s. The observed collector-phase delta is 0s, so the change consumes none
of that budget. The next long pole remains the gate's fixed 20s collector
settle interval; this PR does not claim to improve it.

## Benchmark Evidence:

The full built-binary corpus run is the representative measurement because the
behavior crosses filesystem staging, repository selection, snapshotting, fact
commit, reducer projection, and graph readback. Focused unit proof additionally
covers managed-copy `.gitmodules` preservation and source-tree gitlink lookup;
no microbenchmark is substituted for that end-to-end correctness path.

## No-Regression Evidence:

Before the repair, all three drain checkpoints reached terminal state with
zero residual fact work, zero required nonterminal shared-projection intents,
and zero dead-letter work, but `rc-161` and the `PINS_SUBMODULE` snapshot floor
failed at count 0. After the repair, the same queue terminal counts remain zero
and B-7 reports 472 passes, zero required failures, and zero advisory warnings
in 35s. The Kubernetes namespace assertion `rc-158` also remains green.

Focused proof:

```text
go test ./internal/collector -run 'TestNativeRepositorySelectorSelectRepositoriesFilesystemPreservesGitlabCI|TestNativeRepositorySelectorSelectRepositoriesFilesystemRootRepository|TestEmitSubmoduleFactsForCandidatesResolvesPinnedSHA' -count=1
go test ./internal/collector -count=1
go test ./cmd/golden-corpus-gate -count=1
bash scripts/test-verify-golden-corpus-gate.sh
bash scripts/verify-golden-corpus-gate.sh
```

## Observability Evidence:

Repository snapshot duration and stage attribution remain visible through
`eshu_dp_repo_snapshot_duration_seconds` and
`eshu_dp_collector_snapshot_stage_duration_seconds`. Fact volume and commit
outcomes remain visible through `eshu_dp_facts_emitted_total`,
`eshu_dp_facts_committed_total`, and `eshu_dp_fact_batches_committed_total`.

## No-Observability-Change:

The repair adds no pipeline stage, metric, span, log key, status surface, or
label value. `GitTreePath` only selects the correct local checkout for the
existing `git ls-tree` read, and the helper file contains existing snapshot
worker/parser defaulting moved for the file-size cap. The existing signals
above continue to cover the same collector chokepoints.
