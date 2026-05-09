# AGENTS.md - internal/parser/ruby guidance

## Read first

1. README.md - package boundary and Ruby context invariants
2. doc.go - godoc contract for the Ruby adapter
3. parser.go - declaration, variable, import, and payload behavior
4. calls.go - method-call and argument normalization helpers
5. parser_test.go - behavior coverage for payload shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves the legacy Ruby payload shape, including modules,
  module_inclusions, framework_semantics, and context metadata.
- PreScan derives names from Parse so parent pre-scan and full parse agree.

## Common changes and how to scope them

- Add Ruby evidence by writing a focused test in parser_test.go first.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.
- Use internal/parser/shared helpers for payload buckets and sorting.

## Failure modes and how to debug

- Missing context metadata usually means block push/pop behavior changed.
- Missing call rows usually mean calls.go filtered a DSL or chained-call shape
  that parent parser tests rely on.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Treating Ruby blocks as fully parsed syntax without fixture proof.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Ruby extension ownership or registry behavior from this
  package.
