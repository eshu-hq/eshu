# prod-replatforming-plan-readiness — production validation

Validation-Slug: prod-replatforming-plan-readiness
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: replatforming.plan.readiness passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
