# Searchpostgres

## Purpose

`searchpostgres` adapts the existing Postgres content search store to the
internal retrieval benchmark port. It gives issue #430 and #417 proof work a
measured keyword baseline without enabling NornicDB search or adding public API
or MCP routes.

## Ownership boundary

This package owns only the Postgres baseline adapter for internal benchmark
runs. It does not own the content-store schema, write path, query handlers,
NornicDB integration, graph writes, OpenAPI, MCP tools, or runtime defaults.
The Postgres content-store SQL stays in `internal/storage/postgres`; curated
document projection stays in `internal/searchdocs`.

## Exported surface

See `doc.go` for the godoc-rendered package contract.

- `ContentStore` is the narrow subset of the Postgres content store required by
  the adapter.
- `Backend` implements `searchretrieval.Backend` for repository-scoped keyword
  retrieval.

## Dependencies

`searchpostgres` imports `internal/searchbench`, `internal/searchdocs`,
`internal/searchretrieval`, and `internal/storage/postgres`. It depends on the
storage package only for the existing content row types and search methods.

## Telemetry

None directly. This is benchmark plumbing, not a runtime route. The adapter
feeds `searchretrieval.Runner`, whose observation summary captures query mode,
scope anchor, result counts, truncation, timeout, candidate truth-level counts,
and failure classes. Live benchmark harnesses must bridge those observations to
the evidence records and operator signals named in the #430 design before any
production search surface exists.

## Gotchas / invariants

- Requests must use `keyword` mode and repository scope. Postgres content
  search cannot safely apply service, workload, or environment scope by itself.
- The adapter overfetches by one row per content lane so `searchretrieval` can
  detect truncation after normalizing candidates.
- Rows are projected through `searchdocs`; excluded or sensitive content is
  skipped rather than returned as a search candidate.
- Returned documents are derived retrieval evidence. Search rank and score do
  not become canonical graph truth.

## Related docs

- `docs/internal/design/430-nornicdb-graph-search-split.md`
- `docs/public/reference/search-retrieval-contract.md`
- `docs/public/reference/search-benchmark-evidence.md`
- `docs/public/reference/search-document-projection.md`
