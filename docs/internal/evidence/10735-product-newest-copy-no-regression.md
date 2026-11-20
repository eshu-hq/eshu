# 10735: product-path newest-copy admission costs no regression

Issue eshu-hq/eshu#10735 (binding ruling 2026-11-17), part of #9209. The NVD
product path now elects one winner per (source, advisory id) before matching
and keeps a product only when its own scope and generation holds an admitted
copy. This note records what the change costs on the measured path.

## What the change does

`appendSupplyChainImpactIndexFindings` (go/internal/reducer/supplychain/core/impact_classify.go)
builds the per-CVE product admission from `newestCopyAdmittedProductScopes`
(go/internal/reducer/supplychain/core/newest_copy.go): one winner election per
(source, advisory id) over the group's native observations, plus one map
lookup per product. A product whose scope holds no admitted copy is dropped
before classification, so it never reaches the SBOM match, remediation,
priority, or coalesce stages. `AffectedProduct` gains two string fields
(scope, generation) projected from its envelope; no fact contract changes.
No new lock, batching, knob, I/O, or retry path.

## Cost

No-Regression Evidence: `BenchmarkProductPathNewestCopyAdmission` (200
two-copy NVD groups, 400 anchored products, full `BuildSupplyChainImpactFindings`,
`-benchtime=10x -count=3`) and the pre-existing
`BenchmarkBuildSupplyChainImpactIndexAffectedProducts` (5000 single-copy NVD
matches, unaffected path, `-benchtime=10x -count=2`), run sequentially on
go1.26.6 linux/arm64 (shared host, so allocations carry the weight; timings
are not a CI baseline). Base is `86b2adcfa7` (this branch's base); after is
the #10735 commit on top of it. Row counts: the admission benchmark derives
400 findings before and 200 after (the dropped stale products are the fix);
the index benchmark derives the same finding count both ways.

| benchmark | base ns/op | after ns/op | base B/op | after B/op | base allocs/op | after allocs/op |
|---|---|---|---|---|---|---|
| ProductPathNewestCopyAdmission | 8,591,453–8,880,081 | 5,889,168–6,013,400 | 8,643,822 | 5,471,722 | 57,586 | 38,577 |
| IndexAffectedProducts/none_negated | 12,002,381–12,214,735 | 11,677,096–11,706,227 | 16,341,452 | 16,500,892 | 110,351 | 110,351 |
| IndexAffectedProducts/quarter_negated | 12,636,896–12,849,692 | 11,293,583–11,804,400 | 16,161,249 | 16,281,219 | 109,100 | 109,100 |

The admission benchmark is faster after because 200 losing products never
classify; the per-finding work is unchanged. The unaffected single-copy path
is within host noise on time with identical allocs; bytes/op rises ~1% there
(about two string headers per product row for the new scope fields), which is
the expected width of the two added struct fields and scales with the product
count the loader already bounds. No per-unit regression on either path.

Observability Evidence: the per-pass dropped count rides the
`Result.SubSignals` key `products_dropped_by_newest_copy` (0 when none, so a
clean pass is visible), asserted by
`TestProductPathReportsDroppedProducts` (drops, clean-pass zero, and both
guards), and the vulnerability-index-build row of
`docs/public/observability/telemetry-coverage-reducer-domains.md` names it
(net-zero rows; no new instrument or label).
`scripts/verify-telemetry-coverage.sh` passes on the change.
