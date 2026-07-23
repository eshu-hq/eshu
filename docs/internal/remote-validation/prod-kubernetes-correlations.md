# prod-kubernetes-correlations — production validation

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
