# Searchvector

## Purpose

`searchvector` builds persisted embedding rows for Eshu's curated search
documents. It is the replayable bridge between active `EshuSearchDocument` rows,
the caller-supplied `searchhybrid.Embedder` port, and the Postgres vector
metadata/value stores.

## Ownership boundary

This package owns orchestration only. It does not project search documents,
store SQL, serve API/MCP requests, write the canonical graph, schedule reducer
work, load provider profiles, or enable NornicDB search. Vector rows are derived
read-model state and are not graph truth.

## Exported surface

- `Builder` reads active documents, embeds their shared searchable text, and
  upserts ready or failed vector state across every active-document page.
- `BuildRequest` identifies the scope, provider profile, source class, model,
  vector-index version, and optional document filter.
- `BuildResult` summarizes document, vector, and failed-document counts.
- `FailureClassEmbedder` and `FailureClassInvalidVector` are bounded failure
  classes written to metadata.

## Dependencies

- `go/internal/searchhybrid` for the `Embedder` port, searchable document text,
  and content hash contract.
- `go/internal/storage/postgres` for active search-document rows and vector
  metadata/value row types.

## Telemetry

None directly. Callers that wire this package into runtime work must bridge
build counts, duration, failure class, and active vector status into the
operator-facing signals described in the telemetry docs.

## Gotchas / invariants

- The document store must already enforce active-generation visibility.
- Builds page through active documents until a short or empty page; a successful
  build must not silently cover only the first 500-document slice.
- Paged builds anchor to the first observed generation so active-generation
  changes cannot mix rows from different generations in one build.
- Provider profile, source class, model, dimensions, and vector index version
  are part of the persisted vector identity; local hash builds use the `local`
  profile and `search_documents` source class.
- Embedding text and content hashes must stay byte-identical to
  `searchhybrid` retrieval indexing.
- Embedder error text is not persisted; only bounded failure classes are stored.
- This package is not a queue worker, ANN index, or public semantic-search
  adapter.

## Evidence

No-Regression Evidence: `go test ./internal/searchvector ./internal/storage/postgres
-run 'TestBuilderAnchorsPagedBuildToFirstGeneration|TestBuilderPagesThroughAllActiveDocuments|TestBuilderPersistsReadyVectorsForActiveDocuments|TestEshuSearchDocumentStoreAnchorsExplicitGeneration|TestEshuSearchVectorValueStoreListsOnlyActiveGeneration'
-count=1` failed before vector builds paged through every active-document row,
before paged builds anchored to the first observed generation, and before vector
value reads were gated by matching ready metadata, then passed.

No-Observability-Change: this review fix adds no route, worker, queue domain,
graph write, metric, label, runtime default, API field, or MCP field. Existing
builder result counts and instrumented Postgres query/exec spans remain the
operator-facing signals for vector build progress and read state.

## Related docs

- `docs/internal/design/2578-ann-vector-index-production-hybrid-search-contract.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/semantic-hybrid-search-admission.md`
- `go/internal/storage/postgres/README.md`
