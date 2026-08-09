# prod-dead-iac — production validation

Validation-Slug: prod-dead-iac
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify_dead_iac_compose.sh
Validation-Command: COMPOSE_PROJECT_NAME=eshu-5552-dead-iac-20260809-5 NEO4J_HTTP_PORT=47074 NEO4J_BOLT_PORT=47087 ESHU_POSTGRES_PORT=47032 ESHU_HTTP_PORT=47080 JAEGER_UI_PORT=47086 OTEL_COLLECTOR_OTLP_GRPC_PORT=47017 OTEL_COLLECTOR_OTLP_HTTP_PORT=47018 OTEL_COLLECTOR_PROMETHEUS_PORT=47064 ESHU_API_METRICS_PORT=47164 ESHU_BOOTSTRAP_METRICS_PORT=47167 ESHU_MCP_PORT=47081 ESHU_MCP_METRICS_PORT=47168 ESHU_INGESTER_METRICS_PORT=47165 ESHU_RESOLUTION_ENGINE_METRICS_PORT=47166 bash scripts/verify_dead_iac_compose.sh >/tmp/eshu-5552-dead-iac-20260809-5.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: iac_quality.dead_iac returned ten materialized findings across Terraform, Helm, Ansible, Kustomize, and Compose through both deployed API and MCP surfaces.

## Fresh deployed validation

The uniquely named Compose run rebuilt the binaries, indexed ten public-safe
fixture repositories, and drained 151 work items to terminal success. The API
and MCP responses both reported ten materialized findings: one unused and one
ambiguous artifact in each of the five IaC families. The same run verified the
underlying Postgres reachability rows and the deployed NornicDB repository,
relationship, and deployment-evidence projections before tearing the stack
down with its volumes.

Capability: `iac_quality.dead_iac` (tool `find_dead_iac`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: bounded_repo_scope`, `p95_latency_ms: 10000`,
`max_truth_level: derived`.

## Claim validated

Bounded content-derived dead-IaC candidate scan; exact cleanup determinations
require reducer-materialized usage rows, which this profile does not claim —
the production contract is explicitly `derived`, not `exact`.

## Committed reproducible evidence

**Handler contract, materialized-row preference, and scope gating** —
`go/internal/query/iac_dead_test.go`:
`TestHandleDeadIaCPrefersMaterializedReachabilityRows`,
`TestHandleDeadIaCMaterializedRowsReportsPagination`, and
`TestHandleDeadIaCRequiresExplicitScope`;
`go/internal/query/iac_dead_derived_test.go`:
`TestHandleDeadIaCReturnsScopedDerivedFindings`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleDeadIaC -count=1
```

**Scoped-grant authorization** —
`go/internal/query/iac_dead_grant_test.go`:
`TestHandleDeadIaCScopedGrantRejectsOutOfGrantRepository` and
`TestHandleDeadIaCScopedGrantAllowsInGrantRepository`.

**Full-stack Docker Compose reachability run** —
`scripts/verify_dead_iac_compose.sh` seeds a fixture repository under
`tests/fixtures/product_truth/dead_iac`, runs the pipeline, and asserts the
API and MCP dead-IaC responses (`API_RESPONSE_FILE`, `MCP_RESPONSE_FILE`) and
the underlying reachability row counts against a live Compose stack.
Reproduce (requires Docker Compose):

```bash
scripts/verify_dead_iac_compose.sh
```

## Notes

No private data: cited tests and the Compose fixture use synthetic Terraform
content; no production credentials or deployment-specific values appear in
this artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
