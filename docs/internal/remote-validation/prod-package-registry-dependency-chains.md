# prod-package-registry-dependency-chains — production validation

Validation-Slug: prod-package-registry-dependency-chains
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: package_registry.dependency_chains.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: package_registry.dependency_chains.list -> http:GET /api/v0/package-registry/dependency-chains?repository_id=orders-api&limit=10

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `package_registry.dependency_chains.list` (tool
`list_package_registry_dependency_chains`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: repository_id`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Two-read repo-scoped join of canonical consumption correlations with
provenance-only publication/ownership correlations; publisher legs are
inferred provenance-only links and are never asserted as `Repository` edges;
no-publisher and ambiguous-publisher cases are surfaced explicitly.

## Committed reproducible evidence

**Join logic, terminal/ambiguous states, self-publisher exclusion** —
`go/internal/query/package_registry_dependency_chains_test.go`:
`TestResolvePackageDependencyChainsJoinsConsumerToPublisher`,
`TestResolvePackageDependencyChainsKeepsNoPublisherTerminal`,
`TestResolvePackageDependencyChainsMarksMultiplePublishersAmbiguous`,
`TestResolvePackageDependencyChainsPhase2FiltersPublisherKinds`,
`TestResolvePackageDependencyChainsDropsSelfPublisher`. Reproduce:

```bash
cd go && go test ./internal/query -run TestResolvePackageDependencyChains -count=1
```

**Handler-level bounds** —
`go/internal/query/package_registry_dependency_chains_handler_test.go`.

## Notes

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
