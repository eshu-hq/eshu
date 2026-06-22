# Kotlin Parser

## Purpose

This package owns Kotlin source extraction for the parser engine. It turns one
Kotlin file into parser payload buckets for declarations, imports, variables,
function calls, receiver type inference, smart casts, parser-backed dead-code
roots, and package-bounded function return lookups.

Extraction walks the tree-sitter Kotlin AST. The package holds no regular
expressions and performs no line-scan symbol extraction: declarations, imports,
variables, calls, receiver/type inference, smart-cast flow, scope functions,
cast receivers, and sibling return-type lookups are derived from node kinds,
ranges, and child relationships.

## Ownership boundary

The package owns Kotlin parsing only. Parent engine dispatch, repository path
resolution, registry lookup, and runtime selection stay in go/internal/parser.
The child package must stay independent of the parent package and use shared
parser helpers for common payload and source behavior.

## Exported surface

See doc.go for the godoc contract.

- `Parse` reads one Kotlin file and returns the payload consumed by the
  collector path. The entry point is parser.go:10 and delegates to the AST
  walker in ast_walk.go.
- `PreScan` returns function, class, and interface names through the same
  extraction path used by `Parse`. The entry point is prescan.go:7.

## Internal layout

- `ast_walk.go` — the recursive AST walker, its mutable state, and the scope
  `frame` threaded through recursion.
- `ast_declarations.go` — classes, objects, companion objects, interfaces,
  enums, imports, type parameters, implemented supertypes, primary-constructor
  property types, and the structural pre-pass that builds file-level context.
- `ast_functions.go` — function and secondary-constructor rows, suspend,
  override, extension receivers, annotations, and dead-code-root inputs.
- `ast_variables.go` — property declarations, local/class type inference, and
  smart-cast flow for `if (x is T)` and `when (subject) { is T -> }`.
- `ast_calls.go` — call extraction for `call_expression` and `infix_expression`
  nodes, constructor-call detection, receiver inference, chained calls, cast
  receivers, and full_name reconstruction.
- `receiver_inference.go` / `type_reference.go` — pure receiver and type-algebra
  helpers fed AST-derived strings.
- `dead_code_roots.go` — annotation/name/membership classification of bounded
  dead-code roots.
- `repository_returns.go` — bounded, package-aware sibling return-type
  collection; each sibling file is parsed with tree-sitter.
- `helpers.go` / `scope_function_helpers.go` — string utilities (chain
  normalization, scope-function stripping) that operate on AST-derived text.

## Dependencies

The package imports go/internal/parser/shared for `shared.Options`, source
reading, base payload construction, bucket appends, sorting, and name
deduplication, plus go-tree-sitter for AST traversal. Standard-library
dependencies cover filesystem walking through bounded directories, path
normalization, and string processing.

## Telemetry

This package emits no metrics, spans, or structured logs. Parser runtime
telemetry is owned by the collector and runtime layers that call the parser.

## Gotchas / invariants

`Parse` must preserve the parent payload keys and keep deterministic bucket
ordering before returning. `kotlinFunctionDeadCodeRootKinds` in
dead_code_roots.go only emits bounded parser-backed roots for Kotlin
entrypoints, interfaces, overrides, Gradle, Spring, lifecycle, and JUnit
callbacks. Receiver inference depends on local variables, class properties,
sibling function returns, and type-parameter resolution; the sibling return-type
collection is bounded by the repository root and nearby Kotlin directories so it
does not scan the whole workspace. A companion object's members carry the
enclosing class as their `class_context`, not the companion's own name.

`Parse` extracts calls through several bounded paths that share one per-line
`seenLineCalls` dedup set: receiver-qualified and chained calls
(`kotlinCallPattern`), `this.` calls, infix calls, constructor calls to known
type names, and unqualified bare calls (`kotlinAppendBareCalls`). Bare-call
extraction covers same-scope, top-level, and imported function calls that have no
receiver; it skips qualified calls, declaration and control-flow keywords, and
method-chain receivers.

Bare calls skip only locally-declared types as constructor candidates, never
import aliases. Kotlin imports do not distinguish a top-level function from a
type, so an imported name such as `helper` from `import demo.util.helper` must
still emit a call edge. The constructor-call path runs first and records the same
`name#line` key in `seenLineCalls`, so a genuinely imported constructor such as
`Widget()` is emitted once by the constructor path and skipped by the bare-call
path without dropping imported function calls.

## Related docs

- docs/public/architecture.md
- docs/public/reference/local-testing.md
