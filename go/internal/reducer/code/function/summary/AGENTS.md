# Agent instructions: internal/reducer/code/function/summary

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

Persists one generation's durable value-flow function summaries, param-level
taint sources, and the `FunctionID`->graph-uid map (issue #6061). Moved out
of the reducer root as its own package. See the README's Purpose and
Ownership boundary sections for exactly what this package owns versus what
`code/value`'s fixpoint solver owns.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/function/summary/README.md`
- `go/internal/reducer/code/value/README.md` (the fixpoint sibling this package used to solve directly)
- `go/internal/reducer/code/value/refresh/README.md` (the singleton this package's ACK now feeds, issue #6923)
- `docs/internal/design/reducer-target-tree.md`
- `docs/internal/evidence/6923-value-flow-single-solve.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the code-function-summary
  stanza of `compat_decode.go` and `defaults_additive_domains_incident_code.go`'s
  wiring), never the reverse.
- **`internal/parser/summary` is always imported as `parsed`, never bare
  `summary`.** This package's own name is `summary`; importing the parser
  package unaliased would either collide or force every call site to
  self-reference as `summary.summary.X`.
- **The graph-id view discards its own quarantines.** It reads the SAME
  `code_function_summary` facts the summary-effects view already
  quarantined; do not double-count one malformed fact's quarantine by
  recording both views' results.
- **`Handler` must never solve the value-flow fixpoint itself (issue
  #6923).** It has no field for a fixpoint projector; do not re-add one. The
  handler's job is to make its ACK an accurate value-flow-refresh producer
  signal (`refresh_affected_repos`, `CanonicalWrites` including removed
  rows), not to run the solve.
- **`CanonicalWrites` must include removed rows on a full-snapshot replace.**
  A replace that empties a repo still changed that repo's fixpoint inputs and
  must still trigger the refresh singleton even though
  `persistedFunctionCount == 0`.

## Common changes

Adding a new function-summary field: extend `flow.Effects` in
`internal/parser/summary` first, then thread the field through
`codeFunctionSummaryEffects` (`decode.go`) and the corresponding envelope
builder in the moved handler tests.

## Failure modes to avoid

- Exporting a new unexported helper "just in case" a future caller needs it.
  This package's exports exist because the reducer root's compat forwarders,
  `cmd/reducer`'s wiring, or the Postgres store implementers need them;
  nothing else in this package has an external caller today.
- Reintroducing a root-local copy of `codeFunctionSummaryEffects`,
  `codeFunctionGraphID`, or `codeFunctionSource` — they are unexported and
  package-local on purpose; the reducer root reaches this family only
  through `Definition`, `Handler`, and the `Extract*`
  functions.

## Do not change without ADR review

- The separate quarantine-discard rule for the graph-id view (it must not
  double-count the summary-effects view's quarantines on
  `input_invalid_facts`).
- The no-inline-solve invariant (issue #6923): this handler must not regain a
  fixpoint-projector field. The refresh singleton
  (`code/value/refresh.Handler`) is the only global-solve entry point.
