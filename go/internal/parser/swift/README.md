# Swift Parser

## Purpose

`internal/parser/swift` owns Swift language extraction without importing the
parent `internal/parser` package. It emits the Swift payload, dead-code root
hints, and pre-scan names for imports, nominal types, functions, variables, and
call metadata, all derived from the tree-sitter AST.

## Ownership boundary

The package owns Swift parse and pre-scan behavior plus the AST node-walk helper
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

Extraction is AST-only. The tree-sitter node walk is the sole primary extractor
for imports, nominal types, functions, initializers, variables/properties,
calls, and dead-code root classification. There is no line-scan regex symbol
extraction and no `regexp` import. Keep output deterministic because pre-scan
output feeds repository import maps.

Two passes run over one parse tree. The first records whole-file semantic facts
(type conformances, protocol method sets including `init`, and Vapor `use:`
route handlers). The second emits payload rows, using the facts to classify
dead-code roots that need whole-file knowledge.

A declaration reports its keyword line, not a leading attribute line. The walk
uses the first non-`modifiers` child so `@main`/`@Test`/`@available` on a
preceding line do not shift the reported `line_number` or stored function
`source` header.

`extension Foo { ... }` declares no new type, so it emits no type entity. The
grammar models it as a `class_declaration` whose extended type is a `user_type`
child rather than a `type_identifier` name field, so `swiftExtensionTypeName`
resolves the extended type name and pushes an `extension` scope. Members
declared inside the extension are attributed to the extended type via
`class_context`.

Properties are extracted wherever they appear, including inside function bodies
(`property_declaration`) and protocol bodies (`protocol_property_declaration`).
Names are deduplicated file-wide with first-occurrence winning, so the emit walk
must visit nodes in source order.

Calls come from `call_expression` nodes. A navigation callee
(`receiver.method(...)`, including `super`/`self` receivers) emits a receiver row
with a receiver-prefixed `full_name` and `inferred_obj_type`; a bare identifier
callee emits a plain row. Enum cases, attribute argument lists, and declaration
headers are not `call_expression` nodes and so never produce call rows.

Dead-code roots must be emitted as `dead_code_root_kinds` metadata, not query
source fallbacks. Current roots cover `@main` types, `main`, SwiftUI `App` types
and `body`, protocol methods and same-file implementations, constructors on
concrete types, overrides, UIKit application delegate callbacks, Vapor route
handlers, XCTest methods, and Swift Testing `@Test` functions. Helpers should
stay package-local unless another language-owned package has a real caller.

No-Regression Evidence (issue #3589, Swift AST migration): the parity golden in
`testdata/parity/` was captured from the prior regex extractor and asserts
byte-identical output for every non-call bucket. The `function_calls` bucket
carries documented bug-fix deviations (see `AGENTS.md`) covered by
`deviation_test.go`. No-Observability-Change: this parser-local extraction change
adds no metric, span, log, status field, queue behavior, graph query,
environment variable, or runtime knob.

## Related docs

- `docs/public/languages/support-maturity.md`
