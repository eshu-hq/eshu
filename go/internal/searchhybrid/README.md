# Searchhybrid

## Purpose

`searchhybrid` is a pure-Go hybrid retrieval backend over the curated design-430
search-document lane. It indexes `searchdocs.Document` records and serves bounded
keyword (BM25), semantic (vector), and hybrid (Reciprocal Rank Fusion of BM25 and
vector) retrieval through the `searchretrieval.Backend` port, with no hosted API
dependency. It is the issue #2237 retrieval implementation that the design-430
benchmark (#2235) evaluates against the Postgres baseline and NornicDB.

## Ownership boundary

This package owns only the in-process index and ranking. It does not own:

- the curated document projection (`internal/searchdocs`),
- the bounded retrieval contract or runner (`internal/searchretrieval`),
- the evidence/scoring contract (`internal/searchbench`),
- the concrete local embedding model. The model is supplied through the
  `Embedder` port; the package works with no embedder at all.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Embedder` — the optional local-embedding port (deterministic, no hosted call).
- `Options` — index configuration (document cap, embedder, BM25 `K1`/`B`, RRF `k`).
- `Index` / `NewIndex` — a bounded in-memory index with `Size`, `Overflow`, and
  `HasEmbedder` accessors.
- `Backend` — implements `searchretrieval.Backend` for keyword, semantic, and
  hybrid retrieval.
- `DocumentTerms` / `QueryTerms` — shared lexical tokenizers used by the
  persisted Postgres index writer and reader so benchmark and runtime BM25 lanes
  score the same text.

## Retrieval modes

- **keyword** — BM25 over the combined title, context text, path, and labels,
  served from an inverted index (term → postings) so a query visits only the
  documents that contain its terms, not the whole corpus. Documents with no
  lexical overlap (BM25 score `0`) are excluded.
- **semantic** — cosine similarity over local embedding vectors. Requires an
  embedder; without one the mode returns an error.
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
re-embedded.

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
- The `Embedder` must be deterministic and must not call a hosted service.
- Semantic mode requires an embedder; hybrid without one is BM25-only.
- The in-process index is read-only after construction; rebuild to reflect new
  documents. The public semantic-search route uses the persisted Postgres index
  instead of this request-local structure.

## Related docs

- `docs/internal/design/430-nornicdb-graph-search-split.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/search-document-projection.md`
- `docs/public/reference/search-benchmark-evidence.md`
