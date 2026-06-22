# Swift Parser

## Purpose

`internal/parser/swift` owns Swift language extraction without importing the
parent `internal/parser` package. It emits the Swift payload, dead-code root
hints, and pre-scan names for imports, nominal types, functions, variables, and
bounded call metadata.

## Ownership boundary

The package owns Swift parse and pre-scan behavior plus the line-based helper
logic used by those operations. The parent parser still owns registry dispatch,
repository-level pre-scan orchestration, and content metadata enrichment.

## Exported surface

The godoc contract is in `doc.go`. Current exports are:

- `Parse` extracts Swift imports, types, functions, variables, calls, and
  parser-backed dead-code root kinds for known Swift runtime entrypoints.
- `PreScan` returns deterministic names for import-map pre-scan.

## Dependencies

This package imports `internal/parser/shared`, `go-tree-sitter`, and the Go
standard library. It must not import the parent parser package.

## Telemetry

This package emits no metrics, spans, or logs. Parser timing remains owned by
the collector snapshot path.

## Gotchas / invariants

`Parse` walks the tree-sitter AST once (`ast_extract.go`) and emits every bucket
from node ranges; there is no line-scan path. Tree-sitter supplies declaration
spans, inheritance clauses, function and initializer spans, parameters, property
declarations, and call targets. `helpers.go` keeps only the pure dead-code root
classification, which reads same-file conformance and protocol-method facts built
from the AST (`tree_sitter_syntax.go`). Keep output deterministic because pre-scan
output feeds repository import maps.

`extension Foo { ... }` declares no new type, so it emits no type entity. The
grammar models it as a `class_declaration` whose extended type is a `user_type`
child rather than a `type_identifier` name field, so `swiftExtensionTypeName`
resolves the extended type name and the walk pushes an `extension` scope. Members
declared inside the extension are attributed to the extended type via
`class_context`.

Only genuine `call_expression` nodes produce `function_calls` rows. The AST walk
therefore fixes line-scan false positives the prior regex emitted: enum case
declarations (`case success(Value)`), `mutating`/`override` declaration lines,
`private(set)` modifiers, and string interpolation are no longer recorded as
calls, and real subscript, initializer, and chained-method calls the scanner
missed are now captured. `super`/`self` receiver calls keep their receiver text.

The Vapor `use:` route hint is the one documented permanent exception: it has no
symbol-node form, so `collectSwiftVaporRouteHandlers` reads it as framework
evidence from the `value_argument_label` `use`, feeding the
`swift.vapor_route_handler` dead-code root without emitting a symbol row.

Dead-code roots must be emitted as `dead_code_root_kinds` metadata, not query
source fallbacks. Current roots cover `@main` types, `main`, SwiftUI `App` types
and `body`, protocol methods and same-file implementations, constructors on
concrete types, overrides, UIKit application delegate callbacks, Vapor route
handlers, XCTest methods, and Swift Testing `@Test` functions. Helpers should stay
package-local unless another language-owned package has a real caller.

No-Regression Evidence: the comprehensive-fixture golden gate
`TestSwiftComprehensiveSymbolExtractionGate` and the parity tests
`TestDefaultEngineParsePathSwift*` pin imports, bases, class context, args, end
lines, variable types, constructor roots, protocol roots, dead-code roots, and
pre-scan names; `go test ./internal/parser/... -count=1` passes (1269 tests).
`engine_swift_ast_migration_test.go` red-proves the regex-false-positive removal
and full-body source span against pre-migration `main`. No-Observability-Change:
this parser-local extraction change adds no metric, span, log, status field,
queue behavior, graph query, environment variable, or runtime knob.

## Related docs

- `docs/public/languages/support-maturity.md`
