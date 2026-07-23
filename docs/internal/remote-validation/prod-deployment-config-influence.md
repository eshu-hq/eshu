# prod-deployment-config-influence — production validation

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
