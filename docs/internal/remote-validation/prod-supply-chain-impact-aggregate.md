# prod-supply-chain-impact-aggregate — production validation

Validation-Slug: prod-supply-chain-impact-aggregate
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: supply_chain.impact_findings.aggregate passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
