# AGENTS.md - internal/parser/javascript/jsdataflow guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: precise vs conservative lowering
3. `lower.go` - `LowerFunction` and the recursive statement lowering
4. `bindings.go` - parameter, def/use, and assignment extraction
5. `lower_test.go` - if/else-merge and for-loop back-edge reaching-def proofs
6. The Go counterpart this mirrors: `../../golang/cfg_lower.go`,
   `../../golang/cfg_bindings.go`, and the shared engine `../../cfg`

## Invariants this package enforces

- Reuse the shared `internal/parser/cfg` engine; do not reimplement
  reaching definitions here.
- Lower control flow precisely for blocks, if/else, for, for-in/of, and while.
  Unmodeled constructs contribute uses but no defs (a safe false negative, never
  a false edge).
- Do not descend into nested function/arrow bodies for the enclosing function's
  uses; closures are a later pass.
- An `augmented_assignment_expression`/`update_expression` reads and writes its
  target; a plain `assignment_expression` only writes. A member/subscript target
  reads its base, never defines it.
- Output is bounded and deterministic via the cfg engine.

## Common changes and how to scope them

- Add a control construct: add a `lowerX` method in `lower.go` and a fixture in
  `lower_test.go` first (assert def->use by source line).
- Add a binding shape: extend `assignDefsUses`/`exprUses` in `bindings.go` with a
  test; keep member/subscript targets as base reads only.
- Extend the taint catalog: update the typed source or qualified sink specs in
  `taintfacts.go` and add both a positive framework case and a same-name
  unrelated negative in `taintfacts_test.go`. Keep `EffectsSpec` source handling
  aligned with `TaintFacts`.

## Failure modes and how to debug

- Missing def->use edge: the statement kind is unhandled (falls to the default
  uses-only path) or a field name differs from the grammar. Dump the AST node
  kinds for the fixture.
- A closure's captured variable attributed to the outer function: `exprUses`
  descended into a nested function; check `isNestedFunction`.

## Do not change without review

- The shared cfg engine reuse, or the nested-function exclusion in `exprUses`.
