# Rust Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract

| Field | Value |
| --- | --- |
| Language | `rust` |
| Family | `language` |
| Parser | `DefaultEngine (rust)` |
| Entrypoint | `go/internal/parser/rust_language.go` |
| Package detail | `go/internal/parser/rust/README.md` |
| Fixture repo | `tests/fixtures/ecosystems/rust_comprehensive/` |
| Main parser tests | `go/internal/parser/engine_systems_test.go`, `go/internal/parser/rust/*_test.go` |
| Runtime validation | Compose-backed fixture verification; see [Local Testing](../reference/local-testing.md) |

## Supported Route Surfaces

| Capability | Feature key | Status | Evidence | Current truth |
| --- | --- | --- | --- | --- |
| Axum/Actix/Rocket route truth | `axum-actix-rocket-route-truth` | supported | `go/internal/parser/rust_route_entries_test.go::TestDefaultEngineParsePathRustEmitsExactFrameworkRouteEntries`, `go/internal/reducer/handles_route_rust_test.go::TestBuildHandlesRouteIntentRowsEmitsRustAxumRouteMatches`, `go/internal/query/content_reader_framework_routes_rust_test.go::TestParseFrameworkSemanticsExtractsRustFrameworkRoutes` | Direct Axum `Router::new().route("...", get(handler))` chains, Actix route attributes resolved to `actix_web`, and Rocket route attributes resolved to `rocket` emit exact `framework_semantics.*.route_entries` when the source proves literal path, literal HTTP method, and handler identifier. `HANDLES_ROUTE` is projected only when the reducer resolves that handler exactly. |

## Supported Surfaces

The Rust parser emits functions, structs, enums, traits, imports, function and
method calls, scoped calls, modules, constants, statics, type aliases, macro
evidence, generic metadata, derive metadata, where-clause evidence, and impl
blocks.

Impl blocks are first-class enough for graph-first `code/language-query`,
`code/call-chain`, entity-context, and `code/relationships` surfaces. The
query path preserves impl-block `kind`, `trait`, `target`, lifetime metadata,
and semantic summaries when the graph or content rows carry that evidence.

Primary proof:

- `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathRust`
- `go/internal/parser/engine_systems_test.go::TestDefaultEngineParsePathRustImplBlocks`
- `go/internal/parser/engine_rust_lifetimes_test.go::TestDefaultEngineParsePathRustCapturesImplLifetimes`
- `go/internal/query/code_relationships_rust_graph_test.go::TestHandleRelationshipsReturnsGraphBackedRustImplBlockOwnership`
- `go/internal/query/content_relationships_rust_test.go::TestBuildContentRelationshipSetRustImplBlockContainsMethods`

## Known Limitations
- Bounded lifetime metadata is preserved on function and impl signatures, but
  lifetime-aware graph semantics are not first-class beyond parser/query
  metadata.
- Macro-generated code is not traversed.
- `cfg` and Cargo feature solving, cross-crate semantic module resolution, and
  broad trait dispatch remain outside the documented exactness boundary.
- Generated route tables, dynamic paths, closure handlers, cfg-gated route
  declarations, macro-expanded routes, variable-held routers, and Cargo feature
  selection do not emit exact route entries.

## Parser Performance

`Parse` walks the tree-sitter AST once per file instead of three times
(issue [#4840](https://github.com/eshu-hq/eshu/issues/4840), child of epic
[#4831](https://github.com/eshu-hq/eshu/issues/4831)). Previously
`rustBenchmarkFunctionNames`, the main payload walk, and axum route detection
(inside `buildRustFrameworkSemantics`) each performed a separate full
`shared.WalkNamed` traversal over the same tree. The axum route scan is now
folded into the main payload walk: `call_expression` candidates (their text
and end byte, matching the ordering the walk always applied) are collected
in-line while the main walk already visits every `call_expression` node for
`function_calls`, and route resolution is deferred to
`buildRustFrameworkSemantics` because it needs the full import table, which is
only complete once the main walk finishes. The `rustBenchmarkFunctionNames`
pre-pass stays a separate walk before the main walk, because
`criterion_group!` can name a benchmark function that a single forward pass
has not reached yet, and moving that lookup after the main walk would reorder
`dead_code_root_kinds` for benchmark functions.

Parser output is byte-identical before and after this change: a corpus dump
across all `.rs` fixtures under `tests/fixtures`, canonicalized to
recursively key-sorted JSON and hashed per file per `Options` variant
(`Options{}` and `Options{IndexSource: true}`), produced a `0/0` symmetric
diff between the pre-change and post-change dumps
(`go/internal/parser/rust/equivalence_dump_test.go`).

## Framework And Library Support

Supported today:

- Tokio runtime/test functions and Criterion benchmarks are modeled as derived
  roots.
- Cargo entrypoints, tests, exact `pub` API items, and direct trait
  implementation methods are modeled as root evidence (`rust.main_function`,
  `rust.test_function`/`rust.tokio_test`, `rust.tokio_main`,
  `rust.public_api_item`, `rust.trait_impl_method`).
- Conditional derive evidence and direct module/import declarations are
  parsed and recorded (`derives`, `conditional_derives`, the `modules`
  bucket) but are not modeled as dead-code root evidence -- no root kind in
  `go/internal/parser/rust/helpers.go` or `metadata.go` reacts to a derive
  or a module/import declaration.
- Direct Axum `Router::new().route(...)` calls emit exact
  `framework_semantics.axum.route_entries` when the path is literal, the method
  helper resolves to `axum::routing`, and the handler is a bare same-file
  identifier.
- Direct Actix and Rocket route attributes emit exact
  `framework_semantics.actix_web.route_entries` and
  `framework_semantics.rocket.route_entries` when the attribute is
  crate-qualified or imported/aliased from the framework and the path is
  literal.

Not claimed today:

- Arbitrary macro expansion, `cfg` and Cargo feature solving, cross-crate
  semantic module resolution, and broad trait dispatch remain outside the
  exactness boundary.
- Broader route-to-handler truth for generated Axum routers, Actix service
  configuration, Rocket mounting/ranking/runtime guards, and other Rust web
  frameworks is tracked by
  [#4163](https://github.com/eshu-hq/eshu/issues/4163).
