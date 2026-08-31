# Agent instructions: packagereg

Read `doc.go` and `README.md` first.

## Invariants

- `startQueryHandlerSpan` MUST forward through the package-local
  `packageregTracer` var (`handler_tracing.go`), never call
  `queryspan.HandlerTracer()` inline at a handler call site. The var is the
  seam a test swaps a recording provider into; bypassing it compiles clean
  and silently emits zero spans to the test's recorder.
- `attachCollectorListReadiness`/`collectorListReadiness`
  (`package_registry_collector_readiness.go`) MUST keep the non-empty-page
  short-circuit before the probe call. A page with rows is proof the
  collector ran; consulting the probe on a non-empty page lets a failing or
  stale probe downgrade an already-evidenced response.
- MUST NOT import root package `query`. Root's `package_registry_alias.go`
  already imports this package for its compatibility aliases, so the reverse
  import cycles. If a change needs something only root exposes, either it
  already has a leaf equivalent under `internal/query` (`querycontract`,
  `queryauth`, `querydecode`, `queryselector`, `queryspan`) or it does not
  belong in this family; ask before adding one.
- This family's six capabilities are registered in ROOT
  (`contract_package_registry.go`, `contract_capability_matrix.go`), not here
  -- root owns the router and always links into production, so its `init()`s
  always run there. `go test ./internal/query/packagereg` never runs root's
  `init()` functions (the cycle above), so `main_test.go`'s `TestMain`
  registers the same six capabilities directly with
  `querycontract.RegisterCapabilities` before this package's own tests run.
  If you add or change a capability here, update it in BOTH root's files and
  `main_test.go`, or this package's tests silently drift from what production
  actually enforces -- verify with a full `go test ./internal/query/packagereg -v`
  case count after any capability change, not just a build.

## When you change `PackageRegistryDependenciesCypher`

This callsite is pinned in `go/internal/queryplan/testdata/hot-cypher.yaml`
(`QP-SC-PKGREG-DEPS`) by a SHA256 of its source text. Any edit -- including a
rename -- fails `TestLegacyQueryplanManifestBindsProductionQueries` until the
digest is re-pinned. Before re-pinning, prove the Cypher text itself did not
change (the test failure reports both the manifest and the actual production
hash; only copy the production hash in if you have separately confirmed the
query is unchanged).

## Test doubles that cannot be shared with root

Go never compiles a package's `_test.go` files into anything another package
can import. Where this package's tests need something root's `_test.go`
files also define (a fake store, a slice-equality helper, the SQL-lockstep
field extractor), the fix is a local copy in this package, not an import --
see `package_registry_correlation_deref.go`,
`package_registry_slice_test_helpers_test.go`, and
`package_registry_sql_lockstep_helpers_test.go` for the existing ones. Keep a
new one minimal and cite the root original it mirrors in its doc comment.

## Verification

From `go/`:

```
go test ./internal/query/... ./internal/mcp -count=1
go test ./internal/query/packagereg -count=1 -v
go vet ./...
```

`internal/mcp` is included because `PackageRegistryHandler`'s compatibility
alias is exactly the surface an accidental unexported-symbol dependency would
break silently.
