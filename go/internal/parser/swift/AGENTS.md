# Swift Parser Agent Notes

## Read First

Read `language.go` first (the thin entry point and bucket assembly), then
`ast_walk.go` (the recursive emit walk and `swiftPayloadBuilder` state) and
`tree_sitter_syntax.go` (the first-pass fact collection). Read `ast_imports.go`,
`ast_types.go`, `ast_functions.go`, `ast_variables.go`, and `ast_calls.go` for
the per-node extraction, then `dead_code_roots.go` and `helpers.go` for the pure
helpers. Keep changes scoped to Swift unless the caller explicitly asks for a
cross-language parser contract change.

## Invariants

Do not import the parent `internal/parser` package. Use
`go/internal/parser/shared` for `shared.Options`, source reads, base payload
construction, bucket appends, sorting, and pre-scan name cleanup.

Extraction is AST-only. Do not reintroduce `regexp` or `strings.Split(src,
"\n")` line-scan symbol extraction. Confirm grammar node kinds with a compiled
grammar probe or the Go test harness, not filtered text search. The vendored
grammar is
`github.com/indigo-net/Brf.it/pkg/parser/treesitter/grammars/swift`.

`Parse` preserves the parent engine behavior and payload shape: the
`map[string]any` keys and value shapes are the contract proven by the
`engine_swift_*` tests, `swift_dead_code_roots_test.go`,
`engine_kotlin_swift_symbol_gate_test.go`, the `swift_comprehensive` golden
fixtures, and the package-local parity golden in `testdata/parity/`. `PreScan`
keeps deriving names from the same `Parse` path so collection pre-scan and full
parsing agree.

## Common Changes

Import, type, function, variable, and call extraction belongs in the `ast_*.go`
file keyed by node kind. Whole-file facts (conformances, protocol methods, Vapor
route handlers) belong in `tree_sitter_syntax.go`. Dead-code root classification
belongs in `dead_code_roots.go`. Scope tracking flows through the recursion
`scope []swiftScope`, not a brace-depth stack; functions do not push a type
scope, so members inside function bodies still resolve to the enclosing type.

## Failure Modes

A new grammar construct the walker does not handle silently drops a row; add a
focused test and probe the AST before editing. Scope bugs usually change
`class_context`, `context`, `inferred_obj_type`, or `dead_code_root_kinds`.
Visiting nodes out of source order breaks first-occurrence variable dedup.

## Anti-Patterns

Do not add parent-package imports, regex or line-scan extraction,
whole-repository scans, or Swift fixes in other language packages. Do not change
payload keys without focused Swift tests and downstream parser contract
validation.

## Migration: regex retired in favor of AST (issue #3589, epic #3531)

Migrated (line-scan regex deleted, behavior now from the tree-sitter node walk):

- `importPattern` to `import_declaration` > `identifier` text.
- `classPattern`, `actorPattern`, `structPattern`, `enumPattern`,
  `protocolPattern`, `extensionPattern` to `class_declaration` /
  `protocol_declaration` with keyword disambiguation and `swiftTreeInheritance`.
- `functionPattern` plus the `init(` text checks to `function_declaration`,
  `protocol_function_declaration`, and `init_declaration` nodes with `parameter`
  argument names.
- `variablePattern` to `property_declaration` and `protocol_property_declaration`
  nodes with `type_annotation` types.
- `receiverCallPattern` and `callPattern` to `call_expression` nodes.
- `vaporRoutePattern` to the `use:` labeled `value_argument` node (label
  `value_argument_label` == "use", value `simple_identifier`).

Justified permanent exceptions: none. All Swift symbol and call extraction is
AST-driven; the package has no `regexp` import.

### Byte-parity deviations (documented bug fixes, covered by `deviation_test.go`)

The non-call payload buckets are byte-identical to the prior extractor (locked by
`testdata/parity/`). The `function_calls` bucket carries three classes of
intentional bug fix, each asserted by a failing-first test:

1. Declaration headers no longer leak call rows. The old plain-call regex skipped
   only `func `/`init(` prefixes, so `override func`, `mutating func`, and
   `private func` lines produced phantom calls (e.g. `start`, `helper`,
   `translate`, `unusedCleanupCandidate`). Enum cases (`serverError`,
   `invalidInput`) and the `private(set)` access modifier likewise produced
   phantom calls. The AST walk emits only real `call_expression` nodes.
2. Nested call arguments are read from the argument node, not by slicing to the
   last `)`. `transform(value)` inside an outer call now yields args `["value"]`
   instead of `["value)"]`.
3. Real calls the regex missed are now captured: trailing-closure calls without
   parentheses (`WindowGroup { ... }`) and calls inside extension method bodies
   (`log(...)`, `store[...]`). Genuine `super.method(...)` and `self.method(...)`
   receiver calls are preserved with their receiver-prefixed `full_name`.

A declaration also reports its keyword line, not a leading attribute line, so
`@main`/`@Test` placed above a declaration no longer shifts `line_number` or the
stored function `source` header.

## No-Regression Evidence

Commands run (raw output via `rtk proxy`):

- `go test ./internal/parser/swift -count=1` — ok, 9 package tests pass (8
  call/line deviation tests plus the parity golden across 11 sources).
- `go test ./internal/parser -run Swift -count=1` — ok, 10 Swift parent-engine
  tests pass (bases, args, variable metadata, imports/calls, receiver inference,
  multiline scope, extension context, dead-code roots, symbol gate).
- `go test ./internal/parser/... -count=1` — ok, all language parser packages
  pass.
- `go test ./internal/parser ./internal/parser/goldenaudit ./internal/reducer
  -run Swift -count=1` — ok.
- `gofmt -l internal/parser/swift` — empty.
- `golangci-lint run ./internal/parser/...` — 0 issues.

No-Observability-Change: the migration is parser-local. It adds no metric
instrument, metric label, span, log line, status field, env var, queue, worker,
lease, batch, runtime knob, or graph query. Operators still diagnose Swift
parsing through the collector parse-stage logs and the file parse-duration
metric.
