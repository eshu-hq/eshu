# Rust Parser Adapter

## Purpose

This package owns Rust-specific tree-sitter payload extraction for functions,
types, traits, impl blocks, imports, macro definitions and invocations, calls,
constants, statics, type aliases, root metadata, and lifetime metadata.

## Ownership Boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns Rust syntax walking and payload assembly, while the parent
package keeps registry dispatch, runtime parser construction, and compatibility
method signatures.

## Exported Surface

The package exposes Parse for full payload extraction and PreScan for dependency
symbol discovery.

## Dependencies

This package imports the shared parser helper package and tree-sitter types. It
must not import the parent parser package.

## Operational Notes

This package emits no telemetry directly. Parser timing and runtime observability
remain owned by the parent engine.

## Current Limits

Brace imports are preserved as one raw `use` row instead of being expanded per
symbol. Lifetime names are structured when they appear in signatures and impl
headers. Functions now carry async, unsafe, visibility, and selected
`dead_code_root_kinds` metadata. Bare `fn main` roots are limited to Cargo-shaped
entrypoint paths such as `src/main.rs`, `build.rs`, `src/bin`, and `examples`;
a `#[tokio::main]` attribute is direct root evidence. `#[test]` and
`#[tokio::test]` are test roots, whether the attribute is on its own line or
directly before `fn` on the same line. Const and static items are emitted
through the `variables` bucket with `variable_kind`, `type` items through
`type_aliases`, and `macro_rules!` definitions through `macros`.

Generic type parameters, derives, module items, attribute macros beyond the
root cases above, and expanded brace-import symbols are not emitted as separate
metadata yet. Add package-local tests before widening any of those claims.
