# prod-replatforming-rollups — production validation

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
