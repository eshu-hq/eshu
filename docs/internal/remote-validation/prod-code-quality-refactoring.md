# prod-code-quality-refactoring — production validation

Validation-Slug: prod-code-quality-refactoring
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: code_quality.refactoring passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.
B12-Assertion: code_quality.refactoring -> mcp:inspect_code_quality

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


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
