# Repository read-model package

## Purpose

`repositoryreadmodel` holds the repository identity and page-shaping reads
for the repository handler family (Issue #6060, lane B): repository
ref/branch resolution (`refs.go`), cursor-paged ref reads (`refs_page.go`),
repository name lookup (`name_lookup.go`), and bounded list-page shaping
(`list_page.go`). These reads sit below the handler orchestration in
`repository` and beside the ContentReader read-model files that stay in the
query root.

## Ownership boundary

This package owns read shaping, not handler orchestration: pages, cursors,
and identity resolution. It imports only the standard library, `net/http`,
and `querycontract`. It never imports `repository`,
`repositoryartifacts`, or the query root. Both `repository` and the staying
root package consume it; the dependency arrow points one way.

## Exported surface

The exported surface is described in [doc.go](doc.go). The page type and
its constructors, the ref readers, and name lookup are exported because the
`repository` package and staying root callers use them. Unexported helpers
stay unexported; cross-package test pins go through `querytestutil` or
`querycontract`.

## Dependencies

Standard library, `net/http`, and `querycontract` only, plus
`querytestutil` in tests. No handler packages, no graph drivers.

## Verification

Run focused `repositoryreadmodel` tests, then `repository` and root
`query` suites. Changes here touch only read shaping: response envelopes,
bounds, and truncation semantics must not change.
