# prod-iac-resource-list — production validation

Capability: `iac_inventory.resources.list` (tool `list_iac_resources`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 3000`,
`max_truth_level: exact`.

## Claim validated

Bounded keyset-paginated list over the authoritative Terraform/IaC graph
projection, anchored on a single label, with deterministic ordering, limit
validation, truncation-and-cursor accounting, and module/type filters.

## Committed reproducible evidence

**Handler contract, pagination, and filters** —
`go/internal/query/iac_resources_test.go`:
`TestIaCResourcesHappyPath`, `TestIaCResourcesEmpty`,
`TestIaCResourcesLimitValidation`, `TestIaCResourcesDefaultLimitWhenAbsent`,
`TestIaCResourcesTruncationAndCursor`, `TestIaCResourcesFilters`, and
`TestIaCResourcesModuleFilterIncludesQuotedAddresses`. Reproduce:

```bash
cd go && go test ./internal/query -run TestIaCResources -count=1
```

**Backend-unavailable honesty** —
`go/internal/query/iac_resources_test.go`:
`TestIaCResourcesReturns503WhenGraphMissing`.

## Notes

No private data: cited tests use synthetic Terraform/IaC graph fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
