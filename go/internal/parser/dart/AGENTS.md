# AGENTS.md - internal/parser/dart guidance

## Read first

1. README.md - package boundary and payload buckets
2. doc.go - godoc contract for the Dart adapter
3. parser.go - regex parser and pre-scan behavior
4. parser_test.go - behavior coverage for payload shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves the legacy Dart payload shape and deterministic bucket order.
- `dead_code_root_kinds` must stay syntax-local: top-level `main`,
  constructors, `@override`, Flutter `build`/`createState`, and public `lib/`
  declarations outside `lib/src/`.
- PreScan derives names from Parse so the parent engine sees the same language
  evidence in both phases.

## Common changes and how to scope them

- Add Dart evidence by writing a focused test in parser_test.go first.
- Keep file reading in this package through internal/parser/shared.ReadSource.
- Keep registry, Engine dispatch, and content-shape changes outside this
  package unless the task explicitly includes those files.

## Failure modes and how to debug

- Missing declarations or root metadata usually mean a regex stopped matching
  the line-oriented fixture shape.
- Duplicate or unstable call rows usually mean seen-call tracking or bucket
  sorting changed.

## Anti-patterns specific to this package

- Importing the parent parser package.
- Adding tree-sitter state for this simple adapter without a behavior need.
- Emitting new bucket keys without matching downstream shape work.

## What NOT to change without an ADR

- Do not change Dart extension ownership or registry behavior from this package.
