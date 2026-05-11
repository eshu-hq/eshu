# AGENTS.md - internal/parser/c guidance

## Read First

1. README.md - package boundary and parser responsibilities
2. doc.go - godoc contract
3. parser.go - C payload extraction
4. dead_code_roots.go - C root metadata for dead-code reachability
5. helpers.go - local helper functions copied out of the parent package

## Invariants This Package Enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- The caller owns tree-sitter parser construction and closing.
- Parse and PreScan must preserve the parent engine payload shape.
- Dead-code public header roots stay bounded to directly included local headers.
  Do not add repo-wide header scans to the per-file parse path.
- Function pointer and callback roots are conservative metadata roots; they do
  not claim exact C reachability while macro expansion, conditional compilation,
  include graphs, and dynamic symbol lookup remain unresolved.

## Common Changes And Scope

- Add C parser behavior by starting with focused parser tests in the parent
  parser package or this package.
- Add dead-code root behavior in `dead_code_roots.go` and pair it with query
  tests that prove rooted C functions are suppressed from cleanup results.
- Keep registry dispatch and runtime parser lookup in the parent parser package.
- Keep shared cross-language primitives in internal/parser/shared.

## Anti-Patterns

- Importing the parent parser package.
- Moving registry or engine dispatch into this package.
- Changing payload keys without updating downstream parser tests.
- Treating every non-static C function as public API without header evidence.
- Walking the whole repository looking for headers during every C parse.
