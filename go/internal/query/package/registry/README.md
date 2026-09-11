# Package registry query handlers

## Purpose

Serves the package-registry read surface: package and version identity
lookups, package-native dependency edges, reducer-derived ownership/
consumption/publication correlations, dependency chains, and graph
aggregate/inventory counts. All routes hang off `Handler.Mount`.

## Ownership boundary

This package owns everything under `GET /api/v0/package-registry/*`: the
handler struct, its Cypher builders, its Postgres correlation store, its
graph aggregate store, and its response models. It does not own auth, the
graph/content port definitions, or the response-envelope/capability contract
-- those live in `querycontract`, `queryauth`, and the other leaf packages
under `internal/query` (see Dependencies).

Root package `query` keeps compatibility aliases and forwarders
(`package_registry_alias.go`) for `PackageRegistryHandler`,
`PackageRegistryCorrelationRow`, `PostgresPackageRegistryCorrelationStore`,
`GraphPackageRegistryAggregateStore`, and their two constructors, so
`cmd/api` and `cmd/mcp-server` build unchanged. Root also keeps this family's
six capability registrations (`contract_package_registry.go`,
`contract_capability_matrix.go`) -- they stay in root deliberately, since root
owns the router and always links into the production binary. This package's
own tests get the same registrations from `main_test.go`'s `TestMain` instead
(see Gotchas below).

## Exported surface

- `Handler` and `Mount` -- the HTTP entry point.
- `CorrelationStore`, `AggregateStore` -- the two storage ports the handler
  depends on, plus their production implementations
  `PostgresCorrelationStore` and `GraphAggregateStore` and their `New*`
  constructors.
- `CorrelationRow`, `CorrelationFilter`, `CorrelationPage`, and the
  aggregate/inventory result types (`AggregateFilter`, `AggregateCount`,
  `InventoryDimension`, `InventoryRow`).
- `PackageResult`, `VersionResult`, `DependencyResult`, `IdentityIssue` --
  the package/version/dependency response and identity-issue shapes.
- `PackageDependencyChain*` and `ResolvePackageDependencyChains` -- kept
  their names across the #6642 Part D destutter: `Package` here names the
  data (a package dependency chain), not the enclosing `registry` package,
  so it is not a stutter under naming.md rule 4.
- `DependenciesCypher` -- exported (unlike cypher.go's other Cypher
  builders) because `go/internal/query/queryplan_legacy_production_binding_test.go`
  drives the real production statement through the query-plan comparison it
  runs for every handler family; see Gotchas below.

## Move evidence (#6642 Part D)

This package nested here verbatim from the flat `internal/query/packagereg`
leaf (#6060), splitting it into a directory per `docs/internal/naming.md`
rules 2 and 4: `package/` holds no Go files of its own (a pure directory
level, needing no doc trio), and `registry/` is this package. Every
`package_registry_X.go` file dropped its directory-name-stutter prefix
(`X.go`); `package_registry.go` became `handler.go` because `X` there was
empty; `package_registry_test.go` became `handler_test.go` -- its test
companion pairing, not the mechanical `test.go` the bare prefix-strip would
have produced (which the Go toolchain would not have recognized as a test
file at all, since it must match `*_test.go` with a non-empty prefix). Files
that never carried the prefix (`doc.go`, `main_test.go`, `handler_tracing.go`,
`factschema_decode_package_correlations.go`) kept their names.

Every exported identifier leading with `PackageRegistry` dropped that word
(`PackageRegistryHandler` -> `Handler`, `PackageRegistryCorrelationStore` ->
`CorrelationStore`, `PackageRegistryCorrelationFilter` -> `CorrelationFilter`,
`PackageRegistryCorrelationRow` -> `CorrelationRow`,
`PackageRegistryCorrelationQueryer` -> `CorrelationQueryer`,
`PackageRegistryCorrelationResult` -> `CorrelationResult`,
`PackageRegistryCorrelationPage` -> `CorrelationPage`,
`PackageRegistryDependenciesCypher` -> `DependenciesCypher`,
`PackageRegistryIdentityIssue` -> `IdentityIssue`,
`PackageRegistryPackageResult` -> `PackageResult`,
`PackageRegistryVersionResult` -> `VersionResult`,
`PackageRegistryDependencyResult` -> `DependencyResult`,
`PackageRegistryAggregateStore` -> `AggregateStore`,
`PackageRegistryInventoryDimension` -> `InventoryDimension`,
`PackageRegistryAggregateMaxLimit` -> `AggregateMaxLimit`,
`PackageRegistryAggregateFilter` -> `AggregateFilter`,
`PackageRegistryAggregateCount` -> `AggregateCount`,
`PackageRegistryInventoryRow` -> `InventoryRow`), plus the two names that
carried a second leading word ahead of the stutter
(`PostgresPackageRegistryCorrelationStore` -> `PostgresCorrelationStore`,
`NewPostgresPackageRegistryCorrelationStore` -> `NewPostgresCorrelationStore`,
`GraphPackageRegistryAggregateStore` -> `GraphAggregateStore`,
`NewGraphPackageRegistryAggregateStore` -> `NewGraphAggregateStore`).
`PackageDependencyChain*` and `ResolvePackageDependencyChains` kept their
names (see Exported surface). Unexported identifiers, and every method name
(receiver types changed; the methods themselves were never part of this
rename), were left exactly as they were. No new leading `Registry` stutter
was introduced by any of the drops.

Root's `package_registry_alias.go` now aliases every pre-move exported
spelling to this package's destuttered names (`type PackageRegistryHandler =
registry.Handler`, and so on); `cmd/api` and `cmd/mcp-server` compile
unchanged. The two other importers,
`go/internal/query/package_registry_family_test_doubles_test.go` and
`go/internal/query/queryplan_legacy_production_binding_test.go`, were
repointed to the new import path and qualifier and the destuttered names.
The `go/internal/queryplan/testdata/query-source-coverage.yaml` and
`testdata/hot-cypher.yaml` manifests were re-pinned: file paths moved, the
`(*PackageRegistryHandler).*` and `(GraphPackageRegistryAggregateStore).*`
symbol strings became `(*Handler).*` and `(GraphAggregateStore).*`, and their
`source_sha256` digests were re-derived because the receiver type name
literally appears in the hashed function text (`func` keyword through the
closing brace); `scoped_access.go`'s three plain-function entries
(`packageRegistryAnchorVisibility`, `packageRegistryNameAnchorCandidates`,
`packageRegistryVersionAnchorPackageID`) kept their original digests
unchanged, since their bodies never named a renamed type -- only their
file-path field changed. `DependenciesCypher`'s digest was re-derived the
same way. No
Cypher text, response shape, pagination bound, or capability behavior
changed. No production-file rename this move required touched the
`querycontract`/`queryauth`/`decode`/`queryselector`/`queryspan` leaf
packages it depends on.

## No-Regression Evidence

No-Regression Evidence (#6642 rename): baseline `020757ad6` (pre-move HEAD) vs this branch: a name-for-name test-list
union (`go test ./internal/query/packagereg/... -list '.*'` at the baseline,
`go test ./internal/query/package/registry/... -list '.*'` on this branch)
matches exactly. `go test ./internal/query/... -count=1` and
`go test ./cmd/api ./cmd/mcp-server -count=1` pass; `go/internal/queryplan`'s
manifests were re-pinned and `go test ./internal/queryplan/ -count=1` passes.
The `git diff origin/main --stat -- testdata/golden testdata/cassettes` is
empty: this move touches no Cypher text, queue behavior, or projection
output. `go list -deps ./internal/query/package/registry` does not include
`internal/query` -- the leaf still does not import the root.

`go test ./internal/mcp -count=1` passes: the `internal/mcp` route-serves-data
registry entry for `GET /api/v0/package-registry/packages`
(`go/internal/mcp/route_serves_data_registry_routes_2.go`) had its path
strings and handler struct name repointed to
`go/internal/query/package/registry/{handler.go,cypher.go}` and `Handler` in
the same change, the way every earlier query rename repointed that registry.

## No-Observability-Change

No-Observability-Change (#6642 rename): this package emits the same spans it always did. `handler_tracing.go` holds
a package-local `packageregTracer = queryspan.HandlerTracer()` and a
`startQueryHandlerSpan` that forwards to `queryspan.StartHandlerSpanWith`, so
the tracer scope name and every span attribute are unchanged from before the
move. No metric was added, renamed, or removed, and no log key changed.

## Dependencies

The Go standard library, `database/sql`, `go/internal/storage/postgres/pgarray`,
`sdk/go/factschema` (and its `reducerderived/v1` package), `go/internal/scope`,
`go/internal/telemetry`, and these `internal/query` leaf packages:

- `querycontract` -- `GraphQuery`, `ContentStore`, response/truth envelopes,
  capability gates, row-value decoders, `CollectorListReadinessStore` and its
  two `Build*` functions.
- `decode` -- the classified fact-decode failure
  (`*decode.Error`) this package's correlation decode wrappers return.
- `queryselector` -- `ResolveForRequestWithAccess`, the repository-selector
  resolution this package's correlation and dependency-chains handlers use.
- `queryauth` -- `AuthContext`, `AuthContextFromContext`,
  `RepositoryAccessFilterFromContext` (via `querycontract`), the scoped-token
  authorization bounds every scoped-access test drives.
- `queryspan` -- the per-route HTTP span (see Gotchas below).

It does **not** import root package `query`: that import would cycle, since
root imports this package for the compatibility aliases above.

## Gotchas / invariants

**`main_test.go`'s `TestMain` is not redundant with root's capability
registrations.** `go test ./internal/query/package/registry` never runs root
package `query`'s `init()` functions (the import would cycle), so without
`TestMain` registering the same six capabilities directly with
`querycontract`, every handler test in this package fails with the
capability gate's `unsupported_capability` 501 -- not because the handler is
broken, but because nothing ever registered a capability for it to check
against. Production is unaffected: root always links into the real binary
and always runs its own `init()`s. Keep `TestMain`'s values in sync with
`contract_package_registry.go` and `contract_capability_matrix.go`'s
`baseCapabilityMatrix` if either changes.

**The tracer is a package-local var, not an inline `queryspan.HandlerTracer()`
call.** `handler_tracing.go` declares `packageregTracer` once and every
handler forwards through it. A test that swaps in a recording provider
targets that var; calling `queryspan.HandlerTracer()` directly at each call
site instead compiles and emits zero spans to a test recorder -- it silently
breaks the seam a test relies on.

**Collector-readiness ordering: a non-empty page never consults the probe.**
`attachCollectorListReadiness`/`collectorListReadiness`
(`collector_readiness.go`) mirror root's `collector_list_readiness.go`
exactly. A nil store yields no envelope. A page with `resultsReturned > 0` is
classified `ready_with_results` WITHOUT calling the configured-collector
probe: returned rows are themselves proof the collector ran, so a failing or
stale probe must never downgrade an already-evidenced page. The probe runs
only for an empty page, to disambiguate `not_configured` from
`ready_zero_results`; a probe error there yields `readiness_unavailable` so
the page is never dropped. Getting this order wrong (checking the probe
first) is a real behavior regression, not a style choice.

**`DependenciesCypher` is pinned in the queryplan manifest.**
`go/internal/queryplan/testdata/hot-cypher.yaml`'s `QP-SC-PKGREG-DEPS` entry
carries a `source_sha256` over this function's source text. Any edit --
including a rename -- fails `TestLegacyQueryplanManifestBindsProductionQueries`
until the digest is re-pinned; re-pin only after proving the Cypher text
itself did not change.

**Some helpers are local copies of root helpers, not forks.** Two separate
Go constraints force this, and they are worth keeping apart.

`derefString`/`derefBool` (`correlation_deref.go`) are
production code. Root has the same helper, `derefString` (named
`workItemDerefString` before #6642 destuttered it; its `derefBool` twin was
dropped in the same move), but it is unexported, and an unexported symbol
cannot be called across a package boundary. Root exports no equivalent to
wrap, and it cannot move here because `factschema_decode_supplychain.go`
still calls it.

The slice-comparison and SQL-lockstep helpers (`slice_test_helpers_test.go`,
`sql_lockstep_helpers_test.go`) are copies for a different reason: Go never
compiles a package's `_test.go` files into anything another package can
import, so a test helper cannot be shared across packages at all.
`sql_lockstep_helpers_test.go`'s `documentationSchemaDir` walks two
directories further up than root's copy to reach the repo root
(`internal/query/package/registry/<file>` sits two levels deeper than
`internal/query/<file>`).

Both are small and self-contained, so neither carries real drift risk.

**`package_registry_nornicdb_live_test.go` and two auth-middleware tests stay
in root**, not here, even though they test this family's routes. The
NornicDB-live test drives `NewNeo4jReader`, which wraps root's shared
read-retry policy and has no leaf package this family could import without
cycling back through root; extracting it is a larger, cross-family change out
of scope for this move. The two `AuthMiddlewareWithScopedTokens` route-allowlist
tests exercise root's middleware directly and never call `Handler`.

## Related docs

- [Cypher performance](../../../../docs/public/reference/cypher-performance.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`:

```
go test ./internal/query/... ./cmd/api ./cmd/mcp-server ./internal/mcp -count=1
go test ./internal/query/package/registry -count=1 -v
go test ./internal/queryplan/ -count=1
go vet ./...
```

`cmd/api`/`cmd/mcp-server` are included because `Handler`'s compatibility
alias is exactly the surface an accidental unexported-symbol dependency would
break silently; `internal/queryplan` is included because this move's file
renames and receiver-type renames re-pin two query-plan manifests.
`internal/mcp` is included because its route-serves-data registry names
this package's files by path and its handler by struct name.
