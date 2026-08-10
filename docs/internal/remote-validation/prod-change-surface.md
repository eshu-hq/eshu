# prod-change-surface — production validation

Validation-Slug: prod-change-surface
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.change_surface passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: platform_impact.change_surface -> mcp:find_change_surface

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.change_surface` (tools `find_change_surface`,
`investigate_change_surface`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 7000`, `max_truth_level: exact`.

## Claim validated

Hybrid graph-plus-infra impact traversal from a changed target, bounded by depth and truncation,
with deterministic confidence dedup and NornicDB-safe query shapes.

## Committed reproducible evidence

**Bounded traversal, resolution, and truncation** — `go/internal/query/impact_change_surface_investigation_test.go`:
`TestInvestigateChangeSurfaceReturnsAmbiguityWithoutTraversal`,
`TestInvestigateChangeSurfaceUsesBoundedTraversal`,
`TestInvestigateChangeSurfaceResolvesBareServiceNameByCanonicalWorkloadID`,
`TestInvestigateChangeSurfaceMarksRawGraphTruncationBeforeEnvironmentFilter`, and
`TestInvestigateChangeSurfaceAcceptsCodeTopicAndChangedPaths`. Reproduce:

```bash
cd go && go test ./internal/query -run TestInvestigateChangeSurface -count=1
```

**Legacy traversal correctness (dedup, depth clamp, truncation)** —
`go/internal/query/impact_change_surface_legacy_test.go`:
`TestChangeSurfaceTraversalQueriesAreNornicDBSafe`,
`TestFindChangeSurfaceImpactRowsDedupsConfidenceStably`,
`TestFindChangeSurfaceClampsMaxDepth`,
`TestFindChangeSurfaceReportsTruncationWithOverfetch`, and
`TestFindChangeSurfaceUnresolvableTargetReturnsEmpty`. Reproduce:

```bash
cd go && go test ./internal/query -run TestFindChangeSurface -count=1
cd go && go test ./internal/query -run TestChangeSurfaceTraversalQueriesAreNornicDBSafe -count=1
```

**Scoped-token authorization and cross-tenant filtering** —
`go/internal/query/auth_scoped_routes_impact_change_surface_test.go`:
`TestFindChangeSurfaceScopedGrantAndDeny`, `TestInvestigateChangeSurfaceScopedGrantAndDeny`,
`TestAuthMiddlewareWithScopedTokensAllowsChangeSurfaceFamily`, and
`TestInvestigateChangeSurfaceScopedFiltersCrossTenantTopicEvidence`. Reproduce:

```bash
cd go && go test ./internal/query -run "TestFindChangeSurfaceScopedGrantAndDeny|TestInvestigateChangeSurfaceScopedGrantAndDeny|TestAuthMiddlewareWithScopedTokensAllowsChangeSurfaceFamily|TestInvestigateChangeSurfaceScopedFiltersCrossTenantTopicEvidence" -count=1
```

**NornicDB rewrite proof** — `docs/internal/evidence/5287-change-surface-nornicdb.md` (#5287)
documents the before/after Cypher rewrite of both `changeSurfaceImpactRows` (investigate) and
`findChangeSurfaceImpactRows` (legacy `find_change_surface`) from multi-clause `MATCH ... MATCH
path=...` shapes the pinned NornicDB build mis-executes, to a single anchored
`MATCH path = (start:Label {id:$target_id})-[*1..N]->(impacted)` clause.

**Contract declaration** — `go/internal/query/openapi_change_surface_test.go`:
`TestOpenAPIChangeSurfaceInvestigationDocumentsInputFamilies`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPIChangeSurfaceInvestigationDocumentsInputFamilies -count=1
```

## Notes

No private data: fixtures use synthetic service/repository targets; the NornicDB evidence doc
describes query-shape rewrites only, no hostnames or credentials.

Related: #5552 (burn-down).
