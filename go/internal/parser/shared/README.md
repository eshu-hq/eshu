# Shared Parser Helpers

## Purpose

`internal/parser/shared` holds the small contracts language-owned parser
packages need without importing the parent `internal/parser` dispatcher. It
contains common payload bucket helpers, source reads, tree-sitter node helpers,
string utilities, integer coercion, and parser options shared by adapter
packages. The shared Go semantic-root options also carry the empty-method-list
convention used when an imported package can reach exported methods through an
escaped interface value and the qualified roots for imported receiver method
calls.

## Ownership boundary

This package owns dependency-safe helper contracts for child parser packages.
It does not own registry dispatch, language selection, content metadata
inference, parser runtime caching, or language-specific semantics.

## Exported surface

The godoc contract is in `doc.go` and `shared.go`. Current exports are
`Options`, `GoImportedInterfaceParamMethods`, `GoDirectMethodCallRoots`,
`GoPackageSemanticRoots`, `GoPackageSemanticRootOptions`, `BasePayload`, `ReadSource`,
`WalkNamed`, `NodeText`, `NodeLine`, `NodeEndLine`, `CloneNode`,
`AppendBucket`, `SortNamedBucket`, `SortNamedMaps`, `CollectBucketNames`,
`IntValue`, `LastPathSegment`, and `DedupeNonEmptyStrings`.

## Dependencies

This package imports the Go standard library and
`github.com/tree-sitter/go-tree-sitter` for node helper signatures. It must not
import the parent `internal/parser` package or any collector, query, storage,
projector, or reducer package.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path.

## Gotchas / invariants

Child parser packages depend on this package to avoid import cycles. Keep it
small and language-neutral; a helper that only one adapter needs belongs in
that adapter package.

`BasePayload`, bucket sorting, and name collection are fact-input contracts.
Changing their shape or ordering changes downstream materialization behavior.

`GoImportedInterfaceParamMethods` uses an empty method list intentionally. It
means the concrete value crossed into another package through an interface
parameter without a known method set, so exported methods on that concrete type
may be valid runtime hooks. Same-repository package contracts should carry
explicit method names from package interface declarations.

`GoDirectMethodCallRoots` uses lower-case qualified import-path receiver keys.
The parent parser decides which package directory receives those roots; child
packages should only carry the typed option.

`SortNamedMaps` sorts by `line_number` first and `name` second. That preserves
the parent parser ordering contract used before language packages were split.

Utility helpers such as `IntValue`, `LastPathSegment`, and
`DedupeNonEmptyStrings` are intentionally small. Keep language-specific parsing
rules out of this package so shared does not become a second parser package.

## Related docs

- `docs/plans/2026-05-09-parser-language-layout.md`
