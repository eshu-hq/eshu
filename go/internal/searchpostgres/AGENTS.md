# AGENTS.md - internal/searchpostgres guidance for LLM assistants

## Read first

1. `go/internal/searchpostgres/README.md` - package purpose, boundaries, and
   invariants.
2. `go/internal/searchpostgres/backend.go` - adapter contract.
3. `go/internal/searchretrieval/README.md` - bounded retrieval runner contract.
4. `go/internal/searchdocs/README.md` - curated search-document projection
   contract.
5. `docs/internal/design/430-nornicdb-graph-search-split.md` - parent design
   for separating canonical graph storage from curated search projection.

## Invariants this package enforces

- **Benchmark-only** - no API route, MCP tool, runtime default, graph write, or
  NornicDB search enablement belongs here.
- **Repository scoped** - the current Postgres content baseline can only safely
  filter by repository before search.
- **Keyword baseline** - this adapter serves `postgres_content_search` with
  `keyword` mode only.
- **Derived documents** - all rows must pass through `searchdocs` projection and
  remain derived retrieval evidence.

## Common changes and how to scope them

- **Change request validation** - add a focused test in `backend_test.go`, then
  update `Backend.Search`.
- **Change row projection** - update the relevant `searchdocs` projection first
  when the document contract changes, then adapt this package.
- **Add runtime exposure** - do not add it here. Public search surfaces require
  separate API/MCP contracts, telemetry, and #430 benchmark evidence.

## Failure modes and how to debug

- Symptom: a request is rejected - confirm it uses `keyword` mode and repo
  scope.
- Symptom: expected rows are absent - inspect the `searchdocs.Decision` reason;
  sensitive or excluded content should stay out of candidates.
- Symptom: benchmark truncation is missing - verify the adapter still overfetches
  by one row per content lane and lets `searchretrieval.BuildResponse` trim.

## Anti-patterns specific to this package

- Calling NornicDB, Cypher, HTTP, MCP, or graph query clients.
- Searching without a repository filter.
- Treating Postgres content rows as canonical truth.
- Adding high-cardinality document or graph handles to metrics.
