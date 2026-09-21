# AGENTS.md - internal/parser/golang/dataflow guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: precise vs conservative lowering
3. `lower.go` - `goLowerFunction` and the recursive statement lowering
4. `bindings.go` - parameter, def/use, assignment, and closure-capture
   extraction
5. `access_paths.go` - field-sensitive access paths, container `[*]`, and the
   pointer-alias map
6. `emit.go` / `interproc.go` - the two exported entry points
   (`EmitBuckets`, `InterprocPayloads`) `language.go` calls
7. `guard_text_test.go` / `lower_test.go` - guard-predicate and if/else-merge,
   back-edge, and access-path precision proofs
8. The TS/JS counterpart this mirrors:
   `../../javascript/jsdataflow/lower.go`,
   `../../javascript/jsdataflow/bindings.go`,
   `../../javascript/jsdataflow/accesspaths.go`, and the shared engine
   `../../../cfg`

## Invariants this package enforces

- Reuse the shared `internal/parser/cfg` engine; do not reimplement reaching
  definitions here.
- Lower control flow precisely for blocks, if/else, and for loops (including
  for-range). Unmodeled constructs (switch, select) contribute uses but no
  defs — a safe false negative, never a false edge.
- Bindings are field-sensitive access paths (`access_paths.go`): a selector
  target `data.SQL` defines `data.SQL`; a subscript lowers to the labeled
  whole-container approximation `m[*]`; deep paths truncate to a
  `*`-suffixed prefix and count `Overflow.AccessPaths`. Never emit a silent
  over-approximation.
- Only a direct `&x` pointer alias (and chains over an existing alias)
  normalizes a field write back to the aliased struct; a plain value copy is
  never treated as an alias. An alias declared in an `if` initializer is
  scoped to that `if` and must not leak past the merge.
- Descend into a nested function-literal body for the enclosing function's
  uses ONLY when the literal is a call argument (closure capture), excluding
  the closure's own params and inner-scope defs.
- A `+=`-style compound assignment and `x++`/`x--` both read and write their
  target; a plain `=` assignment only writes.
- Guard predicate text is redacted (`redactGoGuardLiterals`) and
  whitespace-normalized before it is stored or negated.
- Output is bounded and deterministic via the cfg engine.
- **Never import `internal/parser/golang`.** That back-edge is the cycle
  issue #6774 exists to remove. Anything this package needs from the old flat
  package lives in `internal/parser/golang/symbols` — import that instead.
- Do not rewrite a function body while moving code into or out of this
  package. The accuracy golden gate and a parser equivalence dump compare
  emitted facts; a behavior change here is a defect, not a refactor.

## Common changes and how to scope them

- Add a control construct: add a `lowerX` method in `lower.go` and a fixture
  in `lower_test.go` first (assert def->use by source line, mirroring the
  existing `defUseLines` helper).
- Add a binding shape: extend `goStmtDefsUsesWithOptions`/
  `goExprUsesWithOptions` in `bindings.go` (threading the alias map and
  access-path options).
- Extend the taint catalog: update the source/sink/sanitizer maps in
  `taint_facts.go` and add both a positive case and a same-name-unrelated
  negative case. Keep `goEffectsSpec` (`effects.go`) source handling aligned
  with `goTaintFacts`.
- Change what `language.go` receives: `EmitBuckets` and `InterprocPayloads`
  are the only two exported symbols. Adding a third caller-visible entry
  point means exporting one more identifier here and repointing the caller in
  `internal/parser/golang/language.go` — never widening this package's import
  of `internal/parser/golang` to reach back in.

## Failure modes and how to debug

- Missing def->use edge: the statement kind is unhandled (falls to the
  default uses-only path in `lowerStmt`) or a tree-sitter field name differs
  from the grammar assumed by `bindings.go`. Dump the AST node kinds for the
  fixture.
- A closure's captured variable attributed to the outer function when the
  literal is NOT a call argument: `goExprUsesWithOptions`'s
  `includeFuncLiteralCaptures` flag was threaded incorrectly; closures are
  captured only in call-argument position (`current.Kind() ==
  "call_expression"` in `bindings.go`).
- A field write produced no def: the target was not a precise access path
  (for example an unsupported expression shape); such targets read their
  components but define nothing (`goAssignmentDefsUsesWithOptions`).
- A false sink finding on an unrelated type's same-named method: extend
  `goSinkQualifiedKinds` with a qualified `base.field` match instead of
  widening `goSinkMethodKinds`, which matches by bare method name alone.

## Do not change without review

- The shared cfg engine reuse.
- The call-argument gating of closure capture in `goExprUsesWithOptions`
  (`current.Kind() == "call_expression"` before descending with
  `includeFuncLiteralCaptures`); over-descending invents cross-scope uses.
- The pointer-alias resolution in `access_paths.go`/`lower.go`: only a
  literal `&x` (or a chain over an existing alias) is alias-resolved, and
  `if`-initializer aliases are restored to the outer scope after the merge
  (`restoreScopedAliases`) so a branch-local alias never leaks.
