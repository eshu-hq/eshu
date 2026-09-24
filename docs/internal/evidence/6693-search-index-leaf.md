# #6693 checklist step 8: `search/index/`

Branch `refactor/6693-search-index-leaf`, worktree
`eshu-6693-search-index-leaf`. Moves `search/index/` (package `indexstore`)
out of `go/internal/storage/postgres` (package `postgres`, "root") per
`docs/internal/design/6693-postgres-target-tree.md` and
`docs/internal/design/6693-postgres-target-tree/code.md`.

## What moved

- `store.go` -- package `indexstore`. `EshuSearchIndexStore`,
  `NewEshuSearchIndexStore`, `EshuSearchIndexSearch`,
  `EshuSearchIndexSearchResult`, and `Search` keep their exported names.
  Two private helpers were exported (justified below): `sortedSearchIndexTerms`
  -> `SortedSearchIndexTerms`, `buildEshuSearchIndexQuery` ->
  `BuildEshuSearchIndexQuery`. Everything else
  (`searchIndexHandlePredicate`, `normalizeEshuSearchIndexSearch`,
  `validateEshuSearchIndexSearch`, `loadStats`,
  `eshuSearchIndexStatsQuery`, `eshuSearchIndexDefaultLimit`) stays private.
- `store_test.go` -- package `indexstore`, in-package. Converted the root
  private `fakeExecQueryer`/`queueFakeRows` fixtures to the shared
  `internal/storage/postgres/fake` package (`fake.ExecQueryer`,
  `fake.Rows{Data: ...}`, `db.Queries[i].Query`/`.Args`). No `Adapt` needed;
  the fixture rows already match the store's own destination-column counts.
- `store_schema_test.go` -- package `indexstore_test` (external), imports
  root `postgres` for the already-exported `BootstrapDefinitions()` and
  `Definition` (no shim needed). Its data-plane schema-file relative path grew
  from 4 to 6 `..` segments for the new directory depth.
- `doc.go`, `README.md`, `AGENTS.md` -- new doc trio modelled on
  `storage/postgres/scope/` and `storage/postgres/semantic/`.

## Deviation from the plan: two tests stay in root

Two files the plan mapped to a plain move could not move as annotated. Both
were the file "eshu_search_index_bm25_partition_live_test.go" and
"eshu_search_index_terms_doc_plan_live_test.go" (root-relative base names;
they never had a destination-side counterpart to move to). Root-derivation
detail:

- `eshu_search_index_bm25_partition_live_test.go` calls three private test
  helpers defined in root's own `eshu_search_index_partition_live_test.go`
  (`openSearchIndexPartitionProofDB`, `searchIndexPartitionProofConn`,
  `scannedChildPartitionsLive`). Those are private symbols in a `_test.go`
  file, which Go never lets another package import regardless of
  capitalization -- only a package's own test binary (in-package or external
  test of that same package) links its `_test.go` files. The bm25 file also
  calls what were root-private query-building internals
  (`sortedSearchIndexTerms`, `buildEshuSearchIndexQuery`), now moved to
  `indexstore`. It needs both a root-only symbol class and an indexstore-only
  symbol class at once, so it stays in root (a genuine two-package `SPLIT`
  case, matching the plan's own established `SPLIT` category and its
  precedent `eshu_search_index_term_copy_test.go -> ... # SPLIT: reads
  private symbols of db, root`) and its calls into `indexstore` were
  repointed to the newly exported `indexstore.SortedSearchIndexTerms`,
  `indexstore.BuildEshuSearchIndexQuery`, `indexstore.EshuSearchIndexSearch`,
  `indexstore.EshuSearchIndexSearchResult`, `indexstore.NewEshuSearchIndexStore`.
  It also called "eshu_search_index_test.go"'s private `searchIndexDocumentFixture`
  fixture builder, which moved into `indexstore` as a private (in-package)
  helper; since that too is a `_test.go`-only symbol of a different package,
  it cannot be imported either, so this file keeps a small, deliberate local
  copy of the same fixture builder (documented in a comment at its
  definition).
- `eshu_search_index_terms_doc_plan_live_test.go` defines `planJSONSummary`,
  which root's own `eshu_search_index_partition_live_test.go` already calls.
  It has zero references to anything in `indexstore`, so by the plan's own
  test-placement rule ("a test that reads private symbols of exactly one
  package goes to that package") it belongs in root, not `search/index/`.
  Staying costs nothing: no code in it changed.

Plan updated in this commit: `docs/internal/design/6693-postgres-target-tree/code.md`'s
`search/index/` section now lists only the two tests that actually moved and
explains the reassignment; `root.md`'s root test list gained both files
alphabetically with `# SPLIT: reads private symbols of root, search/index`
and `# spans root=100%; follows its private symbols, not its name` tags and
its header test count went up by 2; `6693-postgres-target-tree.md`'s
per-directory table moved `search/index/` 4 -> 2 test and the root row up
by 2 test, and the "Test placement" tally moved 2 files out of "in-package
test" (minus 2) into "SPLIT" (plus 1) and "stays in root" (plus 1).

## Callers repointed

- `go/internal/query/semanticsearch/semantic_search_postgres.go` (package
  `semanticsearch`, outside `internal/storage/postgres`): `Search` now calls
  `indexstore.NewEshuSearchIndexStore(postgres.SQLDB{DB: s.db})` and builds
  `indexstore.EshuSearchIndexSearch{...}`. `postgres.SQLDB` and the unrelated
  `postgres.NewEshuSearchDocumentStore`/`postgres.EshuSearchDocumentFilter`
  calls in the same file are untouched (search/document is a later leaf).
- `go/internal/storage/postgres/eshu_search_index_bm25_partition_live_test.go`
  (stays in root, see above): imports and calls `indexstore.*` as listed.
- `go/internal/storage/postgres/README.md` mentions the `eshu_search_index_*`
  Postgres tables (not the Go store) and needed no change; confirmed no other
  root doc names `EshuSearchIndexStore`/`NewEshuSearchIndexStore` by symbol.
- `docs/public/reference/search-retrieval-contract.md` cited
  `go/internal/storage/postgres.EshuSearchIndexStore`; repointed to
  `go/internal/storage/postgres/search/index.EshuSearchIndexStore`.

## Exact-name test-repoint proof

For each of the 17 moved test names, `for n in TestEshuSearchIndexStoreSearchesActiveGenerationBM25 TestEshuSearchIndexStoreReportsIndexedCountWithoutMatches TestEshuSearchIndexStoreRequiresBoundedSearch TestEshuSearchIndexStoreLanguageFilterAppendsLabelPredicate TestEshuSearchIndexStoreNoLanguageFilterOmitsLabelPredicate TestBootstrapDefinitionsBoundEshuSearchIndexTermKeys TestBootstrapDefinitionsHashPartitionSearchIndexTerms TestBootstrapDefinitionsSkipPartitionedSearchTermPKeyRebuild TestDataPlaneSearchIndexSchemaHashPartitionSearchIndexTerms TestDataPlaneSearchIndexSchemaSkipsPartitionedSearchTermPKeyRebuild TestBootstrapDefinitionsMigrateSearchTermsToHashPartitions TestBootstrapDefinitionsSearchTermCutoverAvoidsExclusiveReCopy TestBootstrapDefinitionsDoNotRecreateHistoricalSearchTermDocumentIndex TestBootstrapDefinitionsAvoidRedundantSearchTermLookupIndex TestDataPlaneSearchIndexSchemaAvoidsRedundantTermLookupIndex TestBootstrapDefinitionsDropRedundantSearchTermLookupIndex TestBootstrapDefinitionsDropSearchIndexTermsDocumentIndex; do go test ./internal/storage/postgres/search/index/... -list "^$n\$" -count=1 | rg -q "^$n\$" || echo "missing $n"; done` prints nothing: all 17 names are found in the new package. Run with `./internal/storage/postgres` in place of the new package path, the same loop prints `missing <name>` for all 17: none are left in root.

Moved names: TestEshuSearchIndexStoreSearchesActiveGenerationBM25,
TestEshuSearchIndexStoreReportsIndexedCountWithoutMatches,
TestEshuSearchIndexStoreRequiresBoundedSearch,
TestEshuSearchIndexStoreLanguageFilterAppendsLabelPredicate,
TestEshuSearchIndexStoreNoLanguageFilterOmitsLabelPredicate,
TestBootstrapDefinitionsBoundEshuSearchIndexTermKeys,
TestBootstrapDefinitionsHashPartitionSearchIndexTerms,
TestBootstrapDefinitionsSkipPartitionedSearchTermPKeyRebuild,
TestDataPlaneSearchIndexSchemaHashPartitionSearchIndexTerms,
TestDataPlaneSearchIndexSchemaSkipsPartitionedSearchTermPKeyRebuild,
TestBootstrapDefinitionsMigrateSearchTermsToHashPartitions,
TestBootstrapDefinitionsSearchTermCutoverAvoidsExclusiveReCopy,
TestBootstrapDefinitionsDoNotRecreateHistoricalSearchTermDocumentIndex,
TestBootstrapDefinitionsAvoidRedundantSearchTermLookupIndex,
TestDataPlaneSearchIndexSchemaAvoidsRedundantTermLookupIndex,
TestBootstrapDefinitionsDropRedundantSearchTermLookupIndex,
TestBootstrapDefinitionsDropSearchIndexTermsDocumentIndex.

## dirgate

The move takes one non-test file out of `internal/storage/postgres`, so the `internal/storage/postgres` row in `scripts/lib/dirgate-grandfather.tsv` is re-pinned to what `bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints for the rebased tree (each rebase onto a sibling move re-derives it);
`scripts/lib/dirgate-grandfather.tsv` and the generated
`tools/golangci-lint-dirgate/grandfather.go` were updated to match. The run
also prints pre-existing `naming_violation` lines for root files that carry a
justified dirgate marker naming the later checklist step that moves them; this
move adds none.

## Unrelated same-name symbol (verified, not a collision)

`rg` for `sortedSearchIndexTerms` outside `docs/internal/design` and
`docs/internal/evidence` also matches its own definition in
`go/internal/reducer/eshusearch` (`eshu_search_document_index_writer.go`) and
that package's benchmark test. That is an independent, pre-existing private
function in
package `eshusearch` (different package, different signature: returns three
slices including frequencies, ours returns two) that never called or was
called by the store moved here; confirmed unrelated, not a naming collision.

## Verification (from `go/` unless noted; all after the final edit)

```text
gofumpt -l $(git diff --name-only -- '*.go')                                   -> exit 0 (no output)
go build ./...                                                                  -> exit 0
go vet ./...                                                                    -> exit 0
go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait" \
  ./internal/storage/postgres/...                                              -> exit 0
go test ./internal/storage/postgres/... -race -count=1                         -> ok, exit 0
go test ./internal/query/semanticsearch/... -race -count=1                     -> ok, exit 0
go test -list '.*' ./internal/storage/postgres/search/index/ -count=1          -> 17 tests, exit 0
go test ./internal/query -run QueryPlan -count=1                               -> ok, exit 0 (no source-hash pin names this file)
```

From the repository root:

```text
bash scripts/verify-dirgate.sh --all                                           -> exit 0
bash scripts/verify-package-docs.sh                                            -> exit 0 (after commit; see below)
bash scripts/verify-performance-evidence.sh origin/main                        -> exit 0
bash scripts/verify-moved-file-refs.sh                                         -> exit 0, no dangling references
bash scripts/verify-doc-citations.sh                                           -> exit 0
git diff --check                                                               -> exit 0
```

`rg` for every old symbol/file name (`eshu_search_index.go`,
`eshu_search_index_test.go`, `eshu_search_index_schema_test.go`,
`sortedSearchIndexTerms`, `buildEshuSearchIndexQuery`, `postgres.EshuSearchIndex*`,
`postgres.NewEshuSearchIndexStore`) outside `docs/internal/design` and
`docs/internal/evidence` returns nothing except the unrelated `eshusearch`
package match documented above and the SQL table-name mentions in
`scripts/verify_remote_e2e_degradation_report.sh` and its two fixture JSON
files (those name the Postgres table `eshu_search_index_terms`, unaffected by
this Go package move).

No-Regression Evidence: this is a path-and-import-only move; `go test
./internal/storage/postgres/... -race -count=1` and `go test
./internal/query/semanticsearch/... -race -count=1` pass unchanged, and the
BM25 query text, its argument order, and the stats query are byte-identical
(same string literals, same builder), so the SQL Postgres actually runs did
not change.

No-Observability-Change: no metric, span, log field, worker, queue, lease, or
runtime setting changed; this moves a synchronous read and its tests to a new
package path with no behavior change.
