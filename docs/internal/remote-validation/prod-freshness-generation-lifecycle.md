# prod-freshness-generation-lifecycle — production validation

Capability: `freshness.generation_lifecycle` (tool
`get_generation_lifecycle`). Production profile:
`required_runtime: deployed_services`,
`max_scope_size: scope_repository_collector_source_generation_or_status`,
`p95_latency_ms: 1500`, `max_truth_level: exact`.

## Claim validated

Bounded `scope_generations` drilldown joined with per-generation
`fact_work_items` queue status and latest failure, covering active, pending,
superseded, and failed generation states with unknown scope/repository/
generation selectors returning explicit not-found instead of empty
confidence.

## Committed reproducible evidence

**Handler contract across every lifecycle state and not-found path** —
`go/internal/query/freshness_generations_test.go`:
`TestFreshnessGenerationLifecycleActive`,
`TestFreshnessGenerationLifecyclePendingMarksBuilding`,
`TestFreshnessGenerationLifecycleFailedCarriesFailure`,
`TestFreshnessGenerationLifecycleSupersededUnchanged`,
`TestFreshnessGenerationLifecycleUnknownScopeNotFound`,
`TestFreshnessGenerationLifecycleUnknownGenerationNotFound`, and
`TestFreshnessGenerationLifecycleBroadScanEmptyIsNotNotFound`. Reproduce:

```bash
cd go && go test ./internal/query -run TestFreshnessGenerationLifecycle -count=1
```

## Notes

No private data: cited tests use synthetic scope/generation fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
