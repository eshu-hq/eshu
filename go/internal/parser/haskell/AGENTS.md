# AGENTS.md - internal/parser/haskell guidance

## Read first

1. README.md - package boundary, AST invariants, and the where-block invariant
2. doc.go - godoc contract for the Haskell adapter
3. ast_extract.go - the AST walk that emits every symbol bucket
4. ast_nodes.go - node accessors (module name, exports, imports, declarations)
5. helpers.go - dead-code roots, function keys, and bounded call evidence
6. parser.go - Parse, ParseWithParser, PreScan entrypoints
7. parser_test.go and ast_extract_test.go - behavior coverage for payload shape

## Invariants this package enforces

- All Haskell source-symbol extraction is tree-sitter AST based. Do not
  reintroduce regex or line-scan extraction of modules, functions, imports,
  data/class/instance declarations, type signatures, or variables; key every row
  by AST node spans.
- Function-call rows are bounded lexical evidence from definition right-hand
  sides, not compiler-resolved Haskell name binding. This is the only documented
  permanent line-text exception, alongside the function `source` span.
- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves modules as their own bucket and data/newtype/type/class
  declarations as class rows with `semantic_kind`.
- Caller-owned parser entrypoints must keep parser ownership with the caller and
  must not close parsers they did not create.
- Dead-code root metadata is limited to explicit module exports, `main`,
  typeclass methods, and instance methods. Do not mark every top-level
  declaration in implicit-export modules as public API.
- The function `source` field covers the defining clauses and guards but stops
  before a trailing `where` block.
- PreScan derives names from Parse so parent pre-scan and full parse agree.

## Common changes and how to scope them

- Add Haskell evidence by writing a focused test in parser_test.go or
  ast_extract_test.go first, and extend the AST walk rather than adding a regex.
- Keep the `testdata/characterization` goldens current: they are the byte-parity
  gate. Regenerate intentionally with `ESHU_UPDATE_GOLDEN=1` and review the diff.
- Keep where-block variable behavior covered when changing the `binds` walk.
- Use internal/parser/shared helpers for payload buckets and sorting.

## Failure modes and how to debug

- Missing where-block variables usually mean the `binds` field walk changed.
- Duplicate function rows usually mean seen-function tracking changed.
- A wrong field name on a grammar node shows up as an empty name; dump node
  kinds and fields with a throwaway test before assuming the grammar shape.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Reintroducing line-scan or regex symbol extraction.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Haskell extension ownership or registry behavior from this
  package.

## No-regression and observability evidence

No-Regression Evidence (issue #3588, Haskell AST migration): `go test
./internal/parser/haskell -count=1` (byte-parity characterization goldens plus
behavior tests, including the failing-first
`TestParseCapturesMultiLineTypeSignatureClassMethod`) and the downstream gates
`go test ./internal/parser/... ./internal/reducer ./internal/query -count=1`.
The AST walk replaces a per-line regex scan with one recursive parse-tree walk,
bounded by AST node count; no goroutine, channel, lock, queue, or graph-write
behavior is added.

No-Observability-Change (issue #3588): the migration is parser-local. It adds no
metric, span, log, or status; operator-facing signals are identical before and
after.
