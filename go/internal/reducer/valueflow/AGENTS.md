# Agent instructions: internal/reducer/valueflow

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The value-flow fixpoint solver that produces the
`reducer/code-interproc-fixpoint` `TAINT_FLOWS_TO` evidence source: Program
assembly, the in-process/durable weak-component cache, the evidence
loader/projector pair, and the graph-backed cloud sink target loader (issue
#6061). Moved out of the reducer root as its own package. See the README's
Purpose and Ownership boundary sections for exactly what stays in root
despite similar naming (`code_value_flow_stale_cleanup_runner.go`,
`code_value_flow_backfill_state_marker.go`).

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/valueflow/README.md`
- `go/internal/reducer/codetaint/README.md` (the direct evidence sibling this package writes through)
- `docs/internal/design/package-restructure.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via `value_flow_compat.go`'s
  aliases and `cmd/reducer`'s wiring), never the reverse.
- **`GraphQueryRunner` in `graph_ports.go` is deliberately re-declared, not
  imported from root.** It is genuinely owned by root (shared with other
  still-in-root families). Go's structural typing makes the local
  declaration safe: do not "fix" this by adding a root import, and do not
  delete the local declaration without first hoisting the real one to a
  shared leaf package both sides import.
- **The fixpoint uid namespace is separate from `codetaint`'s direct
  `code_interproc_evidence` namespace, on purpose.** Always call
  `codetaint.ExtractCodeInterprocFixpointEvidenceRows`, never
  `ExtractCodeInterprocEvidenceRows`, from this package's write path.
  Unifying them lets a fixpoint-solved edge collide with a direct-fact edge
  in the graph writer's `MERGE`.
- **`ProjectValueFlowFixpointEvidence` retracts the WHOLE fixpoint evidence
  source (or the ledger's enumerated uids), not a scope-stamped slice.** The
  solve reads global durable summary/source state; a scoped retract would
  leave stale edges from scopes not in the triggering batch. Do not change
  this to a scope-stamped retract without re-reading the doc comment on that
  method.
- **When a `Ledger` is wired, the ledger record must happen before the graph
  write**, mirroring `codetaint`'s own ledger-is-a-superset-of-graph
  invariant (issue #4893).

## Common changes

Adding a new value-flow finding field: extend `ValueFlowFixpointEvidenceLoader`'s
row-building (around `LoadCodeInterprocEvidence`), which produces
`codetaint.CodeInterprocEvidenceInput` values — the field itself likely
belongs in `codetaint`'s typed-decode/row shapes, not here. This package only
composes and solves; it does not own the evidence row schema.

Changing the cache key (`valueFlowComponentKey`/`valueFlowSnapshotComponentKey`):
both the in-memory (`value_flow_fixpoint_cache.go`) and durable-snapshot
(`value_flow_fixpoint_snapshot.go`) paths derive component identity from
function-summary content versions and directed edge shape. Keep both key
derivations in sync, or a restart's durable-cache reuse will silently diverge
from the in-process cache's invalidation behavior.

## Failure modes to avoid

- Adding a caller of `code_value_flow_backfill_state_marker.go`'s
  `CodeValueFlowBackfillStateMarker` from this package — despite the name
  overlap, that interface belongs to the still-in-root
  `projected_source_edge_backfill` family, not this one.
- Wiring `ValueFlowProgramAssemblyRunner` into `cmd/reducer` without first
  checking whether production assembly should stay inline inside
  `ValueFlowFixpointEvidenceLoader.LoadCodeInterprocEvidence` instead — the
  runner exists today as a bounded batch driver, not a proven replacement
  for the inline path.
- Bypassing `NewValueFlowFixpointCache()` to construct a `ValueFlowFixpointCache`
  literal directly outside a test — the zero-value `entries` map is nil and
  `get`/`put` guard against a nil cache receiver, but external callers
  should use the constructor.

## Do not change without ADR review

- The separate uid namespaces for direct (`codetaint`) vs. fixpoint
  (`ExtractCodeInterprocFixpointEvidenceRows`) interproc evidence.
- The evidence-source string `codetaint.CodeInterprocFixpointEvidenceSource()`
  this package's projector retracts and writes under — `cmd/reducer` wiring
  and the reducer root's `CodeFunctionSummaryMaterializationHandler` both key
  off it.
