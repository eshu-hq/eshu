# prod-structural-inventory — production validation

Validation-Slug: prod-structural-inventory
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_inventory.structural passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: code_inventory.structural -> mcp:inspect_code_inventory

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `code_inventory.structural` (tool `inspect_code_inventory`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: multi_repo_platform`, `p95_latency_ms: 1200`,
`max_truth_level: derived`.

## Claim validated

Bounded content-index structural inventory for entities, top-level rows,
dataclasses, documented functions, decorators, class methods, super calls, and
function counts per file; broad scans stay capped and paged.

## Committed reproducible evidence

**Handler-level inventory shapes and readiness gating** —
`go/internal/query/code_structural_inventory_test.go`:
`TestCodeHandlerStructuralInventoryReturnsBoundedDataclasses`,
`TestCodeHandlerStructuralInventoryReturns503UntilSubstringIndexesReady`,
`TestCodeHandlerStructuralInventoryFindsClassesWithMethod`,
`TestCodeHandlerStructuralInventoryCountsFunctionsPerFile`, and
`TestCodeHandlerStructuralInventoryRejectsInvalidBounds`. Reproduce:

```bash
cd go && go test ./internal/query -run TestCodeHandlerStructuralInventory -count=1
```

**Bound/scope/where-clause validation** —
`TestStructuralInventoryValidationRejectsOverMaxLimit`,
`TestStructuralInventoryValidationRequiresScope`,
`TestStructuralInventoryValidationRejectsNonFunctionFileCounts`,
`TestStructuralInventoryWhereUsesLanguageVariants`,
`TestStructuralInventoryWhereGuardsUnscopedSuperCallSearch`,
`TestStructuralInventoryWhereHonorsClassNameForClassWithMethod`,
`TestStructuralInventoryWhereRestrictsTopLevelToFunctionsAndClasses`, and
`TestStructuralInventoryWhereMatchesObjectDecorators`. Reproduce:

```bash
cd go && go test ./internal/query -run TestStructuralInventory -count=1
```

## Notes

No private data: cited tests exercise fixture repository/file structures only.

Related: #5552 (burn-down).
