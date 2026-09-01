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
Production Kotlin code and same-package tests stay independent of the parent
package and use shared parser helpers for common payload and source behavior.
External `kotlin_test` files may import the parent package to exercise the
exported engine contract as a caller would.

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
- `engine_kotlin_*_test.go` (16 files, which already includes
  `engine_kotlin_constructor_calls_test.go` and the symbol gate)
  — external-package (`kotlin_test`) engine tests that pin the full Kotlin
  contract through the parent parser's exported `DefaultEngine`/`ParsePath`
  API: bare and imported bare calls, receiver/call metadata (`this`, local,
  cast, infix, object, companion, generic, chained, safe-call, and
  primary-constructor-property receivers), interface `class_context`,
  smart-cast flow (`if`/`when`, generic, no-leak-across-branches), scope
  functions (`apply`/`also`), lazy delegated properties, suspend functions,
  same-file and cross-file/package-aware function-return aliasing, the
  repository-boundary guard for `Parse` and `PreScan`, the tree-sitter
  multiline/nested-class-scoping regressions, the AST fixture-corpus walk, and
  the `TestKotlinComprehensiveSymbolExtractionGate` golden-fixture gate.
  `engine_kotlin_test_helpers_test.go` carries the local copies of assertion
  and fixture-write helpers these files share; `parsertest` supplies the rest.
  Relocated from `go/internal/parser` (issue #6062) following the Elixir
  precedent (#6335): package-owned tests move with the engine tests that
  exercise them, while a helper still shared with tests that stay at the
  parent keeps its own local copy here instead of exporting a parent-private
  helper.

## Dependencies

Production package code imports go/internal/parser/shared for `shared.Options`,
source reading, base payload construction, bucket appends, sorting, and name
deduplication, plus go-tree-sitter for AST traversal. The external `kotlin_test`
engine tests import the parent parser only to verify `DefaultEngine.ParsePath`
(and `PreScanRepositoryPaths`) from outside the implementation package, and
import `go/internal/parser/parsertest` for the bucket/field assertions shared
with other language packages. Standard-library dependencies cover filesystem
walking through bounded directories, path normalization, and string
processing.

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

`Parse` walks the tree-sitter AST and emits calls from one dispatch on each
`call_expression`: receiver-qualified and chained calls take the navigation
path (`emitNavigationCall`), while receiver-less `name(args)` calls take
`emitIdentifierCall`. A single identifier call is classified once — it is either
a constructor or a bare call, never both — so no separate per-line dedup set is
needed. Bare-call extraction covers same-scope, top-level, and imported function
calls that have no receiver; it skips declaration and control-flow keywords and
method-chain receivers (`callIsChainReceiver`).

`emitIdentifierCall` treats a name as a constructor when it is in
`knownTypeNames` and looks type-shaped (`kotlinLooksLikeTypeName`); otherwise it
is a bare call. The pre-pass records two name sets: `knownTypeNames` holds both
file-local type declarations and the simple name an import introduces (its `as`
alias, or the last path segment of a regular import via `importedSimpleName`),
so a constructor call like `Widget()` after `import com.acme.Widget` is
recognized. `localTypeNames` holds only file-local declarations. The bare-call
path skips only `localTypeNames`, never imported names, because Kotlin imports do
not distinguish a top-level function from a type — so an imported name such as
`helper` from `import demo.util.helper` still emits a call edge while the
imported `Widget()` is emitted exactly once through the constructor branch.

## Related docs

- docs/public/architecture.md
- docs/public/reference/local-testing.md
