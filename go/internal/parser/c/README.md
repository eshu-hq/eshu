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

`annotateCDeadCodeRoots` does not run its own tree-sitter walk. `Parse`'s main
payload walk gathers `call_expression` and `declaration` node pointers
(`shared.CloneNode`) into one ordered slice as it visits them, and
`annotateCDeadCodeRoots` resolves that slice in an in-memory loop once
`payload["functions"]` is fully populated, instead of re-walking the whole
tree a second time (issue #4870). See
[docs/public/languages/c.md](../../../../docs/public/languages/c.md#dead-code-roots-walk-merge-issue-4870)
for the performance evidence and the ordering invariant this depends on.

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

### Regex disposition (issues #3540, #3573)

The whole-source `appendCTypedefAliasesFromSource` line scan was removed in
#3540, and the residual `cTypedefAliasPattern` fallback in `parser.go` was
removed in #3573. Typedef aliases, structs, enums, and unions are now extracted
solely from tree-sitter `type_definition` and `typedef`-prefixed `declaration`
nodes. A grammar probe confirmed `type_definition` exposes the alias under the
`declarator` field for every form — `type_identifier` for plain and anonymous
struct/enum/union bodies, `function_declarator` for function-pointer typedefs,
and `array_declarator` for array typedefs — and the underlying struct/enum/union
under the `type` field, so the regex fallback was unreachable for any C the
grammar parses. No payload key, bucket assignment, or alias value changed.

The remaining `parser/c` regexes all live in `dead_code_roots.go` and operate on
already-located AST node text or bounded external header text, so they are
documented out-of-AST / source-text evidence exceptions, not primary symbol
extraction:

- `cDirectInitializerTargetPattern` and `cBraceInitializerPattern` parse the
  text of an already-located `declaration` node to recover function-pointer
  initializer targets. This is call-site / initializer evidence used to mark
  functions referenced indirectly through pointers, not symbol extraction.
- `cHeaderPrototypePattern` and the comment-stripping patterns
  (`cBlockCommentPattern`, `cLineCommentPattern`) scan the bytes of directly
  included local header files read via `os.ReadFile`. Those headers are not part
  of the tree-sitter parse of the current source and are intentionally not
  parsed (to keep cost bounded and avoid repo-wide include-graph resolution), so
  a raw prototype scan over the unparsed file is the only available structured
  read.
- `cFunctionPointerTypedefPattern` builds an auxiliary name index of
  function-pointer typedefs that gates the within-declaration handling above; it
  produces no symbol or edge directly.

## Related Docs

- `docs/public/languages/c.md`
- `docs/public/reference/dead-code-reachability-spec.md`
