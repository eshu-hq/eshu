# AGENTS.md - internal/parser/python/pydataflow guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: precise vs conservative lowering
3. `lower.go` / `lower_blocks.go` - `LowerFunction`, the recursive statement
   lowering, alias threading, and the with/try block lowering
4. `bindings.go` - parameter, def/use, assignment-target, and closure-capture
   extraction
4a. `accesspaths.go` - field-sensitive access paths, container `[*]`, and the
   reference-alias map
5. `taintfacts.go` - the Python source/sink/sanitizer catalog and `TaintFacts`
6. `lineindex.go` - maps source lines to CFG statement IDs for fact placement
7. `effects.go` / `interproc.go` - value-flow `EffectsSpec` extraction and the
   intra-file `InterprocFindings` composition
8. `lower_test.go` / `taintfacts_test.go` / `interproc_test.go` - reaching-def,
   taint-catalog, and cross-function proofs
9. The counterparts this mirrors: `../../golang/cfg_lower.go`,
   `../../javascript/jsdataflow/lower.go`, `taintfacts.go`, `effects.go`,
   `interproc.go`, and the shared engine `../../cfg`

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
- Bindings are field-sensitive access paths (`accesspaths.go`): an attribute
  target `obj.attr` defines `obj.attr`; an attribute read records `obj.attr` plus
  the base object; a subscript lowers to the labeled whole-container
  approximation `d[*]`; deep paths truncate to a `*`-suffixed prefix and count
  `Overflow.AccessPaths`. The attribute name is never collected as a bare
  variable use. Never emit a silent over-approximation.
- Reference aliases (`a = obj`) normalize only the base segment of a multi-part
  access path; bare identifier reads are never alias-resolved, so reaching-def
  truth for simple value flow is unchanged. Clone/intersect the alias map across
  if/elif/else and loops; rebinding a name (loop target, `as` alias, reassign)
  drops its alias; `try` clears aliases on exit.
- A tuple/list target defines each identifier (including nested attribute/
  subscript elements as access paths).
- An `augmented_assignment` reads and writes its target.
- Descend into a lambda body for the enclosing function's uses ONLY when the
  lambda is a call argument (closure capture), excluding its params and
  inner-scope assignments. A non-invoked lambda or a nested def is not descended
  into. The taint catalog's `walkInFunction` still never descends into nested
  scopes (a sink inside a closure stays unattributed).
- The taint catalog requires typed framework request parameters for sources and
  qualified receiver/module evidence for sinks; keep it conservative. Sanitizers
  must be unambiguous and recorded only for a DIRECT sanitizer call (never a
  conditional branch), so a real flow is never wrongly suppressed.

## Common changes and how to scope them

- Add a control construct (match/with/try): add a `lowerX` method in `lower.go`
  and a fixture in `lower_test.go` first (assert def->use by source line).
- Add a binding shape: extend `assignDefsUsesWithOptions`/`pyTargetDefUses`/
  `exprUsesWithOptions` in `bindings.go` (threading the alias map and access-path
  options) with a test in `precision_test.go`.
- Extend the taint catalog: update the typed source or qualified sink specs in
  `taintfacts.go` with `taintfacts_test.go` coverage. A new sink needs a
  TAINTED proof plus a same-name unrelated negative; a new sanitizer needs a
  wrong-kind proof so the kind-set model stays honest. Keep `EffectsSpec` source
  handling aligned with `TaintFacts`.

## Failure modes and how to debug

- Missing def->use edge: the statement kind is unhandled or a field name differs
  from the grammar. Dump the AST node kinds for the fixture.
- A false use of a variable named like an attribute: `exprUsesWithOptions`
  returns early after an `attribute` node, so the attribute name is not visited.
- A lambda's captured variable attributed when it is NOT a call argument:
  `pyInvokesFuncLiteral` returned true outside call-argument position.
- An attribute write produced no def: the target was not a precise access path.

## Do not change without review

- The shared cfg engine reuse.
- The call-argument gating of closure capture in `exprUsesWithOptions`
  (`pyInvokesFuncLiteral`); over-descending invents cross-scope uses.
- Resolving bare identifier reads through the alias map: only the base of a
  multi-part access path is alias-resolved, or simple reaching-def truth breaks.
- The alias clone/intersect across branches and the drop-on-rebind/clear-on-try
  rules; a leaked stale alias can mislabel a path and invent a false edge.
- The direct-call-only sanitizer rule in `markSanitizer`; descending into
  conditional values would suppress real findings.
- The top-level-only, bare-identifier-only call resolution in `effects.go`/
  `interproc.go`. Resolving method calls or nested (lexically private) functions
  would invent false cross-function edges.
