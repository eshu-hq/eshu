# C Parser Adapter

## Purpose

This package owns C-specific tree-sitter payload extraction for functions,
types, includes, macros, typedef aliases, variables, calls, and bounded
dead-code root metadata.

## Ownership Boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns C syntax walking and payload assembly, while the parent package
keeps registry dispatch, runtime parser construction, and compatibility method
signatures. Header public API roots are bounded to local headers directly
included by the parsed C source; this package does not scan every repository
header or resolve transitive include graphs.

## Exported Surface

The package exposes Parse for full payload extraction, PreScan for dependency
symbol discovery, and AnnotatePublicHeaderRoots for the parent parser wrapper to
mark functions declared by directly included local headers. The unexported
`annotateCDeadCodeRoots` pass adds suppressive root metadata for C entrypoints,
callbacks, signal handlers, and direct function-pointer initializer targets.

## Dependencies

This package imports the shared parser helper package and tree-sitter types. It
must not import the parent parser package. It uses standard library filesystem
reads only for directly included local headers passed through the parent engine.

## Telemetry

This package emits no telemetry directly. Parser timing and runtime observability
remain owned by the parent engine.

## Gotchas / Invariants

C dead-code roots are parser metadata, not exact reachability proof. The package
marks `main`, signal-handler arguments, callback argument targets, direct
function-pointer initializer targets, and functions declared by directly
included local headers. `static` header prototypes do not become public API
roots. Macro expansion, conditional compilation, transitive include graphs,
dynamic symbol lookup, and broad callback registries remain query-reported
exactness blockers.

## Related Docs

- `docs/docs/languages/c.md`
- `docs/docs/reference/dead-code-reachability-spec.md`
