# prod-kubernetes-correlations — production validation

Validation-Slug: prod-kubernetes-correlations
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: kubernetes.correlations.list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: kubernetes.correlations.list -> mcp:list_kubernetes_correlations

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `kubernetes.correlations.list` (tool `list_kubernetes_correlations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: cluster_workload_namespace_image_or_digest_scope`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded reducer Kubernetes workload ownership and drift lookup anchored by
scope, cluster, workload object, namespace, image ref, or source digest,
reading the active fact read model with scope-grant filtering.

## Committed reproducible evidence

**Handler bounds and scoped-grant filtering** —
`go/internal/query/kubernetes_correlations_test.go`:
`TestKubernetesListCorrelationsRequiresScopeAndLimit`,
`TestKubernetesListCorrelationsUsesBoundedStore`,
`TestKubernetesListCorrelationsScopedEmptyGrantReturnsEmptyWithoutStoreRead`,
`TestKubernetesListCorrelationsScopedGrantHitsRealStoreAndReturnsRowData`,
`TestKubernetesListCorrelationsUnscopedQueryStaysUnfiltered`,
`TestKubernetesCorrelationQueryUsesActiveFactReadModel`,
`TestKubernetesCorrelationFilterRejectsUnboundedScope`. Reproduce:

```bash
cd go && go test ./internal/query -run TestKubernetesListCorrelations -count=1
cd go && go test ./internal/query -run TestKubernetesCorrelation -count=1
```

**Scoped-token grant filtering and performance/observability evidence** —
`docs/internal/evidence/5167-w6-scoped-cloud-routes.md` (#5167 W6 promotes
this route, among others, onto the scoped-token allowlist binding reads to
`AllowedRepositoryIDs`/`AllowedScopeIDs`).

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
