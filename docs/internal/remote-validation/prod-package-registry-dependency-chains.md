# prod-package-registry-dependency-chains — production validation

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
