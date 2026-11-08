# 10042: the newest advisory copy decides

Issue #10042, part of epic #10037. Copies of one advisory in several scopes
shared one electorate: an older active copy blocked the retraction a newer
withdrawn or Rejected copy should cause, and load order picked which copy
supplied severity, fixed version, and range. The reducer now reduces CVE facts
to the newest copy per (payload source, advisory id) before provenance
selection, and pairs each package with the CVE fact of its own scope and
generation.

## What the change does

`newestCopyAdvisoryInputs` (new file `newest_copy.go`) elects the greatest
`source_updated_at` per key (empty or unparseable ranks oldest, ties break on
the greater fact id) and drops a superseded copy's packages only when the
winning copy names a package of its own. It runs inside
`buildAdvisoryProvenanceObservations` (package and CPE paths) and at the top of
`baseSupplyChainImpactFinding`, so version matching reads the same group. A
duplicate-key guard returns typical distinct-key groups untouched and without
allocating.

No-Regression Evidence: `BenchmarkBuildFindingsMultiSourceGroups` (500 groups
of 3 sources: NVD, GHSA, OSV-carried GHSA), go1.26.6 linux/arm64, base
`70c127e8a8` vs this head, `-benchtime=3x -count=3`:

| Sub-benchmark | Base ns/op | Head ns/op | Base allocs/op | Head allocs/op | Base B/op | Head B/op |
|---|---|---|---|---|---|---|
| live | 12260k | 12250k | 121131 | 121129 | 14.13M | 14.50M |
| all_rejected | 12910k | 12990k | 132129 | 132127 | 14.94M | 15.35M |

Time and alloc counts are at parity; bytes per op rise ~2.6% with identical
alloc counts (larger same-count allocations from the added branch, no new
allocation sites on this input shape). The change is a pure per-group
function: no locks, queues, leases, I/O, or shared state, so no contention or
ordering proof applies beyond the load-order permutation tests in the RED
table.

No-Observability-Change: the election emits no metric, span, or log of its
own. The stage runs inside the standard `supply_chain_impact` reducer
execution covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`, and its decisions stay visible through
`eshu_dp_supply_chain_impact_findings_total`,
`eshu_dp_supply_chain_impact_findings_retracted_total`, and
`eshu_dp_supply_chain_suppression_decisions_total` (see
`telemetry-coverage-reducer-domains.md`, projection (supply-chain)).
