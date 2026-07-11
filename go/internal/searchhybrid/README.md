# Searchhybrid

## Purpose

`searchhybrid` is a pure-Go hybrid retrieval backend over the curated design-430
search-document lane. It indexes `searchdocs.Document` records and serves bounded
keyword (BM25), semantic (vector), and hybrid (Reciprocal Rank Fusion of BM25 and
vector) retrieval through the `searchretrieval.Backend` port, with no hosted API
dependency inside this package. It is the issue #2237 retrieval implementation
that the design-430 benchmark (#2235) evaluates against the Postgres baseline
and NornicDB.

## Ownership boundary

This package owns only the in-process index and ranking. It does not own:

- the curated document projection (`internal/searchdocs`),
- the bounded retrieval contract or runner (`internal/searchretrieval`),
- the evidence/scoring contract (`internal/searchbench`),
- the concrete embedding model or provider adapter. The model is supplied
  through the `Embedder` port; the package works with no embedder at all.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Embedder` — the optional embedding port. This package never performs hosted
  calls; governed adapters live outside this package.
- `Options` — index configuration (document cap, embedder, BM25 `K1`/`B`, RRF `k`).
- `VectorRetrievalMode` — semantic retrieval selection (`Auto`, `Exact`, or
  `Approximate`) while keeping exact cosine as the correctness baseline.
- `Index` / `NewIndex` — a bounded in-memory index with `Size`, `Overflow`, and
  `HasEmbedder` accessors.
- `Backend` — implements `searchretrieval.Backend` for keyword, semantic, and
  hybrid retrieval.
- `DocumentText` / `DocumentContentHash` — shared searchable-text and hash
  helpers for persisted vector builders.
- `DocumentTerms` / `QueryTerms` — shared lexical tokenizers used by the
  persisted Postgres index writer and reader so benchmark and runtime BM25 lanes
  score the same text.
- `TermKey` — bounded deterministic term identity for persisted BM25 lookup
  keys.

## Retrieval modes

- **keyword** — BM25 over the combined title, context text, path, and labels,
  served from an inverted index (term → postings) so a query visits only the
  documents that contain its terms, not the whole corpus. Documents with no
  lexical overlap (BM25 score `0`) are excluded.
- **semantic** — cosine similarity over local embedding vectors through the
  index-owned vector retriever. Requires an embedder; without one the mode
  returns an error. Exact cosine scans every valid in-scope vector and is the
  deterministic zero-value correctness baseline. Approximate retrieval must be
  selected explicitly.
- **hybrid** — Reciprocal Rank Fusion of the BM25 and vector rankings. Without an
  embedder it degenerates to the BM25 ranking and reports `search_method=bm25`.

Each backend resolves the smallest request scope first, ranks only in-scope
documents, and returns up to `limit+1` candidates so the runner can detect and
report truncation. Ordering is deterministic for fixed inputs: ties break by
document id, matching the runner's normalization.

## Bounded memory and overflow

`Options.MaxDocuments` caps the indexed document count. When the corpus exceeds
the cap, documents are ordered by id and the surplus is dropped and counted by
`Index.Overflow`. The backend signals an overflowed (incomplete) corpus on every
candidate with `Metadata["index_overflow"]="true"` and a
`searchbench.FailureClassTruncation` failure class. Per-ranker fusion pools are
also bounded relative to the requested limit.

## Embedding cache

When an embedder is configured, each document is embedded once and cached by the
SHA-256 of its searchable text, so identical or unchanged documents are not
re-embedded. `DocumentText` converts invalid UTF-8 bytes to the same Unicode
replacement characters used by JSON persistence before hashing or embedding.
The hash therefore stays stable when a projected document is written to
Postgres and read back.

## Vector retrieval

`VectorRetrievalAuto` and `VectorRetrievalExact` score every finite, non-zero,
same-dimension vector in the resolved request scope. Vectors that are empty,
zero, dimension-mismatched, or non-finite are skipped before ranking, and query
vectors with the same malformed states produce an empty vector result set.

`VectorRetrievalApproximate` is a pure-Go deterministic angular-LSH candidate
index. It hashes valid document vectors through multiple deterministic
hyperplane tables, probes the exact query signature plus one-bit neighbor
signatures, filters candidates by request scope, then computes exact cosine over
only those candidates. If the scoped ANN candidate set is empty, it falls back
to the exact retriever so semantic mode degrades to the correctness baseline
rather than returning a false empty result. Final ordering still uses
`rankByScore`, so ties break by document id.

## Telemetry

None directly. This is a retrieval backend behind `searchretrieval.Runner`, whose
`Observation` captures mode, scope anchor, duration, candidate and result counts,
truncation, timeout, candidate truth-level counts, and failure classes. The
public semantic-search route adds a route-level `query.semantic_search` span; a
future hosted semantic backend must still bridge retrieval observations to the
design-430 operator metrics without high-cardinality labels.

## Gotchas / invariants

- Search rank and score are derived retrieval evidence; this package never writes
  the canonical graph or promotes a score to canonical truth.
- Searchable text must be valid UTF-8 before it crosses the persistence and
  embedding boundary. Use `DocumentText` and `DocumentContentHash`; do not
  rebuild that normalization in a caller.
- `Embedder` implementations must be deterministic for fixed input; this package
  never calls hosted services directly.
- Semantic mode requires an embedder; hybrid without one is BM25-only.
- Approximate vector retrieval is a local ANN candidate-pruning optimization,
  not canonical truth and not a hosted provider or external vector-store lane.
- The in-process index is read-only after construction; rebuild to reflect new
  documents. The public semantic-search route uses the persisted Postgres index
  instead of this request-local structure.

## Related docs

- `docs/internal/design/430-nornicdb-graph-search-split.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/search-document-projection.md`
- `docs/public/reference/search-benchmark-evidence.md`
