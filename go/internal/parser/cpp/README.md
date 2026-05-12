# C++ Parser Adapter

## Purpose

This package owns C++-specific tree-sitter payload extraction for functions,
classes, structs, enums, unions, includes, macros, typedef aliases, calls, and
dead-code root metadata.

## Ownership Boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns C++ syntax walking and payload assembly, while the parent package
keeps registry dispatch, runtime parser construction, and compatibility method
signatures.

## Exported Surface

The package exposes `Parse` for full payload extraction, `PreScan` for
dependency symbol discovery, and `AnnotatePublicHeaderRoots` for bounded
same-source local header root annotation after imports have been extracted.

## Dependencies

This package imports the shared parser helper package and tree-sitter types. It
must not import the parent parser package.

## Operational Notes

This package emits no telemetry directly. Parser timing and runtime observability
remain owned by the parent engine.

Dead-code roots are intentionally derived, not exact. `Parse` marks direct
evidence for `cpp.main_function`, virtual and override methods, direct callback
argument targets, direct function-pointer initializer targets, and Node
native-addon entrypoints.
`AnnotatePublicHeaderRoots` marks functions and methods declared in directly
included local headers as `cpp.public_header_api` roots after checking that the
header path stays inside the repository root. It does not recurse through
include graphs or resolve build targets, template instantiations, overload
sets, broad virtual dispatch, dynamic symbol lookup, or external linkage.
