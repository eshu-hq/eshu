# Agent instructions: contentread

Read `doc.go` and `README.md` first.

## Invariants

- `ContentReader` and every `content_reader_*.go` file stay in ROOT on
  purpose (see README Gotchas). Never move them here, and never add a
  `(cr *ContentReader)` method here -- Go requires methods in their type's
  package, so that file would not compile, and the type is pinned by
  sibling-lane methods plus 13 sibling tripwire interfaces.
- MUST NOT import root package `query`. Root's `content_read_alias.go`
  already imports this package for its compatibility aliases, so the reverse
  import cycles. If a change needs something only root exposes, either it
  already has a leaf equivalent under `internal/query` (`querycontract`,
  `queryauth`, `decode`, `queryselector`, `queryspan`, `codemodel`) or
  it does not belong in this family; ask before adding one.
- `querycontract.PagedContentSearcher` MUST keep decomposed primitive
  parameters. Reintroducing a shared request struct (or any unexported type
  in the signature) silently breaks cross-package interface satisfaction:
  the assertion compiles, misses, and the handler falls back to the slower
  loop with no error. `paged_searcher_tripwire_test.go` guards this -- if
  you change the seam, that test must fail first (RED) and pass after.
- `firstNonEmptyContentRepoID` MUST stay a trivial mirror of root's
  `firstNonEmpty` (`repository_deployment_evidence_read_model.go`). It is a
  copy because an unexported helper cannot cross the boundary; any logic
  added here but not there (or vice versa) is a silent behavior fork.
- The batch entity-access helpers live in ROOT
  (`content_entity_access_batch.go`), not here -- the batch path filters
  through the evidence family's `filterEvidenceCitationEntitiesForAccess`.
  Do not re-add them here.
- The hybrid ranker MUST NOT gain a provider embedder, network call, or
  unbounded candidate set. `applied=false` keeps the lexical order and the
  `content_index` truth basis; `SearchBackend="hybrid"` is set only on rows
  the fused rank actually reordered.
- `ContentSearchMaxOffset` is wire-public (root's
  `openapi_paths_content.go` emits it and root's OpenAPI sweep asserts it).
  Changing the constant without updating the emitted schema fails
  `TestOpenAPISpec_ContentEntitySchemasExposeMetadata` in root -- update
  both or neither.

## Test doubles that cannot be shared with root

Go never compiles a package's `_test.go` files into anything another package
can import. This package's tests use the shared `querytestutil` doubles
where they exist (`FakePortContentStore`, embedded by the tripwire fake).
Where root's `_test.go` files define something with no shared equivalent (a
recording selector-aware store, the not-ready store), the fix is a minimal
local copy citing the root original -- see `selectorAwareContentStore` in
`content_handler_file_lines_test.go` and `contentSubstringIndexNotReadyStore`
in `content_handler_index_readiness_test.go`. Keep a new one minimal and
cite the root original it mirrors in its doc comment.

## Verification

From `go/`:

```
go test ./internal/query/contentread -count=1 -v
go test ./internal/query/... -count=1
go vet ./internal/query/...
```

`./internal/query/...` (not just this package) is required because root's
alias surface, OpenAPI sweep, and the staying `ContentReader` paths are the
load-bearing compatibility proof -- `query.ContentHandler`,
`query.NewContentHybridRanker`, and the search-limit constants must keep
resolving for `cmd/api` and `cmd/mcp-server` with no caller edited.
