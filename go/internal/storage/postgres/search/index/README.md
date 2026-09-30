# Postgres search index store

## Purpose

This package reads the persisted BM25 search index for active curated search
documents. It is the `search/index/` leaf of the storage/postgres split
(#6693) and, for now, holds only the reader the split hoisted out of the
parent package.

## Ownership boundary

This package owns one read: ranking active documents for a scope, repo, and
query against the persisted `eshu_search_index_terms` / `_documents` /
`_stats` tables, joined to the requesting scope's active generation. It owns
no write path for those tables (the reducer's projection writes them) and no
document-fixture or vector-search concerns; those stay with the parent
package until their own `search/document/` and `search/vector/` moves.

## Exported surface

- `EshuSearchIndexStore` / `NewEshuSearchIndexStore(db.ExecQueryer)` / `NewEshuSearchIndexReader(db.Queryer)`
- `EshuSearchIndexSearch`, `EshuSearchIndexSearchResult`
- `(EshuSearchIndexStore).Search(ctx, EshuSearchIndexSearch) (EshuSearchIndexSearchResult, error)`
- `SortedSearchIndexTerms`, `BuildEshuSearchIndexQuery` -- exported only for
  `storage/postgres`'s own live BM25 partition-pruning proof; production
  callers use `Search`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/storage/postgres/db` for the `ExecQueryer`/`Rows` contracts.
- `internal/searchdocs`, `internal/searchhybrid`, `internal/searchretrieval`
  for the document, term-scoring, and anchor/candidate shapes shared with the
  rest of Eshu's search stack.
- `internal/storage/postgres/fake` (test-only) for the `db.ExecQueryer` test
  double `store_test.go` stages responses against.

## Telemetry

This is a synchronous read with no worker, queue, lease, or retry of its own.
`query.semantic_search.persisted_index` is a child of the query layer's
`query.semantic_search` span. It covers the active-generation stats read, BM25
query, cursor drain, row scans, JSON decoding, and cursor close. Its bounded
events mark `stats_complete`, `query_returned`, `first_row`, and
`rows_complete` (with the returned `candidate_count`). A `stage_error` event
names only the closed failure stage (`stats`, `query`, `scan`, `decode`, or
`iterate`) and marks the span as failed. An empty normalized term set records
`rows_complete` with zero candidates after `stats_complete`, without issuing
the BM25 query. The span never records query text, scope, repo, document,
or user identifiers. The child span and its events show elapsed query-plus-row
work in Tempo; PostgreSQL statement timing and plans require separate database
inspection. Language filtering treats non-array labels as empty instead of
returning a database error.

## Gotchas / invariants

- `Search` always joins the requesting scope's `active_generation_id` from
  `ingestion_scopes`; a scope with no active generation gets zero rows
  reported (the store's own `loadStats` and the main query both filter this
  way), not an error.
- BM25 document frequency counts all postings in the active scope before repo,
  anchor, source-kind, and language filters. Each matching posting looks up its
  document by primary key; scoring groups narrow document keys, and the JSONB
  payload is fetched after the ranked page is bounded. Keep those boundaries
  when changing the query or its plan.
- A missing or non-array `Labels` field contributes no language labels.
- `SortedSearchIndexTerms` and `BuildEshuSearchIndexQuery` are exported only
  because `storage/postgres`'s `eshu_search_index_bm25_partition_live_test.go`
  needs to build the identical BM25 query to run it under `EXPLAIN`. That live
  test also needs root's own live-DB partition-proof helpers, which are
  private to root's `_test.go` files and cannot cross a package boundary, so
  the test stays in root rather than moving here; see
  `docs/internal/design/6693-postgres-target-tree/root.md`'s entry for it.
- Do not import the parent `postgres` package from `store.go`. Leaves under
  `storage/postgres` must not depend on root (#6693), and root's in-package
  test `eshu_search_index_bm25_partition_live_test.go` imports this package,
  so a leaf-to-root import fails root's test build with an import cycle.

## Related docs

- [Postgres storage](../../README.md)
- [storage/postgres target tree](../../../../../../docs/internal/design/6693-postgres-target-tree.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
