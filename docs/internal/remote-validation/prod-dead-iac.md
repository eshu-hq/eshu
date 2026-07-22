# prod-dead-iac — production validation

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
