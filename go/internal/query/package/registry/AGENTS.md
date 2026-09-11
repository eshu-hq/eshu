# Agent instructions: package/registry

Read `doc.go` and `README.md` first.

## Invariants

- `startQueryHandlerSpan` MUST forward through the package-local
  `packageregTracer` var (`handler_tracing.go`), never call
  `queryspan.HandlerTracer()` inline at a handler call site. The var is the
  seam a test swaps a recording provider into; bypassing it compiles clean
  and silently emits zero spans to the test's recorder.
- `attachCollectorListReadiness`/`collectorListReadiness`
  (`collector_readiness.go`) MUST keep the non-empty-page short-circuit
  before the probe call. A page with rows is proof the collector ran;
  consulting the probe on a non-empty page lets a failing or stale probe
  downgrade an already-evidenced response.
- MUST NOT import root package `query`. Root's `package_registry_alias.go`
  already imports this package for its compatibility aliases, so the reverse
  import cycles. If a change needs something only root exposes, either it
  already has a leaf equivalent under `internal/query` (`querycontract`,
  `queryauth`, `decode`, `queryselector`, `queryspan`) or it does not
  belong in this family; ask before adding one.
- This family's six capabilities are registered in ROOT
  (`contract_package_registry.go`, `contract_capability_matrix.go`), not here
  -- root owns the router and always links into production, so its `init()`s
  always run there. `go test ./internal/query/package/registry` never runs
  root's `init()` functions (the cycle above), so `main_test.go`'s `TestMain`
  registers the same six capabilities directly with
  `querycontract.RegisterCapabilities` before this package's own tests run.
  If you add or change a capability here, update it in BOTH root's files and
  `main_test.go`, or this package's tests silently drift from what production
  actually enforces -- verify with a full
  `go test ./internal/query/package/registry -v` case count after any
  capability change, not just a build.
- Exported identifiers MUST NOT reintroduce a `PackageRegistry`/`Registry`
  leading stutter (naming.md rule 4): the package clause is already
  `registry`, so a type named e.g. `RegistryHandler` would repeat the
  package name in its own exported surface. `PackageDependencyChain*` and
  `ResolvePackageDependencyChains` are the one deliberate exception --
  `Package` there names the data (a dependency chain between packages), not
  this package, so it does not stutter.

## When you change `DependenciesCypher`

This callsite is pinned in `go/internal/queryplan/testdata/hot-cypher.yaml`
(`QP-SC-PKGREG-DEPS`) by a SHA256 of its source text. Any edit -- including a
rename -- fails `TestLegacyQueryplanManifestBindsProductionQueries` until the
digest is re-pinned. Before re-pinning, prove the Cypher text itself did not
change (the test failure reports both the manifest and the actual production
hash; only copy the production hash in if you have separately confirmed the
query is unchanged). The same rule applies to any other symbol registered in
`go/internal/queryplan/testdata/query-source-coverage.yaml` under this
package's `file:` entries -- a method's `source_sha256` covers its receiver
type name too, so renaming the receiver (not just the method) also forces a
re-pin.

## Test doubles that cannot be shared with root

Go never compiles a package's `_test.go` files into anything another package
can import. Where this package's tests need something root's `_test.go`
files also define (a fake store, a slice-equality helper, the SQL-lockstep
field extractor), the fix is a local copy in this package, not an import --
see `correlation_deref.go`, `slice_test_helpers_test.go`, and
`sql_lockstep_helpers_test.go` for the existing ones. Keep a new one minimal
and cite the root original it mirrors in its doc comment.

## File names must not repeat `registry` (naming.md rule 2)

This directory's files already dropped the `package_registry_` prefix
(`#6642` Part D). A new file's name must not reintroduce a `registry`- or
`package_registry_`-prefixed name; name it for what it holds instead
(`cypher.go`, `aggregates.go`, `dependency_chains.go`, and so on are the
existing pattern).

## Verification

From `go/`:

```
go test ./internal/query/... ./cmd/api ./cmd/mcp-server -count=1
go test ./internal/query/package/registry -count=1 -v
go test ./internal/queryplan/ -count=1
go vet ./...
```

`cmd/api`/`cmd/mcp-server` are included because `Handler`'s compatibility
alias is exactly the surface an accidental unexported-symbol dependency would
break silently. `internal/queryplan` is included because a rename here
re-pins its manifests. `internal/mcp` is included because its route-serves-data registry
(`go/internal/mcp/route_serves_data_registry_routes_2.go`) names this
package's `handler.go`/`cypher.go` by path and `Handler` by struct name for
the `GET /api/v0/package-registry/packages` route; a rename here must repoint
that entry.
