# AGENTS.md - internal/parser/python/pydataflow guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: precise vs conservative lowering
3. `lower.go` - `LowerFunction` and the recursive statement lowering
4. `bindings.go` - parameter, def/use, assignment-target, and attribute handling
5. `lower_test.go` - if/elif/else-merge and for-loop back-edge reaching-def proofs
6. The counterparts this mirrors: `../../golang/cfg_lower.go`,
   `../../javascript/jsdataflow/lower.go`, and the shared engine `../../cfg`

## Invariants this package enforces

- Reuse the shared `internal/parser/cfg` engine; do not reimplement reaching
  definitions here.
- Lower control flow precisely for blocks, if/elif/else, for-in, and while.
  Unmodeled constructs contribute uses but no defs (a safe false negative, never
  a false edge).
- Attribute access `a.b` is a use of `a` only; the attribute name must not be
  collected as a variable use (it could collide with a same-named binding).
- A member/subscript assignment target reads its base, never defines; a
  tuple/list target defines each identifier.
- An `augmented_assignment` reads and writes its target.
- Do not descend into nested function/lambda bodies for the enclosing function's
  uses.

## Common changes and how to scope them

- Add a control construct (match/with/try): add a `lowerX` method in `lower.go`
  and a fixture in `lower_test.go` first (assert def->use by source line).
- Add a binding shape: extend `assignDefsUses`/`assignTargets`/`exprUses` in
  `bindings.go` with a test; keep attribute/subscript targets as base reads.

## Failure modes and how to debug

- Missing def->use edge: the statement kind is unhandled or a field name differs
  from the grammar. Dump the AST node kinds for the fixture.
- A false use of a variable named like an attribute: `exprUses` did not skip the
  `attribute` field of an `attribute` node.

## Do not change without review

- The shared cfg engine reuse, the attribute-skip in `exprUses`, or the
  nested-function exclusion.
