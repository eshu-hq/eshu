# AGENTS.md - internal/searchhybrid guidance for LLM assistants

## Read first

1. `go/internal/searchhybrid/README.md` - package purpose, boundaries, modes.
2. `go/internal/searchhybrid/backend.go` - retrieval entry point and ranking.
3. `go/internal/searchhybrid/index.go` - BM25 stats, embedding cache, overflow.
4. `go/internal/searchhybrid/rrf.go` - ranking and Reciprocal Rank Fusion.
5. `go/internal/searchretrieval/README.md` - bounded retrieval runner contract.
6. `go/internal/searchdocs/README.md` - curated search-document contract.
7. `docs/internal/design/430-nornicdb-graph-search-split.md` - parent design.

## Invariants this package enforces

- **No hosted dependency** - the default path is pure Go. Embeddings are optional
  and arrive through the `Embedder` port, which must be deterministic and must
  not call a hosted service.
- **Derived evidence** - search rank and score never become canonical graph
  truth; the package performs no graph write.
- **Bounded and deterministic** - the index caps document count and signals
  overflow; retrieval resolves the smallest scope first, bounds fusion pools, and
  returns deterministic top-K (ties break by document id) plus one extra
  candidate for truncation detection.
- **Scope-first** - rank only documents matching the request anchor.

## Common changes and how to scope them

- **Change ranking math** - add a focused test in `backend_test.go` or
  `index_test.go` first, then update `bm25Score`, `rrfFuse`, or `rankForMode`.
  Keep ordering deterministic (id tie-break).
- **Add a real embedder** - implement `Embedder` in its own package; do not embed
  a model here. Keep `Embed` deterministic and hosted-free.
- **Change overflow behavior** - keep `Index.Overflow` and the candidate
  `index_overflow` / truncation failure-class signal in lockstep.

## Failure modes and how to debug

- Symptom: empty keyword results - confirm query terms overlap document text;
  BM25 excludes zero-score documents by design.
- Symptom: semantic mode errors - an embedder is required for semantic mode.
- Symptom: non-deterministic order - a new ranking path likely lacks the id
  tie-break; route it through `rankByScore`.
- Symptom: unexpectedly many candidates - retrieval returns at most `limit+1`;
  the runner truncates to `limit`.

## Anti-patterns specific to this package

- Calling NornicDB, Cypher, HTTP, MCP, or a hosted embedding API.
- Writing the canonical graph or promoting a search score to canonical truth.
- Re-embedding unchanged documents instead of using the content-hash cache.
- Ranking across the whole corpus without resolving the request scope first.
