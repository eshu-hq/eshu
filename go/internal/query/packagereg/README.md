# Package registry query handlers

## Purpose

Serves the package-registry read surface: package and version identity
lookups, package-native dependency edges, reducer-derived ownership/
consumption/publication correlations, dependency chains, and graph
aggregate/inventory counts. All routes hang off `PackageRegistryHandler.Mount`.

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

- `PackageRegistryHandler` and `Mount` -- the HTTP entry point.
- `PackageRegistryCorrelationStore`, `PackageRegistryAggregateStore` -- the
  two storage ports the handler depends on, plus their production
  implementations `PostgresPackageRegistryCorrelationStore` and
  `GraphPackageRegistryAggregateStore` and their `New*` constructors.
- `PackageRegistryCorrelationRow`, `PackageRegistryCorrelationFilter`,
  `PackageRegistryCorrelationPage`, and the aggregate/inventory result types.
- `PackageRegistryDependenciesCypher` -- exported (unlike this file's other
  Cypher builders) because `go/internal/query/queryplan_legacy_production_binding_test.go`
  drives the real production statement through the query-plan comparison it
  runs for every handler family; see Gotchas below.

## Dependencies

The Go standard library, `database/sql`, `go/internal/storage/postgres/pgarray`,
`sdk/go/factschema` (and its `reducerderived/v1` package), `go/internal/scope`,
`go/internal/telemetry`, and these `internal/query` leaf packages:

- `querycontract` -- `GraphQuery`, `ContentStore`, response/truth envelopes,
  capability gates, row-value decoders, `CollectorListReadinessStore` and its
  two `Build*` functions.
- `querydecode` -- the classified fact-decode failure
  (`*querydecode.Error`) this package's correlation decode wrappers return.
- `queryselector` -- `ResolveForRequestWithAccess`, the repository-selector
  resolution this package's correlation and dependency-chains handlers use.
- `queryauth` -- `AuthContext`, `AuthContextFromContext`,
  `RepositoryAccessFilterFromContext` (via `querycontract`), the scoped-token
  authorization bounds every scoped-access test drives.
- `queryspan` -- the per-route HTTP span (see Gotchas below).

It does **not** import root package `query`: that import would cycle, since
root imports this package for the compatibility aliases above.

## Telemetry

Spans: every handler starts a span via `startQueryHandlerSpan`
(`handler_tracing.go`), forwarding to `queryspan.StartHandlerSpanWith` under
the unchanged `eshu/go/internal/query` instrumentation-scope name, so
existing span queries and dashboards are unaffected by the move.

No new metrics or logs were added by the move itself.

## Gotchas / invariants

**`main_test.go`'s `TestMain` is not redundant with root's capability
registrations.** `go test ./internal/query/packagereg` never runs root package
`query`'s `init()` functions (the import would cycle), so without `TestMain`
registering the same six capabilities directly with `querycontract`, every
handler test in this package fails with the capability gate's
`unsupported_capability` 501 -- not because the handler is broken, but because
nothing ever registered a capability for it to check against. Production is
unaffected: root always links into the real binary and always runs its own
`init()`s. Keep `TestMain`'s values in sync with
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
(`package_registry_collector_readiness.go`) mirror root's
`collector_list_readiness.go` exactly. A nil store yields no envelope. A page
with `resultsReturned > 0` is classified `ready_with_results` WITHOUT calling
the configured-collector probe: returned rows are themselves proof the
collector ran, so a failing or stale probe must never downgrade an
already-evidenced page. The probe runs only for an empty page, to
disambiguate `not_configured` from `ready_zero_results`; a probe error there
yields `readiness_unavailable` so the page is never dropped. Getting this
order wrong (checking the probe first) is a real behavior regression, not a
style choice.

**`PackageRegistryDependenciesCypher` is pinned in the queryplan manifest.**
`go/internal/queryplan/testdata/hot-cypher.yaml`'s `QP-SC-PKGREG-DEPS` entry
carries a `source_sha256` over this function's source text. Any edit --
including a rename -- fails `TestLegacyQueryplanManifestBindsProductionQueries`
until the digest is re-pinned; re-pin only after proving the Cypher text
itself did not change.

**Two test doubles are local copies of root test helpers, not forks.** Go
never compiles a package's `_test.go` files into anything another package can
import, so `workItemDerefString`/`workItemDerefBool`
(`package_registry_correlation_deref.go`) and the slice-comparison/SQL-lockstep
helpers (`package_registry_slice_test_helpers_test.go`,
`package_registry_sql_lockstep_helpers_test.go`) are this family's own copies
of small, self-contained root helpers -- not forks of anything with real
drift risk.

**`package_registry_nornicdb_live_test.go` and two auth-middleware tests stay
in root**, not here, even though they test this family's routes. The
NornicDB-live test drives `NewNeo4jReader`, which wraps root's shared
read-retry policy and has no leaf package this family could import without
cycling back through root; extracting it is a larger, cross-family change out
of scope for this move. The two `AuthMiddlewareWithScopedTokens` route-allowlist
tests exercise root's middleware directly and never call
`PackageRegistryHandler`.

## Related docs

- [Cypher performance](../../../../docs/public/reference/cypher-performance.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Performance and observability evidence

No-Regression Evidence: this package is a relocation of the package-registry
handler family out of `go/internal/query`'s flat root (#6060). No query text
changed, so there is no before/after latency to report and none is claimed.

- Baseline and after: `origin/main` (1f0e1e172) versus this branch. The emitted
  Cypher is byte-identical across the move. Verified two ways rather than
  asserted: the string literals in `package_registry_cypher.go` were diffed
  against their pre-move originals and match exactly, and the anchor census is
  unchanged at `MATCH (p:Package` x11, `MATCH (d:PackageDependency` x2,
  `MATCH (v:PackageVersion` x1. The only Go-level delta in that file is the
  rename of `packageRegistryDependenciesCypher` to its exported form, which the
  `queryplan` manifest records with a new `source_sha256` because the digest
  covers the identifier as well as the body.
- Backend and version: unchanged. This package selects no backend and issues no
  DDL; it reads through `querycontract.GraphQuery`, exactly as the same code did
  from root.
- Input shape and row counts: unchanged, and both aggregate reads carry a bound
  the code enforces. `PackageRegistryPackageInventory` is capped at
  `PackageRegistryAggregateMaxLimit+1` (501) by the store's own limit check plus
  `SKIP $offset LIMIT $limit` in the query template.
  `CountPackageRegistryPackages` is a single-row `count(p)` plus a group-by whose
  cardinality is bounded by the closed 13-value `packageidentity.Ecosystem` enum
  (registered `max_results: 32`). Both are recorded as typed `non_hot`
  dispositions in `go/internal/queryplan/testdata/query-source-coverage.yaml`.
- Terminal queue counts: not applicable. This package enqueues nothing and owns
  no worker, lease, or batch path.

No-Observability-Change: the spans, metrics and logs an operator sees are
the same ones root emitted. `handler_tracing.go` here holds a package-local
`packageregTracer = queryspan.HandlerTracer()` and a `startQueryHandlerSpan`
that forwards to `queryspan.StartHandlerSpanWith`, so the tracer scope name and
every span attribute are unchanged from root's helper. No metric was added,
renamed or removed, and no log key changed.

Why the change is safe: `go test ./internal/mcp -count=1` passes, which is the
load-bearing proof — every external `query.<Type>` reference still resolves
through root's forwarders with no caller edited. The B-7 golden corpus and the
B-12 snapshot are untouched (`git diff --name-only origin/main..HEAD --
testdata/` returns nothing), so projected graph truth and query response shapes
are unchanged by construction rather than by assertion.
