# Searchnornicdb

## Purpose

`searchnornicdb` is the first internal NornicDB hybrid retrieval adapter for
issue #417. It converts NornicDB gRPC `SearchText` hits into
`searchretrieval.Candidate` values for explicit `SemanticContext` labels only.

This package is not a public API, MCP tool, graph query surface, or default
runtime path.

## Contract

The backend enforces these bounds before returning candidates:

- request validation from `searchretrieval`;
- `hybrid` mode only;
- explicit top-K limit, overfetched by one result only when still within the
  internal maximum so truncation can be observed;
- caller-provided context timeout through `searchretrieval.Runner`;
- NornicDB label filter set to `SemanticContext`;
- rejection of non-hybrid `search_method` values;
- rejection of `fallback_triggered=true`;
- rejection of hits that do not carry the `SemanticContext` label;
- rejection of hits outside the request's smallest scope anchor;
- per-candidate `derived` / `read_model` truth labels;
- stable graph handles for later bounded expansion.

NornicDB's pinned gRPC API does not expose pre-search property filters. This
adapter therefore performs a post-result scope check and does not claim full
issue #417 measured acceptance until a live projected corpus can prove that the
runtime search itself is pre-bounded or otherwise accepted by design.

## Telemetry

The adapter returns low-cardinality candidate metadata:

- `search_method`;
- `fallback_triggered`;
- `node_id`;
- `vector_rank`;
- `bm25_rank`.

`searchretrieval.Runner` records duration, mode, candidate count, result count,
truncation, timeout, failure class, and candidate truth-level counts. A future
runtime caller must bridge those observations into metrics, spans, or
structured logs without using high-cardinality IDs as metric labels.

## No-Regression Evidence

No runtime defaults, API contracts, MCP contracts, graph writes, reducer
projection queues, or schema migrations are changed by this package. Focused
tests prove the adapter sends only `SemanticContext` label requests, preserves
bounded limits, maps derived truth labels, rejects NornicDB fallback, rejects
label escapes, and rejects candidates outside the request scope.

## Observability Evidence

Adapter metadata plus `searchretrieval.Runner` observations cover duration,
mode, result count, truncation, timeout, failure class, and truth summaries for
the internal retrieval attempt. This branch does not wire a live OTEL exporter.

## Verification

```bash
go test ./internal/searchnornicdb -count=1
```

## Related Docs

- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/search-document-projection.md`
- `docs/public/reference/search-benchmark-evidence.md`
