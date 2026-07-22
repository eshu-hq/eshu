# prod-developer-change-plan — production validation

Capability: `platform_impact.developer_change_plan` (tool
`plan_developer_change`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 7000`, `max_truth_level: exact`. API and MCP accept
normalized changed-file records; local git ref resolution happens in the CLI
or caller.

## Claim validated

`plan_developer_change` builds a read-only `developer_change_plan.v1`
artifact from bounded pre-change impact evidence, with scoped-token
authorization applied before the underlying impact read.

## Committed reproducible evidence

**Handler contract** — `go/internal/query/prechange_impact_test.go`:
`TestDeveloperChangePlanBuildsReadOnlyActions`. Reproduce:

```bash
cd go && go test ./internal/query -run TestDeveloperChangePlan -count=1
```

**Scoped-token authorization** —
`go/internal/query/auth_scoped_routes_impact_change_surface_test.go`:
`TestPlanDeveloperChangeScopedRepoGrantAndDeny`.

**OpenAPI contract lockstep** —
`go/internal/query/openapi_change_surface_test.go`:
`TestOpenAPIDeveloperChangePlanDocumentsWorkflow`.

## Notes

No private data: cited tests use synthetic changed-file fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
