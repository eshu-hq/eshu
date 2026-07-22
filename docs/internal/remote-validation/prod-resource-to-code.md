# prod-resource-to-code — production validation

Capability: `platform_impact.resource_to_code` (tool
`trace_resource_to_code`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 7000`,
`max_truth_level: exact`. Exact only when infra and code relations
converge.

## Claim validated

Traces a resource back to its code repository, anchored on the
`impactAnchorLabelDisjunction` label set, with requested-limit bounds,
truncation reporting, and an explicit start-without-paths response when no
path exists.

## Committed reproducible evidence

**Anchoring, bounds, empty-path handling** —
`go/internal/query/impact_anchor_label_test.go`:
`TestTraceResourceToCodeAnchorsResolvedLabel`,
`TestTraceResourceToCodeReturnsStartWithoutPaths`; and
`go/internal/query/impact_legacy_bounds_test.go`:
`TestTraceResourceToCodeUsesRequestedLimitAndReportsTruncation`. Reproduce:

```bash
cd go && go test ./internal/query -run TestTraceResourceToCode -count=1
```

**Live NornicDB correctness fix** —
`docs/internal/evidence/5286-by-id-impact-anchors-nornicdb.md` (fixes the
`trace-resource-to-code` and `explain-dependency-path` by-id impact reads,
both anchored on `impactAnchorLabelDisjunction`, on the pinned NornicDB
backend).

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
