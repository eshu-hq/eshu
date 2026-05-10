# AGENTS.md - internal/parser/rust guidance

## Read First

1. README.md - package boundary and parser responsibilities
2. doc.go - godoc contract
3. parser.go - Rust payload extraction
4. helpers.go - tree-sitter and syntax helper functions
5. metadata.go - attribute, import, module, and generic metadata helpers

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
  module declarations, direct derives, attributes, and generic parameter names
  are structured metadata. Macro-expanded modules/imports, conditional derives,
  field-level attributes, enum-variant attributes, full `where` bound semantics,
  associated type constraints, and higher-ranked trait bounds remain limits
  unless a test and downstream contract prove the new claim.
- Keep attribute capture item-local. Function, type, module, alias, variable,
  and macro metadata may use direct item attributes or immediately adjacent
  leading attributes, but field-level and enum-variant attributes must stay on
  their own syntax nodes until a downstream contract needs them.
- Keep impl target metadata bounded to the receiver type. Multiline and
  same-line `where` clauses are evidence for future bounds modeling, not part
  of `target`.
- Keep dead-code root metadata conservative. `rust.main_function`,
  `rust.test_function`, `rust.tokio_main`, and `rust.tokio_test` require direct
  function-name, Cargo path, immediately preceding attribute, or same-line
  attribute evidence.

## Anti-Patterns

- Importing the parent parser package.
- Moving registry or engine dispatch into this package.
- Changing payload keys without updating downstream parser tests.
