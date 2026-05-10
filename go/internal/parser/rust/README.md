# Rust Parser Adapter

## Purpose

This package owns Rust-specific tree-sitter payload extraction for functions,
types, modules, traits, impl blocks, imports, macro definitions and invocations,
calls, constants, statics, type aliases, root metadata, attributes, derives, and
generic parameter metadata, conditional derive evidence, nested field and enum
variant attributes, and structured where-clause evidence.

## Ownership Boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns Rust syntax walking and payload assembly, while the parent
package keeps registry dispatch, runtime parser construction, and compatibility
method signatures.

## Exported Surface

The package exposes `Parse` for full payload extraction, `PreScan` for
dependency symbol discovery, and `ResolveModuleRowFileCandidates` with
`ModuleResolution` for filesystem candidate calculation from parser-emitted
module rows. `doc.go` carries the godoc contract for callers.

## Dependencies

This package imports the shared parser helper package and tree-sitter types. It
must not import the parent parser package.

## Telemetry

This package emits no telemetry directly. Parser timing and runtime observability
remain owned by the parent engine.

## Gotchas / Invariants

Brace imports are expanded into one `imports` row per syntactic import while
preserving the raw `full_import_name` on every row. Module declarations and
inline modules emit `modules` rows with `module_kind`. Lifetime names are
structured when they appear in signatures and impl headers; type and const
generic parameter names are emitted as conservative name lists without merging
`where` bounds. Impl block `target` metadata stops before a multiline or
same-line `where` clause so downstream consumers get the syntactic receiver.
Where predicates are still preserved separately as parser evidence, with
associated-type constraints and higher-ranked trait-bound predicates broken out
when the syntax is direct.

Functions carry async, unsafe, visibility, attribute, impl-context, and selected
`dead_code_root_kinds` metadata. Bare `fn main` roots are limited to
Cargo-shaped entrypoint paths such as `src/main.rs`, `build.rs`, `src/bin`, and
`examples`; a `#[tokio::main]` attribute is direct root evidence. `#[test]` and
`#[tokio::test]` are test roots, whether the attribute is on its own line or
directly before `fn` on the same line. Exact `pub` visibility marks functions,
classes, traits, and type aliases with `rust.public_api_item`; scoped
visibility such as `pub(crate)` does not. Methods inside `impl Trait for Type`
blocks carry `impl_kind=trait_impl`, `trait_context`, and
`rust.trait_impl_method` root evidence so cleanup analysis does not delete
runtime-dispatched trait methods by local inbound-edge shape alone.
Criterion-style `criterion_group!` targets and direct `#[bench]` /
`#[divan::bench]`-style attributes mark file-local benchmark functions with
`rust.benchmark_function`; target extraction accepts identifier targets and
leaves generated or expression-based targets unclaimed. Const and static items
are emitted through the `variables` bucket with `variable_kind`, `type` items
through `type_aliases`, and `macro_rules!` definitions through `macros`.

Direct `#[derive(...)]` attributes emit `derives`; conditional derives inside
`cfg_attr` emit `conditional_derives` so consumers do not mistake them for
unconditional type behavior. Direct item attributes may be multiline or share
the item line. Field-level and enum-variant attributes emit `annotations` rows
with `owner`, `target`, and `target_kind` metadata instead of leaking onto the
parent type. Module declaration rows include `declared_path_candidates` such as
`api.rs` and `api/mod.rs`, relative to the current file directory; explicit
`#[path = "..."]` attributes replace those candidates with the declared path and
mark `module_path_source=path_attribute`. Literal `mod ...;` and `use ...;`
declarations inside macro invocation bodies are modeled with
`module_origin=macro_invocation` or `import_origin=macro_invocation`.
Items gated by `cfg` or `cfg_attr` carry `exactness_blockers=cfg_unresolved`;
macro-origin module and import rows carry
`exactness_blockers=macro_expansion_unavailable`.

`parseCargoCfgManifest` is an intentionally bounded Cargo.toml scanner for the
signals future cfg resolution needs: package name, workspace members, feature
names, default feature members, and target cfg dependency sections. It ignores
dynamic or unsupported TOML instead of guessing. `ResolveModuleRowFileCandidates`
does not probe the filesystem; it returns Rust's candidate paths for direct
module declarations, honors explicit `#[path = "..."]` rows, and leaves
macro-origin rows blocked. The parent parser engine uses that helper during
ParsePath to annotate module rows with `resolved_path_candidates`,
`resolved_path`, and `module_resolution_status` when the current repo root is
available.

Arbitrary macro expansion, Cargo feature selection, cfg evaluation, workspace
feature solving, and cross-crate semantic module resolution are still not
modeled. Add package-local tests before widening either claim.

## Related Docs

See `docs/docs/reference/mcp-reference.md` for the dead-code query surface that
consumes parser root evidence.
