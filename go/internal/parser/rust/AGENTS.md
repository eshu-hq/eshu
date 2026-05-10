# AGENTS.md - internal/parser/rust guidance

## Read First

1. README.md - package boundary and parser responsibilities
2. doc.go - godoc contract
3. parser.go - Rust payload extraction
4. helpers.go - lifetime and call helper functions

## Invariants This Package Enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- The caller owns tree-sitter parser construction and closing.
- Parse and PreScan must preserve the parent engine payload shape.

## Common Changes And Scope

- Add Rust parser behavior by starting with focused parser tests in the parent
  parser package or this package.
- Keep registry dispatch and runtime parser lookup in the parent parser package.
- Keep shared cross-language primitives in internal/parser/shared.
- Preserve current limits in README.md when adding coverage. Brace imports,
  generic type parameters, derives, modules, and attribute macros beyond the
  documented Rust root cases are not structured metadata unless a test and
  downstream contract prove the new claim.
- Keep dead-code root metadata conservative. `rust.main_function`,
  `rust.test_function`, `rust.tokio_main`, and `rust.tokio_test` require direct
  function-name or immediately preceding attribute evidence.

## Anti-Patterns

- Importing the parent parser package.
- Moving registry or engine dispatch into this package.
- Changing payload keys without updating downstream parser tests.
