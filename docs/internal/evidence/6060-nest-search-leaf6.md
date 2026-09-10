# 6060 nesting leaf 6: search leaf (entity search + enrichment)

## What moved

`global_name_search.go` thinned to the `searchGlobalEntityNames`
method plus its five grandfathered-support shims (untouched — they
keep relationships pinned bodies byte-identical).
`search_metadata.go` slimmed to the two enrich forwarders, the
`ResultContentEntityType` seam wrapper, and the shared
`cloneQueryAnyMap`, then renamed to `result_enrichment.go` (dirgate
stutter rule) with its test following.

The `search/` leaf owns global entity-name search (`names.go`),
result enrichment and the metadata merge (`enrich.go`), with the doc
trio and merge contract tests. The five dead-code investigation
next-call shapers moved to `deadcode/next_calls.go` as
`InvestigationNextCalls`, which owns them. The analyzer DI now points
at `deadcode.InvestigationNextCalls` and `search.MergeMetadata`.

No queryplan pins name this family (`searchGraphEntitiesWithExact`
stays untouched in `handler.go`).

## No-Regression Evidence

- `go test ./internal/query/codequery/search/
  ./internal/query/codequery/deadcode/ -count=1` — ok.
- `go test ./internal/query/... ./internal/queryplan/ -count=1` —
  35 packages ok, no FAIL.
- `go vet` clean on the default build.
- Scoped `precommit-go.sh lint` — 0 issues; `verify-dirgate.sh
  --all`, `verify-package-docs.sh`, `verify-doc-citations.sh` — green.

## No-Observability-Change

No telemetry, span, metric, or log line changed; the leaf adds no
instrumentation of its own.
