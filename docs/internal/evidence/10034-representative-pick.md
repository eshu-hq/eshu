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
