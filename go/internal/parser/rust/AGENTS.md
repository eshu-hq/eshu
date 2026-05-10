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
  module declarations, direct derives, conditional derives, attributes, generic
  parameter names, where predicates, associated-type constraints, and
  higher-ranked trait-bound predicates are structured metadata when the syntax
  is direct. Arbitrary macro expansion and filesystem-backed module resolution
  remain limits unless a test and downstream contract prove the new claim.
- Keep attribute capture item-local. Function, type, module, alias, variable,
  and macro metadata may use direct item attributes or immediately adjacent
  leading attributes. Field-level and enum-variant attributes must stay on owned
  `annotations` rows instead of promoting to the parent type.
- Keep impl target metadata bounded to the receiver type. Multiline and
  same-line `where` clauses are evidence for future bounds modeling, not part
  of `target`.
- Keep dead-code root metadata conservative. `rust.main_function`,
  `rust.test_function`, `rust.tokio_main`, `rust.tokio_test`,
  `rust.public_api_item`, and `rust.benchmark_function` require direct
  function-name, exact `pub`, Cargo path, file-local Criterion macro target, or
  immediately preceding / same-line attribute evidence.
- Keep module declaration path metadata file-local. `declared_path_candidates`
  names candidate files relative to the current file directory, except explicit
  `#[path = "..."]` attributes, which replace the candidates with that declared
  path. Literal macro-body `mod` and `use` declarations may be modeled, but
  parser code must not run macro expansion or probe the filesystem.
- Keep exactness blocker metadata honest. `cfg` and `cfg_attr` item attributes
  name `cfg_unresolved`, and macro-origin module/import rows name
  `macro_expansion_unavailable`; do not clear those blockers without a
  downstream resolver and tests.

## Anti-Patterns

- Importing the parent parser package.
- Moving registry or engine dispatch into this package.
- Changing payload keys without updating downstream parser tests.
