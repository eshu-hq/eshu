# Searchvector

## Purpose

`searchvector` builds persisted local embedding rows for Eshu's curated search
documents. It is the replayable bridge between active `EshuSearchDocument` rows,
the deterministic local `searchhybrid.Embedder` port, and the Postgres vector
metadata/value stores.

## Ownership boundary

This package owns orchestration only. It does not project search documents,
store SQL, serve API/MCP requests, write the canonical graph, schedule reducer
work, call hosted providers, or enable NornicDB search. Vector rows are derived
read-model state and are not graph truth.

## Exported surface

- `Builder` reads active documents, embeds their shared searchable text, and
  upserts ready or failed vector state.
- `BuildRequest` identifies the scope, model, vector-index version, and
  optional document filter.
- `BuildResult` summarizes document, vector, and failed-document counts.
- `FailureClassEmbedder` and `FailureClassInvalidVector` are bounded failure
  classes written to metadata.

## Dependencies

- `go/internal/searchhybrid` for the no-network `Embedder`, searchable document
  text, and content hash contract.
- `go/internal/storage/postgres` for active search-document rows and vector
  metadata/value row types.

## Telemetry

None directly. Callers that wire this package into runtime work must bridge
build counts, duration, failure class, and active vector status into the
operator-facing signals described in the telemetry docs.

## Gotchas / invariants

- The document store must already enforce active-generation visibility.
- Embedding text and content hashes must stay byte-identical to
  `searchhybrid` retrieval indexing.
- Embedder error text is not persisted; only bounded failure classes are stored.
- This package is not a queue worker, ANN index, or public semantic-search
  adapter.

## Related docs

- `docs/internal/design/2578-ann-vector-index-production-hybrid-search-contract.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/semantic-hybrid-search-admission.md`
- `go/internal/storage/postgres/README.md`
