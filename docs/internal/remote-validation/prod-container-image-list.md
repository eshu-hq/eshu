# prod-container-image-list — production validation

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
