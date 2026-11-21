# Supply Chain Impact C5 Loop Evidence

This page records the local benchmark and correctness proof for issue #10972,
C5 fixes: group consumption rows once per package (fix A,
`appendSupplyChainImpactManifestFindings`) and hoist the affected-match build
out of the manifest-dependency loop (fix B,
`supplyChainImpactParserFileRepositoryIDs`). Both are accuracy-neutral
CPU/alloc cuts to the `supply_chain_impact` handler path; neither changes wire
contracts, queue behavior, round limits, or telemetry.

The evidence uses synthetic repository identifiers and generated fixtures only.
It includes no private target names, IP addresses, repository paths, credentials,
tenant IDs, or provider response payloads.

Benchmark Evidence: `go test -bench -benchmem -count=3` on the real functions
(`BenchmarkAppendSupplyChainImpactManifestFindings10972`,
`BenchmarkSupplyChainImpactParserFileRepositoryIDs10972` in
`go/internal/reducer/supplychain/core`), before commit `a39ed183f4` baseline
versus after fix commits `54d04f73b7` (fix A) and `b226da4565` (fix B).

Reference host: linux/arm64, go1.26.6. The QA pprof term these remove is
`consumptionRowsForRepository` 19.7% plus `affectedPackageConsumptionKeys`
19.0% of reducer CPU on the deployed pin `664cd7533d` (issue #10972, C5).

## Fix A: group consumption rows once

Baseline walks every consumption row of a package once per consuming
repository (rows x repos). The fix partitions rows by repository in one pass
and iterates the sorted keys over the same per-repo scoped index.

Input shapes are repos/rows per package: 200/400, 351/702, 351/3510,
813/8130. 351 is the QA count of repositories with JS/TS files; 813 is the
repository-fact count one run loads.

| Shape | Before (ms/op) | After (ms/op) | B/op before -> after |
| --- | --- | --- | --- |
| 200/400 | 6.4-10.3 | 1.7-2.8 | 3.16MB -> 3.16MB |
| 351/702 | 6.1-13.3 | 3.2-4.8 | 5.76MB -> 5.77MB |
| 351/3510 | 12.6-13.7 | 4.3-7.5 | 9.09MB -> 9.09MB |
| 813/8130 | 55.9-61.6 | 17.6-19.9 | 20.1MB -> 20.1MB |

B/op and allocs/op are unchanged: per-repository classification cost is
invariant and dominates the synthetic fixture, so the real-function ratio
(~2-4x) understates the removed walk. The walk itself is the QA-profiled
19.7% term; its cost is gone on every shape. Correctness is pinned by
`TestGroupConsumptionRowsByRepository10972` (index order, duplicates,
blank-row exclusion on raw IDs) and
`TestAppendManifestFindingsPerRepositoryIdentity10972` (per-repo identity,
sorted order, lockfile preference, determinism, single-repo and zero-row
shapes), with `consumption_selection_9725` passing unchanged.

## Fix B: hoist the affected-match build

Baseline rebuilt the affected-package match list
(`manifestAffectedPackageMatches`, i.e. `affectedPackageConsumptionKeys` per
package) once per manifest dependency. The fix builds it once per call and
reuses it; the callee is pure, the input is loop-invariant, the inner loop is
a membership test plus break, and the output stays sorted.

Input shapes are affected packages/manifest dependencies: 200/176 (QA
median), 200/727 (QA p90), 1000/727.

| Shape | Before | After | Ratio |
| --- | --- | --- | --- |
| 200/176 | 45.0-47.1 ms/op, 56MB, 607k allocs | 1.20-1.31 ms/op, 0.92MB, 10.6k allocs | ~37x |
| 200/727 | 186.9-211.7 ms/op, 231MB, 2.5M allocs | 3.55-3.61 ms/op, 2.1MB, 26.6k allocs | ~55x |
| 1000/727 | 972-1514 ms/op, 1.24GB, 12.4M allocs | 8.44-8.53 ms/op, 4.6MB, 48k allocs | ~115-177x |

Correctness is pinned by `TestSupplyChainImpactParserFileRepositoryIDs10972`
(same sorted IDs, blank/unmatched skips, no-npm nil case, 50x determinism
across Go map iteration orders) and the unchanged
`TestManifestDependencyKeySetIsSharedByBothSupplyChainPaths`.

## Commands

Run from `go/`:

```bash
go test ./internal/reducer/supplychain/... -count=1
go test ./internal/reducer/supplychain/core/ -run '10972|TestBuildSupplyChainImpactFindingsEmitsPerRepositoryManifestFindings9725|TestManifestDependencyKeySetIsSharedByBothSupplyChainPaths|TestDigestlessFleet|TestGoldenCassette' -count=1
go test ./internal/reducer/supplychain/core/ -run '^$' -bench '10972' -benchmem -count=3
go vet ./internal/reducer/supplychain/...
git diff --check
```

Suite result on this host: `supplychain/core`, `supplychain/core/cvss`, and
`supplychain/staged/audit` all `ok` with `-count=1`; `go vet` clean;
`git diff --check` clean. Docker-free golden floor
(`TestGoldenCassetteDebianOSPackageChainSynthesizesFinding` and siblings)
passes unchanged. The live B-7 replay is coordinator-owned and NOT_CHECKED
here.

No-Observability-Change: pure CPU/alloc cut, no contract change. No new
metrics, spans, logs, flags, env, schema, or docs contracts. Acceptance reads
existing signals only: `sub_duration_build_findings` (`diagnostics.go`),
handler duration, `eshu_dp_supply_chain_impact_pass_total` /
`fanout_total`, and post-deploy QA pprof shares of the two removed terms
(coordinator W0/W1). New helpers carry Go doc comments; package READMEs and
`AGENTS.md` files are unchanged.

Why this is safe: both fixes remove only redundant recomputation. Fix A
passes the identical per-repo scoped index the old per-repo walk produced
(proven row-for-row by the grouping tests); fix B reuses a loop-invariant
list the inner loop never mutates (read-only membership test). Full package
suite, golden fixture tests, and the 9725 regression pass without assertion
changes. Live drain acceptance (drain > inflow, canary drained, #10671
minting) still requires the deployed fixes #11081/#11082/#11075 and the W0/W1
protocol; this PR claims only the two measured loop wins.
