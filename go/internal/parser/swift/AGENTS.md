# Swift Parser Agent Notes

Read `language.go` then `ast_extract.go` first. `Parse` walks the tree-sitter
AST once and emits every payload bucket from node ranges; there is no line-scan
path. Keep production files parent-independent: use `internal/parser/shared`
for payload, source, sorting, and node helpers. Do not import `internal/parser`
from `language.go`, `ast_extract.go`, `ast_calls.go`, `ast_nodes.go`,
`tree_sitter_syntax.go`, or `helpers.go`.

The external black-box tests (`engine_swift_*_test.go`, `swift_*_test.go`,
package `swift_test`) are the one documented exception: they import
`internal/parser` to drive `parser.DefaultEngine().ParsePath`. Go compiles
`swift_test` as a package separate from `swift`, so this does not create an
import cycle or reverse the production dependency from the parent engine to
this adapter.

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
- `engine_swift_ast_migration_test.go`, `engine_swift_extension_test.go`,
  `engine_swift_semantics_test.go`, `engine_swift_symbol_gate_test.go`,
  `engine_swift_vapor_routes_test.go`, `swift_dead_code_roots_test.go`,
  `swift_vapor_golden_fixture_test.go` —
  package `swift_test` Engine-level black-box coverage relocated from
  `go/internal/parser` by #6062. `engine_swift_test_helpers_test.go` carries
  the assertion helpers these files need that `internal/parser/parsertest`
  does not already provide.

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
method, path, and handler are exact from syntax. Literal route groups are exact
only when the parent receiver is already proven, the group prefix is literal,
and the closure parameter is a simple identifier. Do not migrate this evidence
to a symbol row or source-text scan.

Dead-code reachability hints belong in parser metadata as
`dead_code_root_kinds`. Keep Swift root modeling bounded to syntax and
same-file evidence that this package can prove without importing the parent
parser.

No-Regression Evidence: `go test ./internal/parser/... -count=1` passes (1269
tests), including `TestSwiftComprehensiveSymbolExtractionGate` and the
`TestDefaultEngineParsePathSwift*` parity suite. No-Observability-Change: this
parser-local change adds no metric, span, log, status field, queue behavior,
graph query, environment variable, or runtime knob.

`dogfood_real_repo_test.go` is a standing regression test (#5399) backing the
`real-repo-validated` grade; it is not opt-in like the `SWIFT_PARSE_DUMP`
equivalence harness. Do not hand-edit
`testdata/dogfood_real_repo_snapshot.txt`; regenerate it with
`DOGFOOD_UPDATE_SNAPSHOT=1 bash scripts/dogfood-swift.sh` after an intended
parser change and verify the bucket-count delta is expected.
