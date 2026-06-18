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

This backend is used by design-430 benchmark evaluation and offline cap-sweep
runs. It still adds no runtime flag or graph write, and it does not enable
whole-graph search.

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
  exact equality while keeping the indexed key bounded;
- `eshu_search_index_stats` stores active corpus size and average document
  length for scoring and response metadata.

Reads join through `ingestion_scopes.active_generation_id`, so superseded
generation rows are ignored without rebuilding an index in the request path. A
projection sweep re-enqueues active scopes whose search documents exist but
index stats are missing, allowing retry to converge after partial failures.

When the reducer starts with `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER=hash` (or
`local_hash`), it builds ready active-generation local vectors for active
search documents into the Postgres sidecar metadata and payload tables. When
API or MCP starts with the same setting, `semantic` and `hybrid` public
requests use those persisted rows through the active document scope. The stored
vector identity must match
`searchembed.NewHashEmbedder(searchembed.DefaultDimensions)`, the active
document content hash, and the configured vector index version. Missing, stale,
partial, rebuilding, failed, incompatible, or malformed vector state must return
explicit unavailable or degraded retrieval state instead of claiming vector
participation. This is a deterministic no-network local path capped at 500
loaded documents. Ready local vectors are exact-scored by default so the public
API/MCP path keeps exact cosine as its correctness baseline. Explicit staged ANN
configuration may use the in-process angular-LSH candidate index with exact
cosine reranking. The route reports `retrieval_state=semantic_active` or
`hybrid_active` only when ready persisted vector retrieval participates. It is
not a hosted-provider, graph-write, or external vector-store integration.

Hosted search-embedding providers remain behind the
[Hosted Search Embedder Gate](hosted-search-embedder-gate.md). That gate must
approve the source class, adapter package boundary, request/response schema,
credential-handle posture, retention rules, and vector metadata before a
provider-backed embedder can feed this retrieval path.

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
- uses the explicit local hash embedder path for `semantic` and `hybrid` only
  when the reducer has built ready vector rows and API/MCP is configured with
  `ESHU_SEMANTIC_SEARCH_LOCAL_EMBEDDER`;
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
index and only uses in-process local vector retrieval when explicitly configured
with the local hash embedder. Broader default runtime search still
requires separate telemetry, capability, backend-proof, and semantic-evaluation
evidence.
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
