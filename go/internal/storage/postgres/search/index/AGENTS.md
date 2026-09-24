# AGENTS.md — Postgres search index store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `store.go`, with `store_test.go` and `store_schema_test.go`.

## Invariants

- Keep the package clause as `package indexstore`; callers import the
  `storage/postgres/search/index` path without an alias.
- `store_test.go` is an in-package test; `store_schema_test.go` is
  `package indexstore_test` (it needs root's exported `BootstrapDefinitions`
  and `Definition`) and imports `internal/storage/postgres`.
- Never import the parent `postgres` package from `store.go`: leaves must not
  depend on root (#6693), and root's in-package
  `eshu_search_index_bm25_partition_live_test.go` imports this package, so a
  reverse import breaks root's test build with an import cycle.
- `SortedSearchIndexTerms` and `BuildEshuSearchIndexQuery` stay exported even
  though production callers never call them directly; root's
  `eshu_search_index_bm25_partition_live_test.go` needs them for its EXPLAIN
  proof and cannot be moved here (it also needs root-private live-DB proof
  helpers). Do not re-privatize them without also fixing that test.

## Common changes

- Changing the BM25 scoring or the query it builds: update
  `BuildEshuSearchIndexQuery` and its fragment assertions in
  `store_test.go`, then check `eshu_search_index_bm25_partition_live_test.go`
  in root, which asserts the same query plan prunes partitions under EXPLAIN.
- Changing the persisted schema (`eshu_search_index_terms` / `_documents` /
  `_stats`): the schema itself lives in root (`schema.go`'s
  `BootstrapDefinitions`); `store_schema_test.go` here only asserts on it.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Privatizing `SortedSearchIndexTerms` or `BuildEshuSearchIndexQuery` breaks
  root's live BM25 partition-pruning proof at compile time.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
