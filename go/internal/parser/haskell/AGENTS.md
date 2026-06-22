# AGENTS.md - internal/parser/haskell guidance

## Read first

1. README.md - package boundary and where-block invariant
2. doc.go - godoc contract for the Haskell adapter
3. parser.go - parse orchestration, kept regex exceptions, and pre-scan
4. tree_sitter_symbols.go - tree-sitter symbol collectors (module, types,
   methods, value bindings)
5. tree_sitter_buckets.go - payload bucket builders, where-variable scan, and
   call-evidence span scan
6. tree_sitter_syntax.go - tree-sitter parse setup and parameter/scope helpers
7. parser_test.go - behavior coverage for payload shape
8. parity_test.go / parity_golden_test.go - byte-parity payload snapshot

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves modules as their own bucket and data/class declarations as
  class rows.
- Primary symbol extraction is a tree-sitter grammar walk. Do not reintroduce
  regex/line-scan symbol extraction (epic #3531).
- Function-call rows are bounded lexical evidence from definition right-hand
  sides, not compiler-resolved Haskell name binding.
- Caller-owned parser entrypoints must keep parser ownership with the caller
  and must not close parsers they did not create.
- Dead-code root metadata is limited to explicit module exports, `main`,
  typeclass methods, and instance methods. Do not mark every top-level
  declaration in implicit-export modules as public API.
- PreScan derives names from Parse so parent pre-scan and full parse agree.

## Common changes and how to scope them

- Add Haskell evidence by writing a focused test in parser_test.go first.
- Keep indentation-sensitive where-block behavior covered when changing
  variable extraction.
- Use internal/parser/shared helpers for payload buckets and sorting.

## Failure modes and how to debug

- Missing where-block variables usually mean indentation handling changed.
- Duplicate function rows usually mean seen-function tracking changed.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Treating every lower-case Haskell line as a variable outside a where block.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Haskell extension ownership or registry behavior from this
  package.

## Line-scan-to-AST migration (epic #3531)

Primary symbol extraction moved off the per-line `FindStringSubmatch` loop onto
the tree-sitter Haskell grammar walk in `tree_sitter_symbols.go`. The probed
grammar node kinds and fields drive each bucket:

- Module: the `header` node's `module` field supplies the dotted name; the
  header node line span supplies `line_number`/`end_line`; the `exports` field's
  `export` children supply explicit-export names (deleted
  `haskellModulePattern`, `haskellCollectModuleHeader`,
  `haskellParseModuleExports`).
- Classes bucket: `data_type`, `newtype`, `type_synomym` (grammar spelling),
  and `data_family` nodes supply data/newtype/type rows by their `name` field;
  `class` nodes supply typeclass rows. `data_family` reports `semantic_kind`
  `data`, matching the prior `(data|newtype|type)\s+(?:family\s+)?` capture
  (deleted `haskellTypeDeclarationRegex`, `haskellClassPattern`).
- Instance context: the `instance` node's `name` field plus its `patterns`
  field (kind `type_patterns`) render the space-joined head such as
  `Runner Worker` (deleted `haskellInstancePattern`).
- Class methods: each `class_declarations` `signature` node's `name` field is a
  typeclass method, captured regardless of line wrapping (deleted
  `haskellTypeSignaturePattern`).
- Functions: `function` and `bind` nodes in declaration scope, plus
  `instance_declarations` and `class_declarations` method bindings, supply
  functions-bucket rows; parameters come from the `patterns` field via
  `haskellTreeFunctionParameters` (deleted `haskellFunctionPattern`,
  `haskellFunctionParameters`, `haskellAppendFunctionCalls`, and the old
  `haskellFunctionSpan`/`collect`/`applyHaskellTreeFunctionMetadata`/
  `appendHaskellTreeFunctionCalls`/`haskellFunctionItem` augmentation path). A
  typeclass default-method body (`class_declarations` `function`/`bind`) keeps
  the method as a typeclass method even with no signature and contributes its
  right-hand-side call evidence.
- Call evidence: `haskellAppendRHSCalls` scans each value/method/default-body
  span line by line and slices after the first `=`, so the bound name on a
  definition or local `where`/`let` binding line is never reported as a call.
  Bare continuation lines (no `=`) are scanned whole so multi-line applications
  stay covered.

Justified permanent exceptions (bounded textual evidence, not symbol
extraction): `haskellVariablePattern` records simple where-block local bindings
as variables and intentionally demotes them out of the functions bucket
(keeping only bare `name =`/`name` forms over an indentation scan);
`haskellCallTokenPattern` is the lexical call-evidence token scan over
definition right-hand sides. The import-line reader (`haskellParseImport`) is
bounded evidence that normalizes safe/qualified/package-qualified imports and
resolves `as` aliases, not an AST symbol walk. These three remain regex/string
readers; everything else is AST.

Deviations (AST fixes a regex bug):

1. A class method whose type signature wraps across lines now produces a
   functions row. The deleted `haskellTypeSignaturePattern` required the method
   name and `::` on one line, so wrapped signatures produced no method.
   `TestParseCapturesMultilineClassMethodSignature` fails on the prior regex and
   passes on the AST.
2. The bound name on a definition or local `where`/`let` binding line is no
   longer emitted as a call. The prior multi-pass extractor inconsistently
   scanned binding LHS names (and bare guard keywords left of `=`) as calls for
   some span kinds. `haskellAppendRHSCalls` now restores the documented
   right-hand-side-only contract uniformly. The full-payload snapshot golden was
   re-baselined to drop exactly two such spurious rows: `run` -> `helper` at line
   28 (a `where` binder LHS) and `caller` -> `otherwise` at line 34 (a guard
   keyword left of `=`). `TestParseExcludesWhereBindingNameFromCalls` covers the
   binder-LHS case.
3. Typeclass default-method bodies are scanned for call evidence (and a
   default-only method with no signature is kept as a typeclass method). The
   migrated symbol walk had skipped class default bodies entirely.
   `TestParseCapturesClassDefaultMethodCalls` covers this.

Tests 2 and 3 fail on the first migration commit and pass after the call-evidence
fix. Every other bucket stays byte-for-byte identical, pinned by
`TestHaskellPayloadParitySnapshot`.

No-Regression Evidence: `go test ./internal/parser/haskell -count=1` (12 tests)
and `go test ./internal/parser -count=1` (665 tests) stay green, including
`TestHaskellPayloadParitySnapshot` (re-baselined call-evidence payload) and
`TestDefaultEngineParsePathHaskellFixtures`. `go test ./internal/parser/...
./internal/collector/discovery ./internal/content/shape ./internal/collector
-count=1` passes; `golangci-lint run ./internal/parser/...` reports 0 issues;
`gofmt -l internal/parser/haskell` is empty; `git diff --check` is clean.
`rg -n 'regexp\.' internal/parser/haskell/` shows only the two justified
patterns above.

No-Observability-Change: this change is parser-internal; it changes only how the
existing `modules`, `classes`, `functions`, `function_calls`, `variables`, and
`imports` payload buckets are computed. No metric instrument, metric label,
span, log line, status field, env var, queue, worker, lease, batch, runtime
knob, or graph query is added or changed. Operators still diagnose parser
behavior through existing collector parse-stage logs and
`eshu_dp_file_parse_duration_seconds`.
