# Agent instructions: semanticsearch

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's `semantic_search_alias.go`
  already imports this package for its compatibility aliases, so the reverse
  import cycles. If a change needs something only root exposes, either a leaf
  equivalent already exists under `internal/query` (`querycontract`,
  `queryauth`, `queryspan`) or it does not belong in this family; ask before
  adding one.
- `startQueryHandlerSpan` MUST forward through the package-local
  `semanticSearchTracer` var (`handler_tracing.go`), never call
  `queryspan.HandlerTracer()` inline at a handler call site. The var is the
  seam a test swaps a recording provider into; bypassing it compiles clean and
  silently emits zero spans to the test's recorder.
- The capability is registered in ROOT (`contract_capability_matrix.go`), not
  here — root owns the router and always links into production, so its
  `init()`s always run there. `go test ./internal/query/semanticsearch` never
  runs root's `init()` functions (the cycle above), so `main_test.go`'s
  `TestMain` registers it for this package's tests.
- `Support()` (`capability.go`) is the ONLY declaration of the support row. Both
  registrations above read that one var. MUST NOT re-inline the five fields on
  either side. The previous shape had a copy in `main_test.go` under a "keep in
  sync" comment that nothing enforced: flipping `LocalLightweightMax` to
  non-nil and `RequiredProfile` down to `local_lightweight` left this package's
  suite at exit 0 while production served a different profile.
- The VALUES `Support()` returns are pinned by a root test,
  `semantic_search_capability_support_test.go`, which states them independently
  instead of comparing the registry back to the same var — that comparison
  would hold no matter what that function returned. Editing it reddens the test.
  Measured: the two-field flip above gives `go test ./internal/query/` exit 1
  naming both fields, while `go test ./internal/query/semanticsearch/` stays
  exit 0. This package's own suite cannot catch it, which is why the root test
  exists. Run BOTH sides after any change to `Support()`.
- `Support()` MUST stay a function that allocates its truth levels per call, and
  MUST NOT become an exported var. `CapabilitySupport` holds its ceilings as
  pointers, so a shared var gives every caller — root's production registration
  included — write access to the same ints, and `RegisterCapabilities` copies
  the struct, not what its pointers point at. The three ceilings also need
  separate variables: three pointers to one local stay aliased inside the
  returned struct. `queryauth` keeps its data-class slice unexported behind a
  function for the same reason. This file is the template later #6053 family
  moves copy, so the shape matters more here than the absent writer does.
- `Capability` (`semantic_search.go`) is the single declaration of the
  capability string. Root's `contract.go` reads it from here. MUST NOT
  reintroduce a second literal in root.
- A `SemanticSearchIndexStore` MUST filter on both `SemanticSearchIndexQuery`
  `.ScopeID` and `.RepoID`. They diverge after a repository is re-ingested
  under a new scope, and a store honoring only one answers outside the caller's
  grant. Any new store implementation needs a test proving both are applied.
- `searchVectorBackedMode` gates the search-vector freshness downgrade.
  `mode:"keyword"` is served by the deterministic lexical index and MUST NOT be
  downgraded by a pending search-vector build. Widening that gate makes every
  keyword response report a freshness gap it does not have.
- The degraded counter registers lazily under a `sync.Once`. A test observing
  it MUST go through `querytestutil.WithPackageMetricReader` with
  `resetSemanticSearchInstrumentsForTest`, and MUST NOT call `t.Parallel()` —
  the helper installs a process-global meter provider.

## Where the cross-cutting tests live

Three tests exercise this route from root package `query` on purpose. Do not
"reunite" them with the family:

- `session_permission_enforcement_test.go` — session/scoped-token authorization
  parity. Belongs to root's cross-cutting auth sweep.
- `semantic_search_route_auth_test.go` — scoped-token middleware admission.
  `AuthMiddlewareWithScopedTokens` and its resolver double live in root.
- `semantic_search_language_wire_contract_test.go` — binds root's generated
  `OpenAPISpec()` to this handler's open-pass `languages` behavior.

They drive the handler through `Mount` and a real mux. If you change the route
path or the request shape, these fail in root, not here.

## Shared test fixtures

`querytestutil` holds the fixtures both this package and root need:
`SemanticSearchDocumentFixture`, `SemanticSearchHTTPRequest`, `ScriptedRows`,
`WithPackageMetricReader`. Put a new shared fixture there rather than copying
it — Go never compiles a package's `_test.go` files into anything another
package can import, so a copy is the only alternative and copies drift.

`querytestutil` MUST NOT import this package: this package's in-package tests
import `querytestutil`, so that direction cycles. A fixture that needs a
`semanticsearch` type therefore cannot be shared, and root declares its own
double instead (`stubSemanticSearchIndex` in
`session_permission_enforcement_test.go`).

## Files must stay under 500 lines

`semantic_search.go` and the larger test files are close to the cap. Split by
concern (params, response, scope, rerank, snapshot, telemetry — the existing
split) rather than growing a file.
