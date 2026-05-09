# Rust Parser Adapter

## Purpose

This package owns Rust-specific tree-sitter payload extraction for functions,
types, traits, impl blocks, imports, macro invocations, calls, and lifetime
metadata.

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
headers, but generic type parameters, attributes and derives, module items,
consts, statics, type aliases, and unsafe or async semantics are not emitted as
separate metadata yet. Add package-local tests before widening any of those
claims.
