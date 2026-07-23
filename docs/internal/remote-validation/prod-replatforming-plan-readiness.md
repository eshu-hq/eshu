# prod-replatforming-plan-readiness — production validation

Capability: `replatforming.plan.readiness` (tool
`compose_replatforming_plan`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: account_environment_or_service`, `p95_latency_ms: 5000`,
`max_truth_level: derived`. Provider-neutral source-state taxonomy rollup
over reducer-owned drift and IaC evidence; preserves provider-specific fact
names and per-item source state.

## Claim validated

Composes one bounded, truth-labeled replatforming plan over active AWS IaC
management and runtime-drift findings, refusing safety-gated findings,
carrying ambiguous-owner reasons, and never letting the rollup exceed the
underlying capability's proven truth level.

## Committed reproducible evidence

**Plan composition, safety gating, ambiguity, pagination, wave/blast-radius
ordering** — `go/internal/query/replatforming_plan_handler_test.go`:
`TestComposeReplatformingPlanReturnsReadyImportItem`,
`TestComposeReplatformingPlanRefusesSafetyGatedFinding`,
`TestComposeReplatformingPlanAmbiguousOwnerCarriesReasons`,
`TestComposeReplatformingPlanEmptyEvidenceIsBoundedAnswer`,
`TestComposeReplatformingPlanTruncatesAndPaginates`,
`TestComposeReplatformingPlanRequiresBoundedScope`,
`TestComposeReplatformingPlanUnsupportedProfile`,
`TestComposeReplatformingPlanOrdersWavesAndBlastRadius`. Reproduce:

```bash
cd go && go test ./internal/query -run TestComposeReplatformingPlan -count=1
```

**Contract validation and truth-level conservatism** —
`go/internal/query/replatforming_plan_contract_test.go`:
`TestReplatformingPlanValidateAcceptsWellFormed`,
`TestReplatformingPlanValidateRejectsContractViolations`,
`TestReplatformingPlanValidateAcceptsAmbiguousWithReasons`,
`TestReplatformingSourceStateTruthLevel`,
`TestReplatformingPlanRollupIsConservative`,
`TestReplatformingPlanRollupNeverExceedsCapabilityTruth`. Reproduce:

```bash
cd go && go test ./internal/query -run TestReplatformingPlan -count=1
```

**Wave-planning behavior** —
`go/internal/query/replatforming_plan_waves_handler_test.go`.

## Notes

FLAG (minor gap): the plan route (`compose_replatforming_plan`,
`go/internal/query/openapi_paths_replatforming.go`) has no dedicated OpenAPI
spec-inclusion test analogous to
`TestOpenAPISpecIncludesReplatformingRollups`; contract coverage here is via
the handler/contract tests above, not an OpenAPI-declaration assertion.

No private data: this artifact cites only committed tests, no
deployment-specific values.

Related: #5552 (burn-down), #5407 (artifact-existence gate).
