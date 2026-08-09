# prod-container-image-list — production validation

Validation-Slug: prod-container-image-list
Validation-Tier: deployed_services
Validation-Date: 2026-08-09
Evidence-Kind: compose_e2e
Evidence-Source: scripts/verify-golden-corpus-gate.sh
Validation-Command: GATE_COMPOSE_PROJECT=eshu-5552-claim-honesty-20260809-9 ESHU_POSTGRES_PORT=44142 NEO4J_BOLT_PORT=44187 NEO4J_HTTP_PORT=44174 GATE_API_PORT=44180 GATE_MCP_PORT=44191 GATE_PROMETHEUS_SOURCE_PORT=44190 GATE_BUDGET_SECONDS=600 bash scripts/verify-golden-corpus-gate.sh --keep >/tmp/eshu-5552-b7-20260809-9.log 2>&1; echo $?
Validation-Exit-Code: 0
Capability-Assertion: platform_impact.container_image_list passed its non-vacuous capability-specific assertion through the deployed API or MCP surface.

## Fresh deployed validation

A fresh credential-free Compose run rebuilt every binary, replayed all 37 cassette scope generations, drained the projector and reducer to terminal state, and exercised the committed B-12 API/MCP assertion for this capability. The gate completed with 547 passes, zero required failures, and zero advisory warnings in 133 seconds.


Capability: `platform_impact.container_image_list` (tool `list_container_images`).
Production profile: `required_runtime: deployed_services`,
`max_scope_size: optional_digest_repository_or_tag_scope`, `p95_latency_ms: 1500`,
`max_truth_level: exact`.

## Claim validated

Bounded container image (OCI) list over the authoritative `(:ContainerImage)` graph with
deterministic ordering by digest then uid, limit+1 truncation, and offset continuation; optional
digest, `repository_id`, and tag filters.

## Committed reproducible evidence

**List handler happy path, limits, filters, and truncation** — `go/internal/query/images_test.go`:
`TestImageHandlerListHappyPath`, `TestImageHandlerListEmpty`,
`TestImageHandlerListLimitValidation`, `TestImageHandlerListDefaultsLimit`,
`TestImageHandlerListTruncationAndCursor`, `TestImageHandlerListFilters`, and
`TestImageHandlerBackendUnavailable`. Reproduce:

```bash
cd go && go test ./internal/query -run TestImageHandlerList -count=1
cd go && go test ./internal/query -run TestImageHandlerBackendUnavailable -count=1
```

**OCI repository-ID parsing** — same file: `TestSplitOCIRepositoryID`. Reproduce:

```bash
cd go && go test ./internal/query -run TestSplitOCIRepositoryID -count=1
```

**Contract declaration** — `go/internal/query/openapi_images_test.go`:
`TestOpenAPISpecIncludesContainerImageList`. Reproduce:

```bash
cd go && go test ./internal/query -run TestOpenAPISpecIncludesContainerImageList -count=1
```

## Notes

No private data: fixtures use synthetic image digests, refs, and repository IDs only.

Related: #5552 (burn-down).
