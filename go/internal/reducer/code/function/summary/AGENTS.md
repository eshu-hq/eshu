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
- `go/internal/reducer/code/value/README.md` (the fixpoint sibling this package's projector calls into)
- `docs/internal/design/reducer-target-tree.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the code-function-summary
  stanza of `compat_decode.go` and `defaults_additive_domains_incident_code.go`'s
  wiring), never the reverse.
- **`internal/parser/summary` is always imported as `flow`, never bare
  `summary`.** This package's own name is `summary`; importing the parser
  package unaliased would either collide or force every call site to
  self-reference as `summary.summary.X`.
- **The graph-id view discards its own quarantines.** It reads the SAME
  `code_function_summary` facts the summary-effects view already
  quarantined; do not double-count one malformed fact's quarantine by
  recording both views' results.
- **The fixpoint projector always runs after every durable write.** Do not
  reorder `Handle` to call `ValueFlowFixpointWriter` before the
  summary/source/graph-id persistence completes.

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
  through `Definition`, `MaterializationHandler`, and the `Extract*`
  functions.

## Do not change without ADR review

- The separate quarantine-discard rule for the graph-id view (it must not
  double-count the summary-effects view's quarantines on
  `input_invalid_facts`).
- The fixpoint-projector-runs-last ordering in `MaterializationHandler.Handle`.
