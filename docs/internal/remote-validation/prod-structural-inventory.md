# prod-structural-inventory — production validation

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
