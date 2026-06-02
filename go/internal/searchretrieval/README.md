# Searchretrieval

## Purpose

`searchretrieval` defines the bounded internal retrieval contract for semantic
evaluation over curated Eshu search documents. It gives future NornicDB BM25,
vector, or hybrid adapters a narrow request and response shape before live
backend I/O or public API/MCP routes are added.

## Ownership boundary

This package owns request validation, scope anchoring, deterministic result
normalization, truncation reporting, false canonical claim counting, and
conversion into `searchbench` scoring input. It does not query Postgres, call
NornicDB, write graph state, expose API/MCP routes, or decide canonical truth.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Request` is one bounded internal retrieval request.
- `Scope` and `Anchor` select the smallest available scope before backend use.
- `Candidate` is one backend-produced `searchdocs.Document` candidate.
- `BuildResponse` validates a request and normalizes ranked top-K results.
- `Response.SearchbenchResults` converts normalized results into benchmark
  scoring input.
- `ValidateRequest` rejects unscoped, unlimited, no-timeout, or invalid-mode
  requests.

## Dependencies

`searchretrieval` imports `go/internal/searchdocs` for document, truth,
freshness, and graph-handle fields. It imports `go/internal/searchbench` for
search modes, failure classes, and scoring conversion. It otherwise uses only
the Go standard library.

## Telemetry

None directly. The package is a pure contract and normalization layer. Live
adapters must emit search request duration by mode, result count, truncation,
timeout, failure class, and candidate truth-level summaries before issue #417
can be considered complete.

## Gotchas / invariants

- Retrieval requests must have a query, scope, limit, timeout, and valid mode.
- Scope anchoring prefers service, then workload, then repository, then
  environment.
- Backend candidates must have stable document ids, graph handles, and finite
  scores before ranking.
- Results are sorted by score descending and document id ascending for stable
  top-K behavior.
- `false_canonical_claim_count` reports any returned result that claims a truth
  level other than `derived`; it does not suppress or promote the result.
- This package is internal evaluation plumbing. Public API/MCP search surfaces
  need a later PR with envelope, capability, OpenAPI, MCP, telemetry, and
  benchmark proof.

## Related docs

- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/search-benchmark-evidence.md`
- `docs/public/reference/search-document-projection.md`
- `docs/internal/design/430-nornicdb-graph-search-split.md`
