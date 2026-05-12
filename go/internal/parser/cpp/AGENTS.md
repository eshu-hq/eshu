# AGENTS.md - internal/parser/cpp guidance

## Read First

1. README.md - package boundary and parser responsibilities
2. doc.go - godoc contract
3. parser.go - C++ payload extraction
4. dead_code_roots.go - C++ parser-backed dead-code root metadata
5. header_roots.go - bounded direct local-header public API roots
6. helpers.go - local helper functions copied out of the parent package

## Invariants This Package Enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- The caller owns tree-sitter parser construction and closing.
- Parse and PreScan must preserve the parent engine payload shape.
- Dead-code roots are parser evidence only. Keep `cpp.public_header_api`
  bounded to headers directly included by the current file and inside the
  repository root.

## Common Changes And Scope

- Add C++ parser behavior by starting with focused parser tests in the parent
  parser package or this package.
- Add dead-code root behavior by proving the parser metadata first, then the
  query suppression and maturity response.
- Keep registry dispatch and runtime parser lookup in the parent parser package.
- Keep shared cross-language primitives in internal/parser/shared.

## Anti-Patterns

- Importing the parent parser package.
- Moving registry or engine dispatch into this package.
- Changing payload keys without updating downstream parser tests.
- Following transitive include graphs, expanding macros, or resolving build
  targets inside this parser package without an ADR-backed design.
