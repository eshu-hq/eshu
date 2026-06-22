# AGENTS.md - internal/parser/ruby guidance

## Read first

1. README.md - package boundary and Ruby context invariants
2. doc.go - godoc contract for the Ruby adapter
3. parser.go - Parse/PreScan entry points, payload assembly, and call pass
4. syntax.go - AST walk producing scopes, declarations, variables, imports,
   and inclusions
5. calls.go - per-line method-call recognizers and argument normalization
6. scan.go - byte-level scanners that replace the legacy call regexes
7. dead_code_roots.go - Ruby parser-backed dead-code root metadata from the AST
8. bundler_blocks.go - opaque-block helper retained for the Bundler scanner
9. parser_test.go - behavior coverage for payload shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves the byte-identical Ruby payload shape, including modules,
  module_inclusions, framework_semantics, and context metadata. The tree-sitter
  rewrite must keep parity with the prior regex output for every bucket.
- Source structure (modules, classes, singleton classes, methods, imports,
  inclusions, variables, end lines) comes from the AST. Method-call evidence
  comes from the byte-level line scan in calls.go/scan.go, never regex.
- Function and class `end_line` metadata comes from AST node end positions
  because reducer call materialization depends on method containment for
  receiverless helper calls.
- PreScan derives names from Parse so parent pre-scan and full parse agree.
  ParseWithParser/PreScanWithParser accept a caller-owned tree-sitter parser.
- Dead-code roots are parser evidence only. Rails controller action and callback
  roots must stay bounded to literal class names, visibility statements, and
  symbol arguments the AST has actually seen.

## Common changes and how to scope them

- Add Ruby evidence by writing a focused test in parser_test.go first.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.
- Use internal/parser/shared helpers for payload buckets and sorting.
- Keep constants in the legacy `variables` bucket unless a downstream shape
  change explicitly introduces a constants bucket. Keep unmodeled Rails/Rake DSL
  calls as call evidence until a focused root model and dogfood proof exists.

## Failure modes and how to debug

- Missing context metadata usually means the AST scope index in syntax.go did
  not record or resolve the enclosing module, class, or method scope.
- Missing call rows usually mean a recognizer in calls.go/scan.go stopped
  matching a DSL or chained-call shape, or the structural-line skip dropped a
  line the line scan should have read.
- Missing Ruby root metadata usually means `dead_code_roots.go` did not see a
  literal callback symbol, script guard call, or class visibility transition.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Treating Ruby blocks as fully parsed syntax without fixture proof.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Ruby extension ownership or registry behavior from this
  package.
