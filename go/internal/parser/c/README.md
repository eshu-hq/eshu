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
header or resolve transitive include graphs. Header reads are also bounded to
the caller-supplied repository root after path cleanup and symlink resolution.

## Exported Surface

The package exposes Parse for full payload extraction, PreScan for dependency
symbol discovery, and AnnotatePublicHeaderRoots for the parent parser wrapper to
mark functions declared by directly included local headers. The unexported
`annotateCDeadCodeRoots` pass adds suppressive root metadata for C entrypoints,
callbacks, signal handlers, and direct function-pointer initializer targets.
Callback roots accept bare and address-of callback arguments. Direct
function-pointer initializer roots include bare and address-of targets for
explicit pointer declarations, multiple initializer declarations, and local
typedef pointer aliases, including brace initializer tables in those
declarations.

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
roots, commented-out prototype text is ignored, and include traversal outside
the repository root is ignored. Macro expansion, conditional compilation,
transitive include graphs, dynamic symbol lookup, and broad callback registries
remain query-reported exactness blockers.

### Regex disposition (issue #3540)

The whole-source `appendCTypedefAliasesFromSource` line scan was removed. Typedef
aliases, structs, enums, and unions are now extracted solely from tree-sitter
`type_definition` and `typedef`-prefixed `declaration` nodes, which a grammar
probe confirmed cover every typedef form (including nested-brace anonymous
structs the single-level regex could mishandle). The remaining regexes operate
on already-located AST node text or bounded external header text, so they are
documented within-AST / source-text exceptions, not primary symbol extraction:

- `cTypedefAliasPattern` (`parser.go`) is a fallback over a `type_definition`
  node's own text when field-based descent does not yield the alias name.
- `cDirectInitializerTargetPattern` and `cBraceInitializerPattern`
  (`dead_code_roots.go`) parse the text of an already-located `declaration` node
  to recover function-pointer initializer targets.
- `cHeaderPrototypePattern` and the comment-stripping patterns scan the bytes of
  directly included local header files. Those headers are intentionally not
  tree-sitter parsed (to keep cost bounded and avoid repo-wide include-graph
  resolution), so a prototype scan is the only available structured read.
- `cFunctionPointerTypedefPattern` builds an auxiliary name index of
  function-pointer typedefs that gates the within-AST declaration handling above;
  it produces no symbol or edge directly.

## Related Docs

- `docs/public/languages/c.md`
- `docs/public/reference/dead-code-reachability-spec.md`
