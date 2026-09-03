# Semantic search query handlers

## Purpose

Serves the curated semantic-search read surface: bounded retrieval over the
persisted search-document corpus in keyword, semantic, and hybrid modes,
repository-to-scope resolution, graph-neighborhood reranking, corpus snapshot
caching, and the search-vector-ready freshness downgrade. One route,
`POST /api/v0/search/semantic`, hangs off `SemanticSearchHandler.Mount`.

## Ownership boundary

This package owns the semantic-search route: the handler struct, its request
normalization and parameter bounds, its Postgres index and snapshot stores, its
scope resolver, its reranker, its response models, and its degraded-search
telemetry. It does not own auth, the response-envelope and capability contract,
or the HTTP span helper — those live in `queryauth`, `querycontract`, and
`queryspan` (see Dependencies).

Root package `query` keeps compatibility aliases and forwarders
(`semantic_search_alias.go`) for `SemanticSearchHandler`, the index and hybrid
port types, the snapshot and scope-resolver types, and their constructors, so
`cmd/api` and `cmd/mcp-server` build unchanged. Root also performs this family's
capability registration (`contract_capability_matrix.go`), deliberately: root
owns the router and always links into the production binary. This package's own
tests register it themselves from `main_test.go`'s `TestMain`, because they
cannot link root (see Gotchas). Both sides register the same `Support()` value from
`capability.go` — the row itself is declared here, once, and its values are
pinned by a root test.

Three tests that drive this route live in root rather than here, because the
route is only their vehicle and the surface they actually assert on is root's:
the session/scoped-token permission parity sweep
(`session_permission_enforcement_test.go`), the scoped-token middleware
admission test (`semantic_search_route_auth_test.go`), and the OpenAPI
`languages` wire-contract test
(`semantic_search_language_wire_contract_test.go`).

Two of the three reach the handler through `Mount` and a real mux, which is what
the deployed API does: the session-permission sweep and the OpenAPI `languages`
test. The scoped-token admission test does not -- it wraps a bare
`http.HandlerFunc` in `AuthMiddlewareWithScopedTokens`, never builds a mux and
never names `SemanticSearchHandler`. That middleware consults no route table, so
the route string in it is only a vehicle and changing the real route path will
not fail it. Its own doc comment says so; do not count it as a route guard.

## Exported surface

`SemanticSearchHandler` and its `Mount`. The store ports callers implement:
`SemanticSearchIndexStore`, `SemanticSearchHybridStore`,
`SemanticSearchDocumentStore`, `SemanticSearchScopeResolver`,
`SemanticSearchSnapshotStore`, `SemanticSearchVectorMetadataStore`,
`SemanticSearchVectorValueStore`, `SemanticSearchVectorReadyReader`, and
`SearchVectorReadyQueryer`. The values crossing those ports:
`SemanticSearchIndexQuery`, `SemanticSearchIndexResult`,
`SemanticSearchSnapshot`, `SemanticSearchSnapshotRequest`, and
`SearchVectorBuildIdentity`. The Postgres implementations and their
constructors, the persisted-vector hybrid backend and its config, and
`Capability`. See `doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, all of them leaves that never import root package `query`:

- `internal/query/querycontract` — response and truth envelopes, error codes,
  capability gate, query profiles, `RepositoryAccessFilter`.
- `internal/query/queryauth` — the request-scoped `AuthContext` and the
  permission-catalog predicates `AllowsPermissionFeature` and
  `AllowsPermissionDataClasses`.
- `internal/query/queryspan` — `HandlerTracer`/`StartHandlerSpanWith`, wrapped
  by this package's own `startQueryHandlerSpan` (`handler_tracing.go`).
- `internal/searchdocs`, `internal/searchretrieval`, `internal/searchbench`,
  `internal/searchhybrid` — the document model, the retrieval runner, the mode
  vocabulary, and the embedder port.
- `internal/storage/postgres` (as `pgstatus`) — the `Queryer` surface the
  snapshot store and scope resolver read through.

Tests additionally use `internal/query/querytestutil` for
`SemanticSearchDocumentFixture`, `SemanticSearchHTTPRequest`, `ScriptedRows`,
and `WithPackageMetricReader`.

## Telemetry

- Span `query.semantic_search` (`telemetry.SpanQuerySemanticSearch`), started
  through the package-local `semanticSearchTracer` seam, with route and
  capability attributes.
- Counter `eshu_dp_search_hybrid_degraded_total`, registered lazily on meter
  `eshu/go/internal/query`. The meter name is unchanged by the package move, so
  existing dashboards and alerts keep resolving.
- Scope resolution and the index cache add span attributes rather than their own
  metrics.

## Move evidence (#6060)

This package was created by moving files out of root package `query`. No
behavior changed, and the two assertions below are structural rather than
promissory — each names what a reader can check.

No-Regression Evidence: the move is a package relocation, not a rewrite. The
performance-evidence gate flags three files, and they do not all differ from
their pre-move form for the same reason:

- `semantic_search_index_cache.go` differs only in the `package` clause and in
  the rename of `semanticSearchIndexQuery`/`semanticSearchIndexResult` to their
  exported spellings, which the exported `SemanticSearchIndexStore` interface
  requires in order to be implementable from another package at all. Check it
  with `git diff -M origin/main...HEAD --
  go/internal/query/semanticsearch/semantic_search_index_cache.go`, and note
  that git needs a pathspec spanning BOTH the old and new directories to pair
  the move as a rename at all -- a destination-only pathspec reports it as a
  new file and hides the comparison.
- `querytestutil/metricreader.go` and `querytestutil/scriptedrows.go` are not
  whole-file moves. They are extractions out of root `_test.go` files
  (`metric_reader_test.go` and `admin_replay_idempotency_test.go`), so they
  differ by the `package` clause, by the export renames the promotion needs
  (`withPackageMetricReader` to `WithPackageMetricReader`; `scriptedRows` to
  `ScriptedRows` with its `data` field exported to `Data`), and by added doc
  comments. No statement changed in either.

The
cache's bounds, LRU eviction, TTL, and filter-signature keying are byte-identical,
so its hit rate and eviction behavior under the same corpus are unchanged. The
gate matches on content, not only on paths, so a pure move trips it — there is
no measurement to report because there is no changed code path to measure, and
inventing before/after numbers for an unchanged code path would be worse than
this note.

No-Observability-Change: `eshu_dp_search_hybrid_degraded_total` is still
registered on meter `eshu/go/internal/query`
(`semantic_search_telemetry.go`), the same meter name as before the move, so
existing dashboards and alerts resolve unchanged. The span is still
`query.semantic_search` recorded under tracer name `eshu/go/internal/query`:
both root and this package obtain it from `queryspan.HandlerTracer()`, which
returns `otel.Tracer(tracerName)` for a `tracerName` const that
`queryspan/handlerspan.go` deliberately pins to the old path, with a comment
saying why. The attributes are set in one place, `StartHandlerSpanWith`, and
are still exactly `http.route`, `eshu.capability`, and `service.namespace`.
This package's local `semanticSearchTracer` var changes only which tracer
value a test can swap in, never the name production records under.

## Gotchas / invariants

- `go test ./internal/query/semanticsearch` does not link root package `query`,
  so root's `init()` capability registrations never run in this test binary.
  `main_test.go`'s `TestMain` registers the capability instead. Without it every
  handler test fails with an `unsupported_capability` 501 that has nothing to do
  with the handler.
- `Support()` (`capability.go`) is the single declaration of the support row, read
  by both registrations. Do not re-inline its fields anywhere. Its values are
  pinned independently by root's
  `semantic_search_capability_support_test.go`; this package's own suite cannot
  detect a wrong support row, because it registers whatever the row says.
- A `SemanticSearchIndexStore` must filter on both `ScopeID` and `RepoID`. They
  differ once a repository is re-ingested under a new scope, and honoring only
  one answers outside the caller's grant.
- `CorpusMayBeTruncated` is part of the answer. An empty result set with it set
  means the corpus bound was hit, not that nothing matched.
- The search-vector freshness downgrade applies only to vector-backed modes.
  `mode:"keyword"` is served entirely by the deterministic lexical index and
  must never be downgraded by a pending search-vector build
  (`searchVectorBackedMode`).
- `startQueryHandlerSpan` must forward through the package-local
  `semanticSearchTracer` var. Calling `queryspan.HandlerTracer()` inline at a
  call site compiles clean and silently emits zero spans to a test's recorder.
- Do not import root package `query`. Root's `semantic_search_alias.go` already
  imports this package, so the reverse import cycles.

## Related docs

- [HTTP API Reference](../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../docs/public/reference/telemetry/index.md)
