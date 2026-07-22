# prod-supply-chain-impact-aggregate — production validation

Capability: `supply_chain.impact_findings.aggregate` (tools
`count_supply_chain_impact_findings`, `get_supply_chain_impact_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_cve_package_repository_or_digest_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded reducer impact aggregate (count and grouped inventory) over reducer
impact facts, replacing a page-and-iterate caller workflow for ecosystem-
totals questions.

## Committed reproducible evidence

**Aggregate rollup contract, canonical-finding counting, and scope anchors** —
`go/internal/query/supply_chain_impact_aggregates_test.go`:
`TestSupplyChainImpactAggregateRoutesReturn503WhenStoreMissing`,
`TestSupplyChainImpactAggregateCountReturnsTotals`,
`TestSupplyChainImpactAggregateQueriesCountCanonicalFindings`,
`TestSupplyChainImpactAggregateQueriesKeepActiveScanAnchor`, and
`TestSupplyChainImpactAggregateQueriesUseListProfileAndSuppressionPredicates`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestSupplyChainImpactAggregate -count=1
```

## Notes

No private data: aggregate rows carry counts and bucket labels only.

Related: #5552 (burn-down).
