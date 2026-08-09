# prod-developer-change-plan — production validation

Validation-Slug: prod-developer-change-plan
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.developer_change_plan passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
