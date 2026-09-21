# Go Semantic Dead-Code Root Evidence

## Purpose

`semantic` derives the type-system and value-flow half of Go dead-code root
evidence: interface satisfaction, function/method value references, generic
constraint methods, and dependency-injection callback arguments. It is the
counterpart to the sibling `internal/parser/golang/deadcode` package's
registration- and signature-shape evidence, and the two are composed by
`deadcode.Evidence`. Extracted from `internal/parser/golang` (issue #6774) so
the root package stops importing everything it needs from one flat,
cycle-prone file set.

## Ownership boundary

This package owns the gather-then-resolve walk over a parsed Go file that
produces semantic dead-code root evidence (`roots.go`), the interface- and
concrete-type-tracing helpers it resolves against (`flows.go`), and the
function-value and generic-constraint helpers (`helpers.go`). It does NOT own
registration matching or signature-shape recognition (those are
`internal/parser/golang/deadcode`), or the shared symbol/type-index helpers
(`internal/parser/golang/symbols`). It never imports
`internal/parser/golang` or `internal/parser/golang/deadcode` — either
back-edge is exactly the cycle #6774 removes; every symbol it once needed
from `deadcode` moved into `symbols` in stage 1.

## Exported surface

See `doc.go` for the full godoc contract. The surface is deliberately one
function:

- `CollectRoots(root, source, importAliases, importedParamMethods,
  localNameBindings, constructorReturns, functionRootKinds,
  interfaceRootKinds, structRootKinds, lookup)` — walks the file once to
  gather resolution-candidate nodes, then resolves them in-memory, mutating
  the three root-kind maps in place. Called once per file, from
  `deadcode.Evidence`.

Everything else (the interface-target tracing, concrete-type resolution,
function-value/closure walking, and generic-constraint matching) is internal:
no other package needs it, so it stays unexported.

## Dependencies

- `internal/parser/golang/symbols` (import-alias resolution, interface method
  extraction, concrete-type resolution, the per-file variable-type index,
  parent lookup), `internal/parser/shared` (node text/line helpers, node
  cloning for nodes retained past the gathering walk, the
  `GoImportedInterfaceParamMethods` collector-supplied type), and
  `github.com/tree-sitter/go-tree-sitter`.

## Telemetry

None. `CollectRoots` is a pure function; a reducer that drives the ingest
pipeline owns telemetry for the stage that calls it.

No-Regression Evidence: this is a pure package-move (issue #6774 stage 2,
`golang/dead_code_semantic_flows.go` -> `golang/deadcode/semantic/flows.go`,
`golang/dead_code_semantic_helpers.go` ->
`golang/deadcode/semantic/helpers.go`, `golang/dead_code_semantic_roots.go` ->
`golang/deadcode/semantic/roots.go`, `package golang` -> `package semantic`).
No function body changed beyond swapping the root's `nodeText`/`nodeLine`/
`walkNamed` forwarders for the `internal/parser/shared` calls they forwarded
to, swapping the root's `GoImportedInterfaceParamMethods` type alias for its
`internal/parser/shared` original, and renaming the one symbol
`deadcode.Evidence` still calls (`goCollectSemanticDeadCodeRoots` ->
`CollectRoots`). Verified by `go test ./internal/parser/golang/... -count=1`
and the accuracy golden gate (`scripts/verify_accuracy_golden_gate.sh`), both
green on the moved code. The gather-then-resolve walk-count characterization
(`TestParseFullTreeWalkCount` in root's `walk_count_test.go`) still passes
unchanged against the moved code, proving the walk count this package
produces did not shift.

No-Observability-Change: the move adds no metric, span, log, status field,
runtime knob, queue, worker, or graph query.

## Gotchas / invariants

- **Gather in pre-order, resolve in-memory** (`roots.go`): `CollectRoots`'s
  single `shared.WalkNamed` walk clones every resolution-candidate node
  (`shared.CloneNode`) into a per-kind slice, because a `*tree_sitter.Node`
  points at a stack-allocated cursor valid only during the walk. The
  resolution loops that follow iterate those slices in the same pre-order the
  walk produced them, matching the visitation order of the two full-tree
  re-walks this replaced.
- **Forward references resolve because gathering is unconditional**: a call
  naming a function declared later, or a type parameter constraint naming an
  interface declared later, still matches — every declaration map
  (`functionNames`, `interfaceMethods`, `structTypes`, ...) is fully built in
  the gathering walk before any resolution loop runs.
- **Same-key ordering across evidence families is deterministic and
  committed**: when one `functionRootKinds` key receives kinds from multiple
  resolution loops (for example both `go.direct_method_call` and
  `go.interface_method_implementation`), the emitted order follows the fixed
  loop sequence in `CollectRoots`. Root's
  `TestDeadCodeCrossKindSameKeyOrdering` locks this in; do not reorder the
  loops without updating that test deliberately.
- **Interface targets distinguish local vs. imported**
  (`symbols.InterfaceTarget`, consumed in `flows.go`'s
  `goMarkConcreteTypeForInterfaceTarget`): a local interface target marks
  root evidence unconditionally once a concrete type is seen; an imported
  interface target only marks it when the concrete type's method set actually
  covers the imported interface's required methods (or, absent method
  evidence, when `AllowExportedMethods` allows every exported method as a
  conservative fallback).

## Related docs

- Issue #6774 (parser/golang leaf split).
- `internal/parser/golang/deadcode/README.md` — the sibling package this one
  composes with, and `deadcode.Evidence`'s call into `CollectRoots`.
- `internal/parser/golang/README.md` — the parser this evidence ultimately
  feeds.
