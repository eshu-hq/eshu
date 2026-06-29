# Swift Parser Agent Notes

Read `language.go` then `ast_extract.go` first. `Parse` walks the tree-sitter
AST once and emits every payload bucket from node ranges; there is no line-scan
path. Keep this package parent-independent: use `internal/parser/shared` for
payload, source, sorting, and node helpers. Do not import `internal/parser`.

File layout:

- `language.go` — `Parse`/`PreScan` entrypoints and bucket sort order.
- `ast_extract.go` — the `swiftExtractor` walk: imports, types, functions,
  variables.
- `ast_calls.go` — `call_expression` extraction and receiver/method resolution.
- `ast_nodes.go` — AST node helpers (children, declaration keyword, parameters,
  property names, type annotations).
- `tree_sitter_syntax.go` — parse helper plus the AST-built semantic facts
  (conformances, protocol methods, Vapor route handlers, exact Vapor route
  entries) and extension naming.
- `helpers.go` — pure dead-code root classification and short-name helpers.

Preserve existing payload keys and sorting unless a parser contract change is
covered by tests and downstream materialization updates.

Migration status (#3589, epic #3531): primary symbol extraction is fully on the
tree-sitter AST. Only genuine `call_expression` nodes yield `function_calls`
rows. The migration intentionally drops line-scan false positives (enum case
declarations, `mutating`/`override` declaration lines, `private(set)` modifiers,
string interpolation) and adds real subscript/initializer/chained calls the
scanner missed; `engine_swift_ast_migration_test.go` documents this deviation
and red-proves it against pre-migration `main`.

Permanent exception: the Vapor `use:` route hint has no symbol-node form. It is
read as framework evidence from the `value_argument_label` `use`
(`collectSwiftVaporRoutes`) to feed the `swift.vapor_route_handler` dead-code
root. The same AST-backed pass may emit `framework_semantics.vapor.route_entries`
only when the receiver is typed `Application` or `RoutesBuilder` and the route
method, path, and handler are exact from syntax. Do not migrate this evidence to
a symbol row or source-text scan.

Dead-code reachability hints belong in parser metadata as
`dead_code_root_kinds`. Keep Swift root modeling bounded to syntax and
same-file evidence that this package can prove without importing the parent
parser.

No-Regression Evidence: `go test ./internal/parser/... -count=1` passes (1269
tests), including `TestSwiftComprehensiveSymbolExtractionGate` and the
`TestDefaultEngineParsePathSwift*` parity suite. No-Observability-Change: this
parser-local change adds no metric, span, log, status field, queue behavior,
graph query, environment variable, or runtime knob.
