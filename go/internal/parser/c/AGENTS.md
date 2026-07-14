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
- Header path cleanup and symlink resolution must keep public-header reads
  inside the caller-supplied repository root.
- Function pointer and callback roots are conservative metadata roots; they do
  not claim exact C reachability while macro expansion, conditional compilation,
  include graphs, and dynamic symbol lookup remain unresolved.
- `annotateCDeadCodeRoots` resolves from the ordered node slice `Parse`
  gathers during its one tree walk (`gatheredResolutionNodes`), not from its
  own `shared.WalkNamed` traversal (issue #4870). Do not reintroduce a second
  full-tree walk in this package; extend the gather step in `Parse` and add
  the new node kind to the resolution loop instead.
  `walk_count_test.go::TestParseFullTreeWalkCount` fails if a second full pass
  is added. The gathered slice must stay a single interleaved (pre-order)
  slice, not per-kind grouped slices: when one function accumulates root
  kinds from more than one node kind, the append order must match their
  relative source position, not a fixed kind-group order.

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

## Within-Node Regex Audit (issue #3573)

This package's regex sites were audited for the parser regex-to-AST epic
(#3531). Disposition:

### Migrated to AST, then deleted

- `cTypedefAliasPattern` (`parser.go`) — DELETED. It extracted the alias name of
  `typedef struct/enum/union { ... } Name;` and was a fallback inside
  `cTypedefName` and `cTypedefBucket`. A grammar probe (`type_definition` node
  dump) confirmed the alias always appears under the `declarator` field
  (`type_identifier` for plain/anonymous bodies, `function_declarator` for
  function-pointer typedefs, `array_declarator` for array typedefs) and the
  underlying type under the `type` field (`struct_specifier` / `enum_specifier`
  / `union_specifier`, with a named struct exposing `[field=name]`). The
  existing field-based descent plus `cTypedefAliasName` already covered every
  form, so the regex branches were dead code. Proven dead by replacing each
  fallback with a `panic` and running the full `./internal/parser/...` suite
  green, then removing the regex and the now-unreachable branches. Byte-parity:
  no payload key, bucket assignment, `name`, `line_number`, `end_line`, `lang`,
  or `type` value changed.

### Documented permanent exceptions (NOT migrated)

All remaining `parser/c` regexes live in `dead_code_roots.go` and are
out-of-AST or call-site/initializer EVIDENCE, not primary symbol extraction:

- `cHeaderPrototypePattern`, `cBlockCommentPattern`, `cLineCommentPattern` —
  scan the bytes of EXTERNAL local header files read via `os.ReadFile` in
  `AnnotatePublicHeaderRoots`. The header is not part of the tree-sitter parse
  of the current source, so this is a raw-text scan of an unparsed file
  (bounded, no transitive include resolution). Out-of-AST evidence.
- `cFunctionPointerTypedefPattern`, `cDirectInitializerTargetPattern`,
  `cBraceInitializerPattern` — dead-code-root EVIDENCE (function-pointer typedef
  names plus initializer targets) over already-located `declaration` node text,
  used to mark functions referenced via pointers. Call-site / initializer
  evidence (owner-confirmation category); not migrated here.

### No-Regression Evidence

- `cd go && gofmt -l internal/parser/c internal/parser/engine_systems_test.go` →
  empty.
- `cd go && go test ./internal/parser/... -count=1` → all packages `ok`
  (parser typedef tests included).
- Added `TestDefaultEngineParsePathCTypedefAliasMultiDeclaratorAndArray` (pins
  multi-declarator `Multi, *MultiPtr` → first alias `Multi`, and array typedef
  `buffer`); confirmed failing-first by disabling the declarator-field path
  before the regex deletion.
- `cd go && golangci-lint run ./internal/parser/...` → 0 issues.
- `scripts/verify-package-docs.sh` → changed Go package docs present.
- `rg -n 'regexp\.' go/internal/parser/c/` → only the six justified-exception
  sites in `dead_code_roots.go` remain.

### No-Observability-Change

No telemetry, spans, metrics, or logs were added or changed. This package emits
no telemetry directly; parser timing stays owned by the parent engine. The
change is a within-package symbol-extraction refactor at byte-parity.

## Dead-Code-Roots Walk Merge (issue #4870)

`annotateCDeadCodeRoots` used to run a second full-tree `shared.WalkNamed`
traversal to resolve `call_expression`/`declaration` nodes against
`payload["functions"]`. `Parse`'s main walk now gathers those node pointers
(`shared.CloneNode`) into one ordered slice as it visits them, and
`annotateCDeadCodeRoots` resolves the slice in an in-memory loop instead of
re-walking the tree. See
[docs/public/languages/c.md#dead-code-roots-walk-merge-issue-4870](../../../../docs/public/languages/c.md#dead-code-roots-walk-merge-issue-4870)
for the full performance evidence.

- No-Regression Evidence: `TestParseFullTreeWalkCount` pins the walk count at
  7 (down from 8: the eliminated pass was the second full-tree traversal, not
  a bounded `firstNamedDescendant` subtree scan). `TestGatherResolveCrossKindDeclBeforeCall`
  and `TestGatherResolveCrossKindCallBeforeDecl` pin `dead_code_root_kinds`
  ordering for a function tagged by both a declaration-based and a
  call-expression-based root, in both source orderings.
  `TestGatherResolveForwardReferenceSignalHandler` pins that a signal handler
  registered before its own definition still resolves. All pre-existing
  parent-package C dead-code-root tests
  (`go/internal/parser/c_dead_code_roots_test.go`) stayed green with zero
  changes. `0/0` symmetric diff (`diff` + `comm -3` both empty) over the
  24-file C/H fixture corpus via `equivalence_dump_test.go`
  (`C_PARSE_DUMP`), old worktree vs. new worktree.
- No-Observability-Change: this package emits no telemetry by design; the
  gather-then-resolve refactor neither adds nor removes spans, metrics, or
  logs.
