# #10034 item 1: representative-pick no-regression note

The pick for a mixed-ecosystem `(CVE, package id)` group used to rank under
the first fact's ecosystem, so the two fact orders named different facts.
`representativeRankingForGroup` (`go/internal/reducer/supplychain/core/`
#10034) ranks an agreed group under its ecosystem exactly as before, and a
disagreeing group under the vendor class its package identity names, through
`vendorClassPriorityRank` directly. `osPackageVendorFromPURL` is unchanged.

## Cost

No-Regression Evidence: the comparator still sorts one group copy with one
rank call per comparison; the only new work is one ranking derivation per
group, which hoists the per-fact ecosystem normalization out of the
comparator. Baseline `ec1643d67f` vs head `b72bca8a8b`,
`BenchmarkOSRepresentativeSelection` (500-fact envelopes per shape,
`-benchmem -count=6`, darwin/arm64 Apple M5 Max, go1.26.10):

| shape | allocs/op base | allocs/op head | B/op head vs base |
| --- | --- | --- | --- |
| joined | 172,639 | 172,637 | identical |
| reselected | 145,121 | 145,121 | identical |
| no-joinable | 79,590 | 79,590 | identical |
| language | 86,088 | 86,088 | identical |

`benchstat` reports `~` (p=1.000) on allocs/op and B/op for every shape:
zero new allocations on every measured path. Wall time on the loaded host
(load1 ~36, non-comparable per the timing-proof rules) read at or below the
base on every shape, so no wall-time regression is claimed or needed: the
verdict rests on the identical allocation profile and the unchanged
algorithmic order. Raw runs: `/tmp/bench-10034-base.txt`,
`/tmp/bench-10034-head.txt` (regenerable with the command above on a detached
base worktree and this branch).

## Observability

No-Observability-Change: no new metric, span, or log. Picks that move land in
the existing `os_representatives_*` outcome cells (`joined`, `reselected`,
`other_anchor`, `no_joinable`, `no_installed_row`) and the finding's existing
`MissingEvidence` reasons; `recordOSRepresentativeOutcome` is untouched.

## #10034 items 2-4 and the #9914-limit comments

RED first in every row: each test below failed on the tree before its fix and
passed after, with the full core package green at the end
(`go test ./internal/reducer/supplychain/core/ -count=1 -timeout 8m` ok;
`go test ./internal/reducer/...` ok except the environmental
`maintenance/census` pair, which needs `ESHU_DEPLOY_CHECKOUT` and never ran
here). All commands with `env -u GOROOT`.

| item | RED (failing proof) | fix | GREEN |
| --- | --- | --- | --- |
| 2 version family | `TestJoinedRowSuppliesTheVersionFamily`: purl family beat the joined row's own fact in one fact order | `version_family.go`: row package manager is host truth; purls must agree or the group stays unsupported | `TestJoinedRowSuppliesTheVersionFamily`, `TestForeignPURLDoesNotDecideTheVersionFamily`, `TestDisagreeingPURLsWithoutARowStayUnsupported` pass; `osVerdictComparator` mirrors the row rule |
| 3 purl-carry | declined: `os://` identities are legal and language facts may lack purl, so a purl mandate would strand them | distinct `no_joinable_identity` outcome (`representative_join.go`) separates "cannot meet the row" from "not installed" | `TestUnmeetableIdentityCountsItsOwnOutcome`, `TestKeylessIdentityCountsItsOwnOutcome` pass; README + `telemetry-coverage-reducer-domains.md` updated |
| 4 amazon + vendor token | `TestAmazonSpellingReadsTheRPMMatcher`: amazon rows missed the RPM matcher; unsupported reason named no vendor | `amazon` added to the rpm arms (`version_family.go`, deciding record, identity, remediation); per-vendor token via closed `osAdvisoryJoinVendorLabel` | `TestAmazonSpellingReadsTheRPMMatcher`, `TestUnsupportedEcosystemNamesItsVendor` pass |
| L1 dpkg spelling | `TestDpkgSpellingReadsTheOSRetractionRule`: dpkg facts missed the debian retraction arm | one-line class fix in `provenance_selection.go` | `TestDpkgSpellingReadsTheOSRetractionRule`, `TestDpkgSpellingKeepsTheRetractionAnchor` pass |
| L3 live consumer | `TestLiveConsumerDoesNotBeatARetractedRowJoin`: consumption on the live side anchored a retracted-row join | `onlyRetractedRecordsAnchor` no longer counts consumption on the live side (`os_advisory_upstream_group.go`) | `TestLiveConsumerDoesNotBeatARetractedRowJoin` pass |
| L2 different-vendor group | `TestDifferentVendorGroupJudgesAnchorPerShard`: a live ubuntu fact pinned a retracted debian group in every fact order | liveness filter + join narrowing run per shard inside `classifySupplyChainImpactPackage` (fleet only; SBOM digest scopes keep whole-group facts) | `TestDifferentVendorGroupJudgesAnchorPerShard` pass; intermediate full-package runs read 8 failures while narrowing sat before filtering or ran on SBOM scopes, green after it moved behind the filter with the fleet-only gate |

## Observability delta (items 2-4)

One new outcome cell: `no_joinable_identity`, with the
`os_representatives_no_joinable_identity` sub-signal, counted and emitted
beside the existing outcome cells in `recordOSRepresentativeOutcome`. No new
metric, span, or log beyond that cell; the unsupported-ecosystem reason now
carries the vendor token the finding and the counter agree on.

Observability Evidence: the new stage file
`go/internal/reducer/supplychain/core/version_family.go` is a pure function
stage with no emission of its own; it is covered by the existing
`eshu_dp_supply_chain_impact_os_representative_joins_total` outcome cells
(including the new `no_joinable_identity` cell and its sub-signal) in the
supply-chain projection row of
`docs/public/observability/telemetry-coverage-reducer-domains.md`, which now
names the file. No new metric, span, or log.

## Cost (items 2-4, L2)

No-Regression Evidence: the whole-index path reuses the naming's filter pair
instead of recomputing it, so a group that shards to nothing classifies
exactly the code the base ran; only a fleet shard pays one extra filtered
join scan on its own rows. Baseline `52e0cbae93` vs head `63999479ef`,
`BenchmarkBuildFindingsMultiSourceGroups` (500 three-source CVE groups per
shape, live and all_rejected, `-benchmem -count=6`, darwin/arm64 Apple M5
Max, go1.27.3):

| shape | allocs/op base | allocs/op head | B/op head vs base |
| --- | --- | --- | --- |
| live | 123.1k | 123.1k | identical |
| all_rejected | 134.1k | 134.1k | identical |

`benchstat` reports `~` (p=0.470 live, p=0.065 all_rejected) on allocs/op and
`~` on B/op for both shapes: zero new allocations on the measured path. Wall
time on the loaded host read at or below the base on both shapes
(-5.11% live, -5.83% all_rejected), so no wall-time regression is claimed or
needed here either: the verdict rests on the identical allocation profile and
the unchanged algorithmic order for the no-shard path. An intermediate
revision that re-filtered inside every classification showed +2.98% allocs/op
on all_rejected; the bisect (pre-L2 vs head) proved the L2 commit the sole
delta, and the reuse above removed it. Raw runs: `/tmp/bench-10034-L2-base.txt`,
`/tmp/bench-10034-L2-reuse.txt` (regenerable with the command above on a
detached base worktree and this branch). The finding count is unchanged: the
benchmark asserts a finding per group, and the full core package (including
every RED/GREEN test in the table above) is green.
