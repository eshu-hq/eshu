# Searchretrieval

## Purpose

`searchretrieval` defines the bounded internal retrieval contract for semantic
evaluation over curated Eshu search documents. It gives future NornicDB BM25,
vector, or hybrid adapters a narrow request and response shape before live
backend I/O or public API/MCP routes are added.

## Ownership boundary

This package owns request validation, scope anchoring, timeout-bound execution
through a backend port, deterministic result normalization, truncation reporting,
false canonical claim counting, observation summaries, and conversion into
`searchbench` scoring input. It does not query Postgres, call NornicDB, write
graph state, expose API/MCP routes, emit OTEL telemetry, or decide canonical
truth.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `Request` is one bounded internal retrieval request.
- `Scope` and `Anchor` select the smallest available scope before backend use.
- `Candidate` is one backend-produced `searchdocs.Document` candidate.
- `Backend` is the adapter port for Postgres, NornicDB, or other future search
  backends.
- `Runner` validates a request, applies its timeout, calls a `Backend`, builds a
  response, and records one `Observation`.
- `Observer`, `Observation`, and `ErrorClass` expose the internal summary that a
  later adapter can bridge to OTEL metrics, spans, or logs.
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

No OTEL telemetry directly. The package emits one `Observation` through an
optional `Observer` interface. Live adapters must bridge those summaries to
operator-facing search request duration, mode, result count, truncation, timeout,
failure class, and candidate truth-level metrics/spans/logs before issue #417
can be considered complete. Do not use high-cardinality anchor ids as metric
labels.

## Gotchas / invariants

- Retrieval requests must have a query, scope, limit, timeout, and valid mode.
- Scope anchoring prefers service, then workload, then repository, then
  environment.
- Backend candidates must have stable document ids, graph handles, and finite
  scores before ranking.
- `Runner` observes validation failures, backend errors, timeouts, normalization
  failures, and successful responses exactly once per request.
- `Runner` passes a timeout-bound context to the backend; adapters must return
  promptly on cancellation.
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
