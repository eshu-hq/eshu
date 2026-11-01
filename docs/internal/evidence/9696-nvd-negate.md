<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Evidence: NVD negate flags in the supply-chain impact index (#9696)

The supply-chain impact index now decodes the four NVD logic flags on
`vulnerability.affected_product` and leaves a vulnerable CPE match out of the
index when its configuration or node is negated. The NVD collector also folds a
CPE match that repeats across configurations of one CVE into one fact, keeping it
negated only when every occurrence is negated. This note records what that costs
and what an operator can see.

Performance Evidence (#9696): measured against `origin/main` `b3499efe2d`, which
already carries the rc.12 pin (eshu#9757), so the larger per-match decode cost of
that pin is in the baseline and is not this change's. This change adds the four
model fields, the decode copy of them, and the negated-match skip.

Two states, built into test binaries from throwaway worktrees and run interleaved
(8 rounds, 2 runs per round and state, so 16 samples per row, rotating the order
each round): A is `b3499efe2d` plus only the benchmark and its two helpers; B is
`b3499efe2d` plus this change. `go test -c` of
`./internal/reducer/supplychain/core/`, then
`-test.bench BuildSupplyChainImpactIndexAffectedProducts` (index build, 20x) and
`-test.bench BuildSupplyChainImpactFindingsAffectedProducts` (whole in-process
build, 10x; no SBOM evidence), both with `-test.benchmem`
(`product_negate_bench_test.go`: 5000 CVEs with one NVD match each carrying all
four flag keys; "quarter" means every 4th match negated).

| Index build, 5000 matches | A: main | B: main plus this change |
| --- | --- | --- |
| none negated, allocs/op (median) | 75,120 | 75,120 |
| none negated, B/op (median) | 8,463,288 | 8,703,426 (+240,138, +2.8%) |
| quarter negated, allocs/op (median) | 75,120 | 73,869 (-1,251) |
| quarter negated, B/op (median) | 8,463,147 | 8,518,547 (+55,400, +0.7%) |
| none negated, ms/op (median; p25-p75) | 3.874 (3.771-3.924) | 3.857 (3.807-3.978) |
| quarter negated, ms/op (median; p25-p75) | 3.843 (3.768-3.894) | 3.794 (3.754-3.830) |

| Whole in-process build, 5000 matches | A: main | B: main plus this change |
| --- | --- | --- |
| none negated, allocs/op (median) | 155,142 | 155,143 |
| none negated, B/op (median) | 23,436,181 | 23,676,359 (+240,178, +1.0%) |
| quarter negated, allocs/op (median) | 155,141 | 135,141 (-20,000) |
| quarter negated, B/op (median) | 23,435,572 | 20,137,462 (-14.1%) |
| none negated, ms/op (median; p25-p75) | 11.371 (11.208-11.518) | 11.662 (11.551-11.737) (+2.6%) |
| quarter negated, ms/op (median; p25-p75) | 11.343 (11.265-11.468) | 9.727 (9.552-9.847) (-14.2%) |

Allocation counts and bytes per operation are deterministic to within a few units
(the table shows medians). Host load average during the runs was 10.6 to 11.6 for
the index build and 8.2 to 8.6 for the whole build, not a quiet host, so read the
times as direction with the spread shown, not as a quiet-host measurement.

- **Index build, none negated:** no added allocations and no measurable time
  change (the A and B interquartile ranges overlap); +240,138 B/op for the four
  model fields the decode copies. The index build runs once per
  `supply_chain_impact` intent and is linear in the match count.
- **Whole build, none negated:** about +2.6% time and +1.0% bytes. The B
  interquartile range sits above A's with no overlap, so I treat it as a real, small
  cost of copying four fields per match through grouping, not noise.
- **Every 4th match negated:** a negated match is not appended to the index, so it
  skips grouping and classification; the whole build is about 14% faster and
  allocates 20,000 fewer objects. Real NVD data may negate far fewer matches, so do
  not read that as the expected gain.

The intent's Postgres fact load comes first and was not measured. No Cypher,
queue, lease, or claim path is touched. A real-NVD-scale wall-time check of the
handler's `build_findings` phase in the QA environment is still owed and is not
claimed here.

Observability Evidence (#9696): `SupplyChainImpactHandler.Handle` sets
`Result.SubSignals["negated_affected_products_excluded"]` on every pass: the count
of vulnerable NVD matches the pass left out of the index because their
configuration or node is negated, 0 when none. It tells an operator that a pass
excluded negated matches. It is one count per pass with no CVE id, and it
includes matches for CVEs that would not have produced a finding anyway (a CVE
with package rows skips the product path; a match with no owned SBOM evidence
produces none). The per-CVE answer is in the stored fact's negate flags, which the
advisory evidence API returns. The signal adds no metric and no label. The test is
`TestSupplyChainImpactHandlerReportsNegatedProductsExcluded` in
`go/internal/reducer/supplychain/core/product_negate_signal_test.go`: it first
failed with `SubSignals["negated_affected_products_excluded"] missing`, asserts
the literal key, and checks 2, 1, and 0 for two negated, one negated among plain,
and no negated matches.

Composition with the retraction path of #9695: a CVE whose only product match is
negated builds no product finding, so the writer's replace-set retraction
tombstones the row an earlier pass left. `TestNegatedProductsRetractThePreviouslyWrittenFindingThroughTheWriter`
in `go/internal/reducer/supplychain/core/product_negate_retraction_test.go` runs two
passes through the real handler: pass 1 derives an `affected_derived` finding from a
plain match; pass 2 sees the same CVE with the match negated and runs through the
real Postgres impact writer over its transaction fake, with the pass-1 row in the
store. It asserts the excluded count is 1, no canonical write, the retraction
statement runs with a keep set that omits the pass-1 row, and the pass reports one
retracted finding. The fake does not execute SQL, so the tombstone row count is
injected as 1; the keep set the writer sends is what the test proves. The test
fails when the negated check is disabled (the count reads 0) and when the retraction
call is skipped (retracted reads 0).

Rollout note: facts stored before the collector folds repeated matches keep the
flags of whichever copy was written last. They correct themselves when the CVE is
next collected.
