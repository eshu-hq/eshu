# AGENTS.md - internal/parser/haskell guidance

## Read first

1. README.md - package boundary and where-block invariant
2. doc.go - godoc contract for the Haskell adapter
3. parser.go - regex parser and pre-scan behavior
4. parser_test.go - behavior coverage for payload shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- Parse preserves modules as their own bucket and data/class declarations as
  class rows.
- Function-call rows are bounded lexical evidence from definition right-hand
  sides, not compiler-resolved Haskell name binding.
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
