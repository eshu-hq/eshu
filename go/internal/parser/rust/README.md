# Rust Parser Adapter

## Purpose

This package owns Rust-specific tree-sitter payload extraction for functions,
types, modules, traits, impl blocks, imports, macro definitions and invocations,
calls, constants, statics, type aliases, root metadata, attributes, derives, and
generic parameter metadata.

## Ownership Boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns Rust syntax walking and payload assembly, while the parent
package keeps registry dispatch, runtime parser construction, and compatibility
method signatures.

## Exported Surface

The package exposes `Parse` for full payload extraction and `PreScan` for
dependency symbol discovery. `doc.go` carries the godoc contract for callers.

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
same-line `where` clause so downstream consumers get the syntactic receiver,
not its bound list.

Functions carry async, unsafe, visibility, attribute, and selected
`dead_code_root_kinds` metadata. Bare `fn main` roots are limited to
Cargo-shaped entrypoint paths such as `src/main.rs`, `build.rs`, `src/bin`, and
`examples`; a `#[tokio::main]` attribute is direct root evidence. `#[test]` and
`#[tokio::test]` are test roots, whether the attribute is on its own line or
directly before `fn` on the same line. Const and static items are emitted
through the `variables` bucket with `variable_kind`, `type` items through
`type_aliases`, and `macro_rules!` definitions through `macros`.

Direct `#[derive(...)]` attributes emit `derives`; conditional derives inside
`cfg_attr` stay raw in `decorators` and `attribute_paths`. Direct item
attributes may be multiline or share the item line; field-level and enum-variant
attributes must not leak onto the parent type. Macro-expanded modules/imports,
full `where` bound semantics, associated type constraints, and higher-ranked
trait bounds are not modeled yet. Add package-local tests before widening any
of those claims.

## Related Docs

See `docs/docs/reference/mcp-reference.md` for the dead-code query surface that
consumes parser root evidence.
