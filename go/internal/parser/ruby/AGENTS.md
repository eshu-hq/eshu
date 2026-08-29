# AGENTS.md - internal/parser/ruby guidance

## Read first

1. README.md - package boundary and Ruby context invariants
2. doc.go - godoc contract for the Ruby adapter
3. parser.go - Parse/PreScan entry points, payload assembly, and call drain
4. syntax.go - AST walk producing scopes, declarations, variables, imports,
   inclusions, and method-call rows
5. nodes.go - tree-sitter node accessors (text, constant, superclass, parameter,
   and typed argument lookups) used by the AST walk
6. calls.go - AST call-name composition and argument/assignment-type helpers
7. dead_code_roots.go - Ruby parser-backed dead-code root metadata from the AST
8. bundler_blocks.go - opaque-block helper retained for the Bundler scanner
9. parser_test.go - same-package behavior coverage for payload shape
10. engine_ruby_semantics_test.go, ruby_dead_code_roots_test.go,
    ruby_route_entries_test.go, engine_bundler_lockfile_test.go - external
    `package ruby_test` coverage of the public Engine contract, with local
    helpers in ruby_engine_helpers_test.go

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but `package ruby` must not import internal/parser. The external
  `package ruby_test` files in this directory do import it, on purpose: Go
  compiles an external test package separately, so black-box Engine tests can
  sit next to the adapter they cover without an import cycle.
- Parse preserves the Ruby payload shape, including modules, module_inclusions,
  framework_semantics, and context metadata. The tree-sitter rewrite must keep
  parity with the prior output for every bucket.
- All Ruby source evidence comes from the AST: modules, classes, singleton
  classes, methods, imports, inclusions, variables, end lines, and method calls.
  No Ruby source bucket may be recovered by scanning source text. Only the
  Bundler manifest path (bundler_*.go) parses lines, because Gemfile and
  Gemfile.lock are not Ruby-grammar inputs.
- Function and class `end_line` metadata comes from AST node end positions
  because reducer call materialization depends on method containment for
  receiverless helper calls.
- PreScan derives names from Parse so parent pre-scan and full parse agree.
  ParseWithParser/PreScanWithParser accept a caller-owned tree-sitter parser.
- Dead-code roots are parser evidence only. Rails controller action and callback
  roots must stay bounded to literal class names, visibility statements, and
  symbol arguments the AST has actually seen.

## Common changes and how to scope them

- Add Ruby evidence by writing a focused test first: parser_test.go for adapter
  internals, or the matching `package ruby_test` file when the claim is about
  what `parser.DefaultEngine().ParsePath` emits.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.
- Use internal/parser/shared helpers for payload buckets and sorting.
- Keep constants in the legacy `variables` bucket unless a downstream shape
  change explicitly introduces a constants bucket. Keep unmodeled Rails/Rake DSL
  calls as call evidence until a focused root model and dogfood proof exists.

## Failure modes and how to debug

- Missing context metadata usually means the AST scope index in syntax.go did
  not record or resolve the enclosing module, class, or method scope.
- Missing call rows usually mean the AST walk in syntax.go did not visit the
  `call` node (for example a nested receiver), or the full-name composition in
  calls.go dropped a receiver or method segment.
- Missing Ruby root metadata usually means `dead_code_roots.go` did not see a
  literal callback symbol, script guard call, or class visibility transition.

## Anti-patterns specific to this package

- Importing the parent parser package from `package ruby` (the external
  `package ruby_test` files are the sanctioned exception).
- Treating Ruby blocks as fully parsed syntax without fixture proof.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Ruby extension ownership or registry behavior from this
  package.
