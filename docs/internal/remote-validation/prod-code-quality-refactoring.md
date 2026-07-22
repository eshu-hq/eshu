# prod-code-quality-refactoring — production validation

Capability: `code_quality.refactoring` (tool `inspect_code_quality`).
Production profile: `required_runtime: deployed_services`, `max_scope_size: multi_repo_platform`,
`p95_latency_ms: 1800`, `max_truth_level: exact`.

## Claim validated

Bounded graph read over projected complexity, line count, and parameter count metrics, with
repo/language scope, limit, offset, and truncation.

## Committed reproducible evidence

**Long-function and argument-count inspection with handles** — `go/internal/query/code_quality_contract_test.go`:
`TestHandleCodeQualityInspectionFindsLongFunctionsWithHandles` and
`TestHandleCodeQualityInspectionFindsFunctionsByArgumentCount`. Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCodeQualityInspection -count=1
```

**Local-lightweight unsupported-capability guard** — same file:
`TestHandleCodeQualityInspectionLocalLightweightReturnsStructuredUnsupportedCapability`.
Reproduce:

```bash
cd go && go test ./internal/query -run TestHandleCodeQualityInspectionLocalLightweightReturnsStructuredUnsupportedCapability -count=1
```

**Contract declaration** — `go/internal/query/openapi_code_quality_test.go`:
`TestOpenAPISpecIncludesCodeQualityInspection` and
`TestOpenAPICodeQualityMinComplexityDoesNotAdvertiseConflictingDefault`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPICodeQuality -count=1
```

## Notes

No private data: fixtures use synthetic function/repository identifiers and metric values only.

Related: #5552 (burn-down).
