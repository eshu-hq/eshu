# AGENTS.md - internal/parser/javascript/jsdataflow guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: precise vs conservative lowering
3. `lower.go` - `LowerFunction` and the recursive statement lowering
4. `bindings.go` - parameter, def/use, assignment, and closure-capture extraction
5. `accesspaths.go` - field-sensitive access paths, container `[*]`, and the
   reference-alias map
6. `lower_test.go` / `precision_test.go` - if/else-merge and back-edge proofs;
   field/container/alias/closure precision proofs
7. The Go counterpart this mirrors: `../../golang/cfg_lower.go`,
   `../../golang/cfg_bindings.go`, `../../golang/cfg_access_paths.go`, and the
   shared engine `../../cfg`

## Invariants this package enforces

- Reuse the shared `internal/parser/cfg` engine; do not reimplement
  reaching definitions here.
- Lower control flow precisely for blocks, if/else, for, for-in/of, and while.
  Unmodeled constructs contribute uses but no defs (a safe false negative, never
  a false edge).
- Bindings are field-sensitive access paths (`accesspaths.go`): a member target
  `obj.field` defines `obj.field`; a subscript lowers to the labeled
  whole-container approximation `m[*]`; deep paths truncate to a `*`-suffixed
  prefix and count `Overflow.AccessPaths`. Never emit a silent over-approximation.
- Reference aliases (`let a = obj`) normalize only the base segment of a
  multi-part access path; bare identifier reads are never alias-resolved, so
  reaching-def truth for simple value flow is unchanged. Clone the alias map per
  branch, merge by intersection, and after loops keep only aliases that agree
  before the loop and at the body exit.
- Descend into a nested function/arrow body for the enclosing function's uses
  ONLY when the literal is a call argument (closure capture), excluding the
  closure's own params and inner-scope defs. A non-invoked literal is not
  descended into.
- An `augmented_assignment_expression`/`update_expression` reads and writes its
  target; a plain `assignment_expression` only writes.
- Output is bounded and deterministic via the cfg engine.

## Common changes and how to scope them

- Add a control construct: add a `lowerX` method in `lower.go` and a fixture in
  `lower_test.go` first (assert def->use by source line).
- Add a binding shape: extend `assignDefsUsesWithOptions`/`exprUsesWithOptions`
  in `bindings.go` (threading the alias map and access-path options) with a test
  in `precision_test.go`.
- Extend the taint catalog: update the typed source or qualified sink specs in
  `taintfacts.go` and add both a positive framework case and a same-name
  unrelated negative in `taintfacts_test.go`. Keep `EffectsSpec` source handling
  aligned with `TaintFacts`.

## Failure modes and how to debug

- Missing def->use edge: the statement kind is unhandled (falls to the default
  uses-only path) or a field name differs from the grammar. Dump the AST node
  kinds for the fixture.
- A closure's captured variable attributed to the outer function when the literal
  is NOT a call argument: `exprUsesWithOptions` descended via a wrong
  `invokesFuncLiteral` result; closures are captured only in call-argument position.
- A field write produced no def: the target was not a precise access path (for
  example a destructuring pattern); such targets read components but define nothing.

## Do not change without review

- The shared cfg engine reuse.
- The call-argument gating of closure capture in `exprUsesWithOptions`
  (`invokesFuncLiteral`); over-descending invents cross-scope uses.
- Resolving bare identifier reads through the alias map: only the base of a
  multi-part access path is alias-resolved, or simple reaching-def truth breaks.
