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
  upserts ready, failed, or source-policy-disabled vector state across every
  active-document page.
- `BuildRequest` identifies the scope, provider profile, source class, model,
  vector-index version, optional document filter, projection revision, and
  vector-scope build fence.
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
- The batch-capable path accepts a bounded limit up to 10,000 documents so the
  reducer can spend its tail budget on a few large scopes. Metadata and value
  stores still split writes into 500-row SQL statements.
- Production requests carry the projection revision and build fence acquired
  immediately before embedding. Both vector value and metadata batch writes
  must still match the active generation, ready document projection, current
  building vector scope, and projected document content hash. A superseded
  worker therefore cannot overwrite a newer build after the fence advances.
- Paged builds anchor to the first observed generation so active-generation
  changes cannot mix rows from different generations in one build.
- Provider-backed builds may supply a per-document admission function. Denied
  documents write `disabled` metadata with no vector value so the side runner
  converges without treating that document as vector-retrievable.
- Provider profile, source class, model, dimensions, and vector index version
  are part of the persisted vector identity; local hash builds use the `local`
  profile and `search_documents` source class.
- Embedding text comes from `searchhybrid.DocumentText`. Vector metadata and
  values carry the persisted search-document `content_hash` token because the
  pending selector compares that exact token. Legacy or test stores that omit
  it fall back to `searchhybrid.DocumentContentHash`.
- Embedder error text is not persisted; only bounded failure classes are stored.
- This package is not a queue worker, ANN index, or public semantic-search
  adapter.

## Evidence

No-Regression Evidence: `go test ./internal/searchvector ./internal/storage/postgres
-run 'TestBuilderAnchorsPagedBuildToFirstGeneration|TestBuilderPagesThroughAllActiveDocuments|TestBuilderPersistsReadyVectorsForActiveDocuments|TestEshuSearchDocumentStoreAnchorsExplicitGeneration|TestEshuSearchVectorValueStoreListsOnlyActiveGeneration'
-count=1` failed before vector builds paged through every active-document row,
before paged builds anchored to the first observed generation, and before vector
value reads were gated by matching ready metadata, then passed.

No-Regression Evidence: `go test ./internal/searchembedprovider
./internal/searchembedruntime ./internal/searchvector ./internal/storage/postgres
./internal/reducer ./cmd/reducer -count=1` covers provider request cancellation,
per-repository/source policy admission before embedding, policy-denied
documents converging as disabled metadata without vector values, top-level
search-document content hashes for pending detection, and reducer wiring of the
shared runtime selector.

Observability Evidence: the builder result now reports `DisabledCount` for
policy-denied documents. `SearchVectorBuildRunner` logs the aggregate
`disabled_count` beside existing document, vector, failure, provider profile,
source class, model, vector-index version, duration, and failure-class fields.
No metric label, graph write, queue domain, API field, or MCP field changed.

## Related docs

- `docs/internal/design/2578-ann-vector-index-production-hybrid-search-contract.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/semantic-hybrid-search-admission.md`
- `go/internal/storage/postgres/README.md`
