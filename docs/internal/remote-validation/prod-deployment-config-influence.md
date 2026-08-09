# prod-deployment-config-influence — production validation

Validation-Slug: prod-deployment-config-influence
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.deployment_config_influence passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.deployment_config_influence` (tool
`investigate_deployment_config`). Production profile:
`required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 6000`, `max_truth_level: exact`.

## Claim validated

Bounded service deployment evidence packet with portable file handles:
prompt-ready deployment-config files, service-story deployment evidence,
ambiguity handling (HTTP 409 for duplicate workload names), and fail-closed
completeness when upstream evidence is saturated or inconsistent.

## Committed reproducible evidence

**Handler contract, ambiguity, and disclosed truncation** —
`go/internal/query/deployment_config_influence_test.go`:
`TestBuildDeploymentConfigInfluenceResponseReturnsPromptReadyFiles`,
`TestInvestigateDeploymentConfigInfluenceReturns404ForUnknownService`,
`TestInvestigateDeploymentConfigInfluenceReturnsConflictForDuplicateWorkloadName`,
and `TestInvestigateDeploymentConfigInfluenceDisclosesSaturatedUpstreamEvidence`.
Reproduce:

```bash
cd go && go test ./internal/query -run DeploymentConfigInfluence -count=1
```

**OpenAPI contract lockstep** —
`go/internal/query/openapi_deployment_config_influence_test.go`.

**Fail-closed completeness contract (design evidence)** —
`docs/internal/evidence/5264-impact-deployment-graph.md`'s
"Deployment-config influence" section documents the coverage-propagation and
fail-closed-completeness rules this handler implements.

## Notes

No private data: cited tests use synthetic service/workload fixtures; no
production credentials or deployment-specific values appear in this
artifact.

Related: #5407 (artifact-existence gate), #5552 (burn-down).
