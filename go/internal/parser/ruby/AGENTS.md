# AGENTS.md - internal/parser/ruby guidance

## Read first

1. README.md - package boundary and Ruby context invariants
2. doc.go - godoc contract for the Ruby adapter
3. parser.go - declaration, variable, import, and payload behavior
4. calls.go - method-call and argument normalization helpers
5. dead_code_roots.go - Ruby parser-backed dead-code root metadata
6. parser_test.go - behavior coverage for payload shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves the legacy Ruby payload shape, including modules,
  module_inclusions, framework_semantics, and context metadata.
- Ruby `end` handling must keep function and class `end_line` metadata current
  because reducer call materialization depends on method containment for
  receiverless helper calls.
- PreScan derives names from Parse so parent pre-scan and full parse agree.
- Dead-code roots are parser evidence only. Rails controller action and callback
  roots must stay bounded to literal class names, visibility lines, and symbol
  arguments the line parser has actually seen.

## Common changes and how to scope them

- Add Ruby evidence by writing a focused test in parser_test.go first.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.
- Use internal/parser/shared helpers for payload buckets and sorting.
- Keep constants in the legacy `variables` bucket unless a downstream shape
  change explicitly introduces a constants bucket. Keep unmodeled Rails/Rake DSL
  calls as call evidence until a focused root model and dogfood proof exists.

## Failure modes and how to debug

- Missing context metadata usually means block push/pop behavior changed.
- Missing call rows usually mean calls.go filtered a DSL or chained-call shape
  that parent parser tests rely on.
- Missing Ruby root metadata usually means `dead_code_roots.go` did not see a
  literal callback symbol, script guard call, or class visibility transition.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Treating Ruby blocks as fully parsed syntax without fixture proof.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Ruby extension ownership or registry behavior from this
  package.
