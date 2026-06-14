# Searchembed

## Purpose

`searchembed` owns local embedding implementations for the semantic and hybrid
search lane. The first implementation is a deterministic feature-hash embedder
that gives `searchhybrid` a credential-free vector arm for tests, local proofs,
and zero-key operation.

## Ownership boundary

This package owns local, no-network embedders behind the `searchhybrid.Embedder`
port. It does not own API or MCP routing, provider governance, hosted embedding
adapters, vector indexes, search-document projection, or canonical graph truth.

## Exported surface

- `DefaultDimensions` — the default vector width for the local hash embedder.
- `MaxUniqueTerms` — per-embedding cap after token normalization.
- `HashEmbedder` — deterministic feature-hash embedder over normalized search
  terms.
- `NewHashEmbedder` — constructor that validates the vector width.

See `doc.go` for the full godoc contract.

## Dependencies

- `go/internal/searchhybrid` — supplies the `Embedder` port and shared query
  tokenizer so lexical and vector paths normalize text the same way.

## Telemetry

None directly. API, MCP, and retrieval runners that use this package must emit
the semantic/hybrid retrieval state, method, failure class, budget, truncation,
and index-freshness signals described in the admission contract.

## Gotchas / invariants

- The hash embedder never calls a hosted service and never reads credentials.
- Empty input returns a zero vector with the configured dimensions.
- One embedding consumes at most `MaxUniqueTerms` normalized unique terms.
- Vector similarity is derived retrieval evidence only and must not create or
  update canonical graph truth.
- This package is not an ANN index and does not make large-corpus production
  readiness claims.

## Related docs

- `docs/public/reference/semantic-hybrid-search-admission.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/search-document-projection.md`
- `docs/internal/design/430-nornicdb-graph-search-split.md`
