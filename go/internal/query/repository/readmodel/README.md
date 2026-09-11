# Repository read-model package

## Purpose

`readmodel` holds the repository identity and page-shaping reads for the
repository handler family (Issue #6060, lane B; nested under `repository/`
for #6642 Part D): repository ref/branch resolution (`refs.go`),
cursor-paged ref reads (`refs_page.go`), repository name lookup
(`name_lookup.go`), and bounded list-page shaping (`list_page.go`). These
reads sit below the handler orchestration in `repository` and beside the
ContentReader read-model files that stay in the query root.

## Layout

- `doc.go` — the godoc contract.
- `list_page.go` — the bounded list-page type and its constructors/response
  builder (`ListPage`, `ListPageFromRequest`, `ListResponse`,
  `PageRepositoryMaps`).
- `name_lookup.go` — `QueryRepositoryNamesByID`, the graph name lookup by id.
- `refs.go` — the `Ref` alias and ref/branch resolution
  (`Refs`, `RefsDefaultBranch`, `RefEntry`, `ValidateSelectedRepositoryRef`).
- `refs_page.go` — the cursor-paged ref stream (`RefPageCursor`,
  `EncodeRepositoryRefPageCursor`/`DecodeRepositoryRefPageCursor`,
  `RefSortKey`, `RefKeyLess`, `SortRepositoryRefsForPaging`,
  `RefPageWindow`, `RefWindowEntries`, `RefsContainTag`).

## Ownership boundary

This package owns read shaping, not handler orchestration: pages, cursors,
and identity resolution. It imports only the standard library, `net/http`,
and `querycontract`. It never imports `repository`, `repositoryartifacts`,
or the query root, including its own parent package `repository`: package
`repository` imports this leaf, so a back-import cycles. Both `repository`
and the staying root package consume it; the dependency arrow points one
way.

## Exported surface

The exported surface is described in [doc.go](doc.go). The page type and
its constructors, the ref readers, and name lookup are exported because the
`repository` package and staying root callers use them. Unexported helpers
stay unexported; cross-package test pins go through `querytestutil` or
`querycontract`.

## Move evidence (#6642 Part D)

This package nested here verbatim from a glued top-level compound-name
package (`repositoryreadmodel`), splitting it into a directory per
`docs/internal/naming.md` rules 2, 3, and 4: only the package clause, the
file names (`repository_list_page.go` -> `list_page.go`,
`repository_name_lookup.go` -> `name_lookup.go`, `repository_refs.go` ->
`refs.go`, `repository_refs_page.go` -> `refs_page.go`, dropping the
package-name stutter), the destuttered exports that repeated the
`repository`/`readmodel` package-name pair (`RepositoryRef` -> `Ref`,
`RepositoryRefs` -> `Refs`, `RepositoryRefEntry` -> `RefEntry`,
`RepositoryRefsDefaultBranch` -> `RefsDefaultBranch`,
`RepositoryRefPageCursor` -> `RefPageCursor`, `RepositoryRefPageDefaultLimit`
-> `RefPageDefaultLimit`, `RepositoryRefPageMaxLimit` -> `RefPageMaxLimit`,
`RepositoryRefPageCursorVersion` -> `RefPageCursorVersion`,
`RepositoryRefSortKey` -> `RefSortKey`, `RepositoryRefKeyLess` ->
`RefKeyLess`, `RepositoryRefPageWindow` -> `RefPageWindow`,
`RepositoryRefWindowEntries` -> `RefWindowEntries`,
`RepositoryRefsContainTag` -> `RefsContainTag`, `RepositoryListPage` ->
`ListPage`, `RepositoryListDefaultLimit` -> `ListDefaultLimit`,
`RepositoryListMaxLimit` -> `ListMaxLimit`, `RepositoryListMaxOffset` ->
`ListMaxOffset`, `RepositoryListPageFromRequest` -> `ListPageFromRequest`,
`RepositoryListResponse` -> `ListResponse`), and the eight importers' import
paths differ. Exported identifiers that contain `Repository` mid-name
rather than as a leading stutter were left unchanged
(`EncodeRepositoryRefPageCursor`, `DecodeRepositoryRefPageCursor`,
`SortRepositoryRefsForPaging`, `ValidateSelectedRepositoryRef`,
`QueryRepositoryNamesByID`) -- out of the Part D destutter scope, which
targeted identifiers beginning with `Repository`; a follow-up may revisit
these for full rule-4 compliance. No read behavior, response envelope,
paging bound, or cursor semantics changed.

## No-Regression Evidence

Baseline `origin/main` at the move vs this branch: `go test
./internal/query/...` passes with 0 failures across 46 packages; a
name-for-name test-list union (`go test ./internal/query/repository/... -list
'.*'`) taken before the move (against the base worktree) and after (against
this branch) match exactly, package-path differences aside. The `git diff
origin/main --stat -- testdata/golden testdata/cassettes` is empty: this
move touches no Cypher text, queue behavior, or projection output. Response
envelopes, pagination bounds (`ListDefaultLimit`/`ListMaxLimit`/
`ListMaxOffset`, `RefPageDefaultLimit`/`RefPageMaxLimit`), and cursor
version/kind semantics (`RefPageCursorVersion`) are pinned verbatim by the
`repository` package's branch-paging suite, exercised through the repointed
import.

## No-Observability-Change

This package emits no metric, span, or log of its own; it is a pure
in-process read-shaping leaf. No new runtime behavior, so no new telemetry.

## Dependencies

Standard library, `net/http`, and `querycontract` only, plus
`querytestutil` in tests. No handler packages, no graph drivers.

## Verification

Run focused `readmodel` tests, then `repository` and root `query` suites.
Changes here touch only read shaping: response envelopes, bounds, and
truncation semantics must not change.
