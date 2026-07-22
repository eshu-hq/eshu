# prod-observability-coverage-correlations — production validation

Capability: `observability.coverage.correlations.list` (tool
`list_observability_coverage_correlations`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: scope_provider_signal_object_resource_or_service_scope_with_source_or_resource_class_filter`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded reducer observability coverage lookup anchored by scope, provider,
coverage signal, observability object, target resource, or target service;
optional `source_class`/`resource_class` filters narrow the anchored page.

## Committed reproducible evidence

**Handler bounds, scoped-grant filtering, class filters** —
`go/internal/query/observability_coverage_correlations_test.go`:
`TestObservabilityCoverageListCorrelationsRequiresScopeAndLimit`,
`TestObservabilityCoverageListCorrelationsUsesBoundedStore`,
`TestObservabilityCoverageListCorrelationsScopedEmptyGrantReturnsEmptyWithoutStoreRead`,
`TestObservabilityCoverageListCorrelationsScopedGrantHitsRealStoreAndReturnsRowData`,
`TestObservabilityCoverageListCorrelationsUnscopedQueryStaysUnfiltered`,
`TestObservabilityCoverageCorrelationQueryUsesActiveFactReadModel`,
`TestObservabilityCoverageListCorrelationsFiltersSourceAndResourceClass`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestObservabilityCoverage -count=1
```

**Scoped-token grant filtering and performance/observability evidence** —
`docs/internal/evidence/5167-w6-scoped-cloud-routes.md` (#5167 W6 promotes
this route, among others, onto the scoped-token allowlist).

## Notes

No private data: this artifact cites only committed tests and a committed
evidence note, no deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
