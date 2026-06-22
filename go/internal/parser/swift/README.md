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

Tree-sitter supplies declaration spans, inheritance clauses, function spans,
parameters, initializer ownership, and protocol methods. Existing bounded regex
helpers still own imports, attributes, variables, calls, and dead-code root
classification. Keep output deterministic because pre-scan output feeds
repository import maps.

`extension Foo { ... }` declares no new type, so it emits no type entity. The
grammar models it as a `class_declaration` whose extended type is a `user_type`
child rather than a `type_identifier` name field, so both the line scan
(`extensionPattern`) and the syntax index (`swiftExtensionTypeName`) resolve the
extended type name and push an `extension` scope. Members declared inside the
extension are attributed to the extended type via `class_context`.

Dead-code roots must be emitted as
`dead_code_root_kinds` metadata, not query source fallbacks. Current roots cover
`@main` types, `main`, SwiftUI `App` types and `body`, protocol methods and
same-file implementations, constructors on concrete types, overrides, UIKit
application delegate callbacks, Vapor route handlers, XCTest methods, and Swift
Testing `@Test` functions. Helpers should stay package-local unless another
language-owned package has a real caller.

No-Regression Evidence: multiline Swift declarations are indexed through the
shared tree-sitter runtime before line-level semantic inference, preserving
existing buckets while retaining bases, class context, args, end lines,
constructor roots, protocol roots, and pre-scan names. No-Observability-Change:
this parser-local extraction change adds no metric, span, log, status field,
queue behavior, graph query, environment variable, or runtime knob.

## Related docs

- `docs/public/languages/support-maturity.md`
