# Search Retrieval Contract

Search retrieval is the bounded contract behind curated Eshu search-document
ranking. It powers internal evaluation paths and the repository-bounded
`POST /api/v0/search/semantic` route plus MCP `search_semantic_context` tool. It
is not a default whole-graph search feature.

The core contract lives in `go/internal/searchretrieval`. It validates bounded
retrieval requests and normalizes ranked `EshuSearchDocument` candidates before
adapters call Postgres, NornicDB, the in-process hybrid index, or any other
backend.

## Request Contract

Every request must include:

| Field | Meaning |
| --- | --- |
| `query` | User or eval-suite query text. |
| `scope` | At least one service, workload, repository, or environment anchor. |
| `mode` | `keyword`, `semantic`, or `hybrid`. |
| `limit` | Explicit top-K limit. |
| `timeout_ns` | Explicit timeout in nanoseconds. |

Scope selection prefers the smallest available anchor:

1. service;
2. workload;
3. repository;
4. environment.

Requests without a scope, limit, timeout, query, or valid mode are rejected
before any backend can run.

## Candidate Contract

Backends must return curated `EshuSearchDocument` records. They must not return
raw graph nodes, arbitrary graph properties, raw provider payloads, log lines,
trace spans, dashboard JSON, query bodies, or other excluded projection data.

Each candidate carries:

- the search document;
- finite backend score;
- optional failure classes;
- optional low-cardinality metadata.

Candidate search documents must have a stable `document.id` and at least one
non-empty graph handle. Candidates with missing document identity, missing graph
handles, `NaN` scores, or infinite scores are rejected before ranking because
they cannot produce stable top-K ordering or bounded graph expansion.

## Runner Contract

`Runner.Retrieve` executes one bounded request through a narrow backend port. It
does not know whether the backend is Postgres content search, NornicDB BM25,
NornicDB vector search, or a fixture adapter.

The runner:

- validates the request before backend use;
- creates a timeout-bound context from `timeout_ns`;
- calls `Backend.Search` for curated candidates only;
- normalizes candidates with `BuildResponse`;
- records one `Observation` when an observer is configured.

Observations carry:

- query id;
- scope anchor;
- mode;
- limit;
- duration;
- candidate count;
- result count;
- truncation state;
- timeout state;
- failure classes;
- candidate truth-level counts;
- error class.

The observation shape is an internal summary, not an OTEL exporter. Later live
adapters must bridge it to metrics, spans, or structured logs without using
high-cardinality anchor ids as metric labels.

## NornicDB Hybrid Prototype

`go/internal/searchnornicdb` implements the issue #417 prototype adapter for the
pinned NornicDB gRPC `SearchText` API. It is internal-only and admits explicit
`SemanticContext` labels.

The adapter:

- requires `hybrid` mode;
- sends the `SemanticContext` label filter to NornicDB;
- overfetches by one result within the internal maximum so truncation can be
  observed;
- rejects NornicDB responses that report non-hybrid `search_method` values;
- rejects `fallback_triggered=true`;
- rejects hits that do not carry the `SemanticContext` label;
- rejects hits outside the request's smallest scope anchor;
- requires per-item `derived` / `read_model` truth labels and `fresh`
  freshness;
- returns only candidate graph handles for later bounded expansion.

The pinned NornicDB gRPC request shape exposes labels but not property filters.
This branch therefore post-checks the returned candidate scope and does not
claim full #417 measured acceptance until live evidence proves pre-search
scoping, or a documented design exception is accepted. There is still no
whole-graph fallback, and the NornicDB adapter remains internal-only.

## Postgres Keyword Baseline Adapter

`go/internal/searchpostgres` implements the Postgres content-search baseline for
internal benchmark runs. It wraps the existing Postgres content-store search
methods, projects rows through `searchdocs`, and returns
`postgres_content_search` candidates for `keyword` mode only.

The adapter:

- requires repository scope before touching the content store;
- rejects service, workload, and environment-only scopes because the current
  content store cannot pre-filter those anchors safely;
- overfetches by one row per content lane so truncation is observed after
  normalization;
- skips rows that `searchdocs` excludes for missing stable handles, excluded
  source kind, or sensitive context;
- emits derived content-search documents, not canonical graph truth.

This adapter is benchmark plumbing. It does not add a public route, MCP tool,
runtime flag, graph write, or NornicDB search enablement.

## In-Process Hybrid Backend

`go/internal/searchhybrid` implements the issue #2237 pure-Go hybrid retrieval
backend over the curated search-document lane, with no hosted dependency. It
indexes `searchdocs.Document` records and serves:

- `keyword` — BM25 over the combined title, context text, path, and labels;
  documents with no lexical overlap are excluded;
- `semantic` — cosine similarity over local embedding vectors (requires an
  `Embedder`; the model is supplied by the caller and must be deterministic and
  hosted-free);
- `hybrid` — Reciprocal Rank Fusion of the BM25 and vector rankings, degenerating
  to BM25 (`search_method=bm25`) when no embedder is configured.

The backend:

- resolves the smallest request scope first and ranks only in-scope documents;
- caps indexed document count and signals an overflowed corpus with
  `index_overflow` metadata and a `truncation` failure class;
- caches embeddings by content hash so unchanged documents are not re-embedded;
- returns up to `limit+1` deterministic candidates (ties break by document id)
  so the runner detects truncation;
- emits derived retrieval evidence, never canonical graph truth.

This backend is used by design-430 benchmark evaluation, offline cap-sweep
runs, and the `find_code` hybrid re-rank below. It still adds no runtime flag or
graph write, and it does not enable whole-graph search.

## find_code Hybrid Re-rank

`go/internal/query.CodeHybridRanker` reorders the lexical `find_code`
(`POST /api/v0/code/search`) content-fallback results by fused BM25 + vector
relevance. It closes the gap where the in-process hybrid backend was never read
at the `find_code` query surface.

Embedding on this path is confined to a process-local deterministic hash
embedder (`searchembed.HashEmbedder`). The ranker MUST NOT use the runtime's
governed semantic-search provider embedder: that embedder POSTs text to an
external endpoint, so embedding the request-local `source_cache` rows through it
would egress source snippets and block on the provider HTTP timeout per result,
ignoring client cancellation and bypassing the semantic policy / document-vector
readiness path. The local embedder keeps all source text inside the process,
and the embedder is not injectable, so a provider embedder cannot reach it.

The ranker:

- runs only after the lexical content path has already retrieved and authorized
  a bounded result set (capped by the request limit); it never widens the
  candidate set, issues no graph or Postgres read, and builds a fresh in-memory
  `searchhybrid.Index` over only those request-local rows;
- embeds documents and the query with the process-local hash embedder only — no
  network call, no source-text egress;
- projects each result row through `searchdocs.ProjectContentEntity`, so the
  searchable text, truth labels, and graph handles match the persisted
  search-document lane;
- ranks repository-scoped `hybrid` mode (BM25 fused with vector cosine via RRF)
  and reorders the existing rows by fused rank, tagging reordered rows with
  `search_backend=hybrid`;
- bounds the pass with a context timeout derived from the caller's context and
  returns the lexical order unchanged if the caller's context is already done;
- preserves the lexical order and lexical `content_index` truth basis unchanged
  when it cannot fuse a signal: disabled, fewer than two results, or no
  projectable document. It never drops, adds, or relabels a row as canonical
  truth.

The ranker is wired (with its own local embedder) only when the runtime's
semantic search is enabled; content-only deployments keep the lexical content
order. The exact-match (`exact=true`) symbol path is left lexical because it is
an identity lookup, not a relevance ranking.

Performance Evidence: the re-rank is CPU-only over at most the request limit of
request-local documents (default 50, hard cap 100 in the ranker), with BM25
served from an inverted index and vector cosine exact below the staged ANN
threshold of 256 documents. It adds no Cypher, graph write, queue, lease, or
goroutine, and runs after the Postgres/graph round-trips it reorders, so it
introduces no new hot-path scan. `go test ./internal/query -run CodeSearch...`
proves a known fixture is reordered by hybrid relevance and that the lexical
fallback edges are byte-stable.

No-Regression Evidence: `cd go && go test ./internal/query ./internal/mcp
./internal/searchhybrid ./internal/searchretrieval ./internal/searchembed
./internal/searchdocs -count=1` and `go test ./cmd/api ./cmd/mcp-server
-count=1` pass with the ranker wired; behavior is identical to the prior lexical
path when the ranker is nil or reports a no-op.

Observability Evidence: reordered rows carry a `search_backend=hybrid` marker
and the response `source_backend` switches to `hybrid_content_store` only when
the hybrid pass changed the ranking, so an operator can confirm from the
response envelope whether vector retrieval participated. The bounded request
reuses the `searchretrieval` request-validation contract.

### search_entity_content / search_file_content Hybrid Re-rank

`go/internal/query.ContentHybridRanker` applies the same bounded lane to the
`search_entity_content` (`POST /api/v0/content/entities/search`) and
`search_file_content` (`POST /api/v0/content/files/search`) tools. It is the same
re-rank design as `CodeHybridRanker`, not a new approach:

- it owns the same process-local deterministic hash embedder and accepts no
  provider embedder, so request-local `source_cache` / file content never
  egresses;
- it runs only after the lexical content path has retrieved and authorized a
  bounded result set (capped by the request limit, default 50, hard cap 200), and
  only when the request resolved to a single repository scope; it never widens
  the candidate set, issues no extra graph or Postgres read, and builds a fresh
  request-local `searchhybrid.Index` over only those rows;
- it projects entity rows through `searchdocs.ProjectContentEntity` and file rows
  through `searchdocs.ProjectContentFile`, so the searchable text, truth labels,
  and graph handles match the persisted search-document lane;
- it ranks repository-scoped `hybrid` mode (BM25 fused with vector cosine via
  RRF) and reorders the existing rows by fused rank, tagging reordered rows with
  `search_backend=hybrid`;
- it preserves the lexical order and lexical `content_index` truth basis
  unchanged when it cannot fuse a signal (no embedder / semantic search disabled,
  fewer than two rows, no single-repo scope, or no projectable document), never
  drops or adds a row, and never relabels a row as canonical truth.

The content tools keep `source_backend=postgres_content_store`; this change adds
only result ordering plus the per-row `search_backend=hybrid` marker on reordered
rows, so the response envelope and wire contract are otherwise unchanged. The
ranker is wired only when the runtime's semantic search is enabled; content-only
deployments keep the lexical content order. `go test ./internal/query -run
'SearchEntityContentResultsAreHybridReranked|SearchFileContentResultsAreHybridReranked|ContentHybridRerank'`
proves a known fixture is reordered by hybrid relevance and that the lexical
fallback edges preserve order, basis, and length.

## Persisted Postgres Search Index

`go/internal/storage/postgres.EshuSearchIndexStore` serves the default public
repository-bounded search surface from persisted BM25 postings for active
curated search documents. The reducer writes the index alongside
`eshu_search_document` facts for each scope and generation:

- `eshu_search_index_documents` stores one normalized document payload, source
  kind, repo id, length, and fact id per `(scope_id, generation_id,
  document_id)`;
- `eshu_search_index_terms` stores term frequencies for BM25 lookup by
  `(scope_id, generation_id, term_key, document_id)`, retaining raw terms for
  exact equality while keeping the indexed key bounded. The primary key's
  `(scope_id, generation_id, term_key)` prefix serves BM25 query joins. The
  reducer clears term rows once per `(scope_id, generation_id)` before
  inserting refreshed page terms, so the persisted term table does not require
  a document-keyed secondary index for refresh or retire;
- `eshu_search_index_stats` stores active corpus size and average document
  length for scoring and response metadata.

Reads join through `ingestion_scopes.active_generation_id`, so superseded
generation rows are ignored without rebuilding an index in the request path. A
projection sweep re-enqueues active scopes whose search documents exist but
index stats are missing, allowing retry to converge after partial failures.

API, MCP, and reducer use the same semantic-search embedder selector. When
`ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` (or `local_hash`) is set, all three
runtimes use the deterministic no-network local profile. `auto_hash` uses that
local profile only when no governed `search_documents` provider profile is
configured. When the selector is unset, they may auto-select exactly one
governed provider profile whose source classes include `search_documents`,
whose source policy is configured, and whose profile declares model id, endpoint
profile id, credential source, and positive `embedding_dimensions`. If multiple
eligible profiles exist,
`ESHU_SEMANTIC_SEARCH_PROVIDER_PROFILE_ID` must choose one or startup fails
closed.

The reducer builds ready active-generation vectors for active search documents
into the Postgres sidecar metadata and payload tables. API and MCP use persisted
rows only when provider profile id, source class, model id, embedding
dimensions, content hash, active generation, and vector index version match the
selected runtime identity. Missing, stale, partial, rebuilding, failed,
incompatible, or malformed vector state returns explicit unavailable or
degraded retrieval state instead of claiming vector participation. Ready vectors
use `VectorRetrievalAuto`: exact cosine below the staged ANN threshold and the
in-process angular-LSH candidate index with exact reranking above it. The route
reports `retrieval_state=semantic_active` or `hybrid_active` only when ready
persisted vector retrieval participates. It is not a graph-write or external
vector-store integration.

Broader production ANN/vector-index retrieval is gated separately by issue #2578
with storage-owner follow-up issue #2582. The first implementation storage owner
is Postgres sidecar vector metadata and build state over active curated search
documents. That gate still requires schema approval, active-generation
freshness, benchmark corpus, false-canonical-claim guard, rollback semantics,
and operator-visible index-state signals before any external vector store,
hosted embedder, or canonical-graph search-index behavior change lands.

## Public Route And MCP Tool

`POST /api/v0/search/semantic` and MCP `search_semantic_context` expose the
first public repository-bounded retrieval slice over active curated search
documents.

The public surface:

- requires `repo_id`, `query`, `mode`, `limit`, and `timeout_ms`;
- treats `repo_id` as the durable search-document scope id for this slice;
- optionally narrows within that repository by service, workload, environment,
  and curated `source_kinds`;
- serves from the active persisted search index without a request-time full
  rebuild or corpus cap;
- uses the selected local or governed provider embedder for `semantic` and
  `hybrid` only when the reducer has built ready vector rows with the same
  persisted vector identity;
- caps returned results at 100;
- returns the canonical Eshu envelope when requested;
- reports derived truth basis, freshness, graph handles, `search_method`,
  `retrieval_state`, `indexed_document_count`, `corpus_limit`,
  `corpus_may_be_truncated`, and false canonical claim count.

Scoped-token behavior is fail-closed before storage access. Empty repository
grants return an empty result set, and out-of-grant repository ids return
not-found.

## Response Contract

`BuildResponse` sorts candidates by score descending and document id ascending,
then returns deterministic top-K results with:

- rank;
- score;
- document;
- truth scope;
- freshness;
- graph handles for bounded graph expansion;
- truncation state;
- false canonical claim count.

The false canonical claim count increments when any result claims a truth level
other than `derived`. Search score, semantic similarity, and link prediction do
not become canonical graph truth.

## Benchmark Link

`Response.SearchbenchResults` converts normalized results into
`go/internal/searchbench` scoring input. This lets issue #417 use the same
recall, precision, nDCG, and false-canonical-claim metrics defined for issue
#1264.

## What This Does Not Do

This contract does not:

- read or write graph state;
- decide HTTP authorization, envelope negotiation, or OpenAPI/MCP schemas;
- enable default runtime or whole-graph search.

The internal Postgres, NornicDB, and in-process hybrid adapters can call their
backends when explicitly constructed by a benchmark or proof harness. The
semantic-search route defaults to the persisted active curated search-document
index and uses persisted vector retrieval only when API/MCP and reducer share
the selected local or governed provider vector identity. Broader default runtime
search still requires separate telemetry, capability, backend-proof, and
semantic-evaluation evidence.
Production embedder-backed `semantic` or `hybrid` retrieval must also satisfy
[Semantic Hybrid Search Admission](semantic-hybrid-search-admission.md).

## Verification Gate

Focused package gate:

```bash
cd go && go test ./internal/searchretrieval ./internal/searchdocs ./internal/searchbench ./internal/searchnornicdb ./internal/searchpostgres ./internal/searchhybrid -count=1
```

Docs changes must also pass:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Related Docs

- [Semantic Hybrid Search Admission](semantic-hybrid-search-admission.md)

- [Search Document Projection](search-document-projection.md)
- [Search Benchmark Evidence](search-benchmark-evidence.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [NornicDB Canonical Graph And Search Projection Split](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/430-nornicdb-graph-search-split.md)
