# prod-resource-to-code — production validation

Validation-Slug: prod-resource-to-code
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.resource_to_code passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
