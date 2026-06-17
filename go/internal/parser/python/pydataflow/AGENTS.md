# AGENTS.md - internal/parser/python/pydataflow guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: precise vs conservative lowering
3. `lower.go` - `LowerFunction` and the recursive statement lowering
4. `bindings.go` - parameter, def/use, assignment-target, and attribute handling
5. `taintfacts.go` - the Python source/sink/sanitizer catalog and `TaintFacts`
6. `lineindex.go` - maps source lines to CFG statement IDs for fact placement
7. `lower_test.go` / `taintfacts_test.go` - reaching-def and taint-catalog proofs
8. The counterparts this mirrors: `../../golang/cfg_lower.go`,
   `../../javascript/jsdataflow/lower.go` and `taintfacts.go`, and the shared
   engine `../../cfg`

## Invariants this package enforces

- Reuse the shared `internal/parser/cfg` engine; do not reimplement reaching
  definitions here.
- Lower control flow precisely for blocks, if/elif/else, for-in, while, with,
  and try/except. Unmodeled constructs contribute uses but no defs (a safe false
  negative, never a false edge).
- `try` handlers (except/else/finally) branch from the pre-try state, never from
  the body end: a body definition must not reach a handler as if the body always
  completed (that would be a false edge). The lost body->handler flow is an
  accepted false negative.
- Attribute access `a.b` is a use of `a` only; the attribute name must not be
  collected as a variable use (it could collide with a same-named binding).
- A member/subscript assignment target reads its base, never defines; a
  tuple/list target defines each identifier.
- An `augmented_assignment` reads and writes its target.
- Do not descend into nested function/lambda bodies for the enclosing function's
  uses (`walkInFunction` enforces this for the taint catalog too).
- The taint catalog matches by final call name only; keep it conservative.
  Sanitizers must be unambiguous and recorded only for a DIRECT sanitizer call
  (never a conditional branch), so a real flow is never wrongly suppressed.

## Common changes and how to scope them

- Add a control construct (match/with/try): add a `lowerX` method in `lower.go`
  and a fixture in `lower_test.go` first (assert def->use by source line).
- Add a binding shape: extend `assignDefsUses`/`assignTargets`/`exprUses` in
  `bindings.go` with a test; keep attribute/subscript targets as base reads.
- Extend the taint catalog: add a name to `pySinkCallKinds`/
  `pySanitizerCallKinds`/`pySourceParamNames` in `taintfacts.go` with a
  `taintfacts_test.go` case. A new sink needs a TAINTED proof; a new sanitizer
  needs a wrong-kind proof so the kind-set model stays honest.

## Failure modes and how to debug

- Missing def->use edge: the statement kind is unhandled or a field name differs
  from the grammar. Dump the AST node kinds for the fixture.
- A false use of a variable named like an attribute: `exprUses` did not skip the
  `attribute` field of an `attribute` node.

## Do not change without review

- The shared cfg engine reuse, the attribute-skip in `exprUses`, or the
  nested-function exclusion.
- The direct-call-only sanitizer rule in `markSanitizer`; descending into
  conditional values would suppress real findings.
