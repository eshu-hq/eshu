# AGENTS.md - repository/readmodel

## Read first

1. `doc.go` for the public contract.
2. `README.md` for the ownership boundary, move evidence, and proof
   requirements.
3. Parent `../AGENTS.md` for query-wide invariants.

## Scope

`go/internal/query/repository/readmodel/` (package `readmodel`): repository
identity and page-shaping reads for the repository handler family.

- `ListPage`, `ListPageFromRequest`, `ListResponse`, `PageRepositoryMaps` —
  bounded list-page shaping. Callers: `repository/repository.go`,
  `repository/repository_stats_limits.go`,
  `repository/repository_language_inventory.go`.
- `QueryRepositoryNamesByID` — graph name lookup by repository id. Callers:
  `impacttrace/deployment_trace_enrichment_support.go`.
- `Ref`, `Refs`, `RefsDefaultBranch`, `RefEntry`,
  `ValidateSelectedRepositoryRef` — ref/branch resolution. Callers:
  `repository/repository_branches.go`, `repository/repository_content.go`,
  `repository/repository_tree.go`.
- `RefPageCursor`, `EncodeRepositoryRefPageCursor`/
  `DecodeRepositoryRefPageCursor`, `RefSortKey`, `RefKeyLess`,
  `SortRepositoryRefsForPaging`, `RefPageWindow`, `RefWindowEntries`,
  `RefsContainTag` — the cursor-paged branches/tags stream (#5503). Callers:
  `repository/repository_branches.go`.

## Invariants

- Leaf package: standard library, `net/http`, and `querycontract` only.
  Never import `repository`, `repositoryartifacts`, or the query root,
  including this leaf's own parent package `repository`: `repository`
  imports this leaf directly, so a back-import cycles.
- Page bounds and truncation semantics are stable wire behavior; changing a
  limit, default, or truncation flag is a contract change, not a refactor.
- Cursor version/kind decoding (`RefPageCursorVersion`, `RefPageCursor`) is
  load-bearing: a decode error MUST turn into a 400 at the caller, never a
  silently empty or reset page.
- No package-name stutter in exported identifiers or file names
  (`docs/internal/naming.md` rules 2 and 4): the ref/page accessors are
  `Ref`/`Refs`/`RefPageCursor`/`ListPage`, not `Repository*`; files are
  `refs.go`/`refs_page.go`/`list_page.go`/`name_lookup.go`, not
  `repository_refs.go` etc. Identifiers that carry `Repository` mid-name
  rather than as a leading stutter (`EncodeRepositoryRefPageCursor`,
  `DecodeRepositoryRefPageCursor`, `SortRepositoryRefsForPaging`,
  `ValidateSelectedRepositoryRef`, `QueryRepositoryNamesByID`) were left
  unchanged by #6642 Part D's destutter scope; see README's move-evidence
  paragraph before renaming them elsewhere.

## Verification

Run focused `readmodel` tests, then `repository` and root `query` suites.
Run `scripts/verify-package-docs.sh` whenever this package changes.

## Common changes

- Add a pure read-shaping helper only when both `repository` and a staying
  root caller need it without importing each other.

## Anti-patterns

- Do not add handler orchestration, graph query construction beyond a
  single bounded read, or family-specific response models here.
- Do not re-glue the package name into a new export or file name
  (`repository_*`, `Repository*` duplicating the package/parent-directory
  name) when extending this leaf.
