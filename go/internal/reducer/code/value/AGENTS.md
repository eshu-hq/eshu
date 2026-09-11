# Agent instructions: internal/reducer/code/value

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The value-flow fixpoint solver that produces the
`reducer/code-interproc-fixpoint` `TAINT_FLOWS_TO` evidence source: Program
assembly, the in-process/durable weak-component cache, the evidence
loader/projector pair, and the graph-backed cloud sink target loader (issue
#6061). Moved out of the reducer root as its own package, and relocated from
`internal/reducer/valueflow` to `internal/reducer/code/value` (package
`value`, dropping the `ValueFlow` prefix from its exported identifiers) under
the same issue. See the README's Purpose and Ownership boundary sections for
exactly what stays in root despite similar naming
(`code_value_flow_stale_cleanup_runner.go`) and why
`backfill_state_marker.go` lives here with no caller in this package.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/code/value/README.md`
- `go/internal/reducer/code/taint/README.md` (the direct evidence sibling this package writes through)
- `docs/internal/design/package-restructure.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it (via the value-flow stanza of
  `compat_projection.go` and `cmd/reducer`'s wiring), never the reverse.
- **`GraphQueryRunner` in `graph_ports.go` is deliberately re-declared, not
  imported from root.** It is genuinely owned by root (shared with other
  still-in-root families). Go's structural typing makes the local
  declaration safe: do not "fix" this by adding a root import, and do not
  delete the local declaration without first hoisting the real one to a
  shared leaf package both sides import.
- **The fixpoint uid namespace is separate from `taint`'s direct
  `code_interproc_evidence` namespace, on purpose.** Always call
  `taint.ExtractInterprocFixpointEvidenceRows`, never
  `ExtractInterprocEvidenceRows`, from this package's write path.
  Unifying them lets a fixpoint-solved edge collide with a direct-fact edge
  in the graph writer's `MERGE`.
- **`ProjectValueFlowFixpointEvidence` keeps its `ValueFlow` infix on
  purpose**, unlike every other renamed identifier in this package: it
  satisfies `code/function/summary`'s `ValueFlowFixpointProjector` interface,
  which names the method verbatim. Renaming it breaks that structural
  contract. It retracts the WHOLE fixpoint evidence source (or the ledger's
  enumerated uids), not a scope-stamped slice. The solve reads global durable
  summary/source state; a scoped retract would leave stale edges from scopes
  not in the triggering batch. Do not change this to a scope-stamped retract
  without re-reading the doc comment on that method.
- **When a `Ledger` is wired, the ledger record must happen before the graph
  write**, mirroring `taint`'s own ledger-is-a-superset-of-graph
  invariant (issue #4893).
- **`LoadValueFlowFixpointComponents`/`StoreValueFlowFixpointComponents` (on
  `FixpointComponentStore`) also keep their `ValueFlow` infix on purpose**,
  matching `internal/storage/postgres.ValueFlowFixpointComponentStore`'s
  method names — an external, structurally-satisfying implementer this
  package does not own.

## Common changes

Adding a new value-flow finding field: extend `FixpointEvidenceLoader`'s
row-building (around `LoadCodeInterprocEvidence`), which produces
`taint.InterprocEvidenceInput` values — the field itself likely
belongs in `taint`'s typed-decode/row shapes, not here. This package only
composes and solves; it does not own the evidence row schema.

Changing the cache key (`valueFlowComponentKey`/`valueFlowSnapshotComponentKey`):
both the in-memory (`code/value/fixpoint_cache.go`) and durable-snapshot
(`code/value/fixpoint_snapshot.go`) paths derive component identity from
function-summary content versions and directed edge shape. Keep both key
derivations in sync, or a restart's durable-cache reuse will silently diverge
from the in-process cache's invalidation behavior.

## Failure modes to avoid

- Treating `BackfillStateMarker` (`backfill_state_marker.go`) as part of the
  fixpoint. It moved here under #6609 so the root could shed the file; its one
  caller is the root's `projected_source_edge_backfill` family (through the
  `CodeValueFlowBackfillStateMarker` alias), and `taint` keeps its own
  structural copy because this package imports `taint`.
- Wiring `ProgramAssemblyRunner` into `cmd/reducer` without first
  checking whether production assembly should stay inline inside
  `FixpointEvidenceLoader.LoadCodeInterprocEvidence` instead — the
  runner exists today as a bounded batch driver, not a proven replacement
  for the inline path.
- Bypassing `NewFixpointCache()` to construct a `FixpointCache`
  literal directly outside a test — the zero-value `entries` map is nil and
  `get`/`put` guard against a nil cache receiver, but external callers
  should use the constructor.
- Renaming `ProjectValueFlowFixpointEvidence`,
  `LoadValueFlowFixpointComponents`, or `StoreValueFlowFixpointComponents` to
  drop their `ValueFlow` infix "for consistency" with the rest of the
  package — see the two Invariants above naming exactly why each one stays.

## Do not change without ADR review

- The separate uid namespaces for direct (`taint`) vs. fixpoint
  (`ExtractInterprocFixpointEvidenceRows`) interproc evidence.
- The evidence-source string `taint.InterprocFixpointEvidenceSource()`
  this package's projector retracts and writes under — `cmd/reducer` wiring
  and `code/function/summary`'s `MaterializationHandler` both key off it.
