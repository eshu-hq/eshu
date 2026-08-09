# prod-replatforming-rollups — production validation

Validation-Slug: prod-replatforming-rollups
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: replatforming.rollups.readiness passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: replatforming.rollups.readiness -> mcp:get_replatforming_rollups

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `replatforming.rollups.readiness` (tool
`get_replatforming_rollups`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: account_environment_or_service`, `p95_latency_ms: 5000`,
`max_truth_level: derived`.

## Claim validated

Bounded drift-and-readiness rollup by account, environment, and service over
the source-state taxonomy; preserves per-item source state, counts ambiguous
or missing attribution in explicit buckets, and never lets a rejected
finding win over supporting evidence.

## Committed reproducible evidence

**Bounded scope, empty-scope, source-state preservation, rejection
precedence, truncation** —
`go/internal/query/replatforming_rollups_handler_test.go`:
`TestReplatformingRollupsRequiresBoundedScope`,
`TestReplatformingRollupsUnsupportedProfile`,
`TestReplatformingRollupsEmptyScope`,
`TestReplatformingRollupsPreservesSourceStateAndReadiness`,
`TestReplatformingRollupsRejectedWinsOverEvidence`,
`TestReplatformingRollupsTruncationFlag`. Reproduce:

```bash
cd go && go test ./internal/query -run TestReplatformingRollups -count=1
```

**OpenAPI contract declaration** —
`go/internal/query/openapi_replatforming_rollups_test.go`:
`TestOpenAPISpecIncludesReplatformingRollups`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
