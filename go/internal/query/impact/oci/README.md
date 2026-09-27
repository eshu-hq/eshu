# OCI registry-truth helpers

## Purpose

`oci` holds the pure OCI registry-truth helpers behind the impact
deployment-trace handler family (#6590): parsing image refs into digest- and
tag-addressed keys (`SplitImageRefs`, `ImageRefDigest`), batching and
row-limit bookkeeping for the bounded tag-observation and image-by-digest
reads (`KeyBatches`, `AdvanceBoundedRead`, `RegistryTruthLimits`), and
shaping raw joined rows into truth rows (`BuildDigestTruthRows`,
`BuildTagTruthRows`, `IndexImagesByDigest`, `JoinImageRepository`,
`JoinTagRepositoryImage`). Nothing here issues a graph query: the three
Run-calling fetchers (`fetchOCIImageDigestRows`, `fetchOCIImagesByDigest`,
`fetchOCIImageTagRows`, `fetchOCIRepositoriesByUID`) and the two bounded
Cypher statements stay in
`go/internal/query/impact/trace_deployment_oci.go`, so the
query-source-coverage registry keeps attributing rows to the fetcher that
calls `Run`, not to this helper package.

## Ownership boundary

This package owns pure OCI registry-truth logic over plain `map[string]any`
rows. The `impact` package imports `oci`, never the reverse; neither imports
the query root or a graph driver. `oci` imports `deployment` for
`OciDigestMatchStrength` (the digest-addressed match_strength value), the
same directed edge `impact/ownership` already takes on `deployment`.

## Exported surface

The exported surface is described in [doc.go](doc.go). Every export exists
because `go/internal/query/impact/trace_deployment_oci.go`'s Run-calling
fetchers call it directly, or because a same-package test needs it.

## Dependencies

The package imports the Go standard library, `querycontract`, and
`impact/deployment` (for `OciDigestMatchStrength`). It must not import the
query root, `impact`, or a graph driver.

## Telemetry

This package emits no metrics, spans, or logs. The `impact` Handler retains
that telemetry (`eshu_dp_query_oci_registry_truth_truncated_total`,
reported from `(*Handler).reportOCIRegistryTruthTruncated`).

No-Observability-Change: this package is a pure helper extraction; the
telemetry call path is unchanged.

## Performance

`AdvanceBoundedRead`, `KeyBatches`, and the truth builders are pure
in-memory row shaping over an already-fetched page, not hot Cypher. No
benchmark delta is claimed for the extraction itself; see
`docs/internal/evidence/6590-oci-registry-truth-bound.md` for the row-limit
enforcement's own before/after measurement.

## Gotchas / invariants

- `AdvanceBoundedRead`'s irreducible-overflow check (`last == keys[0]`)
  requires its `keys` argument to be sorted ascending with its minimum
  first; every caller normalizes through `SortUniqueStrings` before
  batching (`distinctFieldValues`, used by some callers, is
  insertion-ordered, not sorted).
- A withheld ref never gets a placeholder row; `TruncatedImageRefs` /
  `RegistryTruthLimits` are the only place a caller sees it disclosed.
- Keep one home per symbol: no helper copies across `impact`, `oci`,
  `deployment`, and `querycontract`.

## Verification

From `go/`, run `go test ./internal/query/impact/... -count=1`, then
`go test ./internal/query/... ./internal/queryplan/... -count=1`, `go build
./...`, and `go vet ./...`. From the repository root, run
`scripts/verify-package-docs.sh`.

## Related docs

- [Source layout](../../../../docs/public/reference/source-layout.md)
- [HTTP API](../../../../docs/public/reference/http-api.md)
- [#6590 evidence](../../../../docs/internal/evidence/6590-oci-registry-truth-bound.md)
