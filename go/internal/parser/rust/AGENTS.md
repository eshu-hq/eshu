# AGENTS.md - internal/parser/rust guidance

## Read First

1. README.md - package boundary and parser responsibilities
2. doc.go - godoc contract
3. parser.go - Rust payload extraction
4. helpers.go - tree-sitter and syntax helper functions
5. lifetimes.go - AST-node lifetime collection (parameters, signature, return)
6. metadata.go - attribute, import, module, and generic metadata helpers

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

## Within-Node Regex Audit (issue #3572)

This audit classified every regex in this package as either migrated to
tree-sitter AST extraction or a justified permanent exception.

### Moved to AST

- Lifetime extraction (`rustLifetimePattern`, removed). `lifetime_parameters`,
  `signature_lifetimes`, and `return_lifetime` now read `lifetime` grammar nodes
  via `lifetimes.go`. The Rust grammar exposes lifetimes as `lifetime` nodes
  (`'a`) under the `type_parameters`, `parameters`, `trait_bounds`,
  `where_clause`, and `return_type` fields. `rustDeclaredLifetimeParameters`
  walks the declaring `type_parameters` field and reads each
  `lifetime_parameter`'s `name` field; `rustSignatureLifetimeNames` walks the
  item node and skips the `body` field range so body lifetimes (for example a
  `'a'` char literal) are excluded, matching the prior header-only scan;
  `rustReturnLifetimeName` reads the first `lifetime` under the `return_type`
  field. Byte-parity is preserved: names drop the leading apostrophe, first-seen
  left-to-right order holds, and duplicates collapse.
- Macro-definition name (`rustMacroRulesPattern`, removed). The grammar always
  exposes the macro name as the `macro_definition` node's `name` field
  (`identifier`). A `macro_rules!` form with no name parses as an `ERROR` node,
  not a `macro_definition`, so the parser switch never dispatches to the name
  read for an unnamed macro. The regex fallback was therefore unreachable dead
  code; `rustMacroDefinitionName` now reads the `name` field directly.

### Permanent exceptions (not migrated)

- `rustMacroModDeclarationPattern` / `rustMacroUseDeclarationPattern`
  (`macro_declarations.go`). These extract `mod`/`use` declarations from the
  body of a `macro_invocation`. Tree-sitter does not expand macros, so the body
  is an unparsed `token_tree`, not parsed AST — there are no symbol nodes to
  walk. This is extraction over text the grammar does not model as symbol nodes,
  the same judgment class as the merged Python embedded-shell work. Rows stay
  tagged `macro_expansion_unavailable`.
- `rustWhereClausePattern` / `rustIdentifierPattern` (`helpers.go`). Text helpers
  over already-extracted node-text slices: one splits a signature header string
  on the ` where ` keyword, the other validates that a candidate token is a bare
  identifier. Neither scans raw source for symbols, so both remain content/text
  helpers, not symbol extractors.

No-Regression Evidence:

- `cd go && gofmt -l internal/parser/rust` (empty output).
- `cd go && go test ./internal/parser/... -count=1` (all packages `ok`;
  `internal/parser/rust` passes, including the new `TestRustLifetimeASTParity`
  (7 subtests) and `TestRustImplLifetimeASTParity` characterization tests that
  were proven green against the prior regex before the AST swap).
- `cd go && golangci-lint run ./internal/parser/...` (`0 issues`).
- `rg -n 'regexp\.' go/internal/parser/rust/ --glob '!*_test.go'` returns 4
  sites (the documented permanent exceptions), down from 6.

No-Observability-Change: parser-internal change only. No metric, span, log,
status, env var, queue, worker, or graph-write behavior changed. The emitted
payload keys and value shapes are unchanged (byte-parity).
