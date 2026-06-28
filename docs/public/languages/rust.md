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

## Framework And Library Support

Supported today:

- Tokio runtime/test functions and Criterion benchmarks are modeled as derived
  roots.
- Cargo entrypoints, tests, exact `pub` API items, direct trait implementation
  methods, conditional derive evidence, and direct module/import declarations
  are modeled as root evidence.
- Rust web framework handlers are not exact route entries today; Rust does not
  emit `framework_semantics.*.route_entries` or `HANDLES_ROUTE` edges.

Not claimed today:

- Arbitrary macro expansion, `cfg` and Cargo feature solving, cross-crate
  semantic module resolution, and broad trait dispatch remain outside the
  exactness boundary.
- Exact route-to-handler truth for Axum, Actix, Rocket, and other Rust web
  frameworks is tracked by
  [#4098](https://github.com/eshu-hq/eshu/issues/4098).
