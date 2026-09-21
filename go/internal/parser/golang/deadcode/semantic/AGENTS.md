# AGENTS.md - internal/parser/golang/deadcode/semantic guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: the gather-then-resolve walk and the four
   evidence families
3. `roots.go` - `CollectRoots`: the single gathering walk and the in-memory
   resolution loops
4. `flows.go` - interface-target tracing and concrete-type resolution
5. `helpers.go` - function-value/closure walking and generic-constraint
   helpers
6. The caller: `internal/parser/golang/deadcode/roots.go`'s `Evidence`

## Invariants this package enforces

- **Never import `internal/parser/golang` or
  `internal/parser/golang/deadcode`.** Either back-edge is exactly the cycle
  issue #6774 exists to remove. Everything this package needs from the old
  flat package or from `deadcode` lives in `internal/parser/golang/symbols`
  — import that instead.
- Gather every resolution-candidate node in one `shared.WalkNamed` pass before
  resolving any of them, and clone every retained node with `shared.CloneNode`
  — a `*tree_sitter.Node` is only valid during the walk that produced it.
- Resolve the gathered slices in the same pre-order the gathering walk
  produced them, preserving the original two-full-tree-walk's visitation
  order and forward-reference behavior exactly.
- Root-kind evidence is additive only: resolution loops append to
  `functionRootKinds`/`interfaceRootKinds`/`structRootKinds` via
  `symbols.AppendUniqueImportAlias`, never overwrite. A missed match is a
  safe false negative; an overwritten match would silently drop evidence.
- An imported interface target only marks a concrete type when its method set
  is known to cover the imported interface's required methods (or
  `AllowExportedMethods` explicitly allows any exported method as a
  conservative fallback) — never mark on the interface name alone.
- Do not rewrite a function body while moving code into or out of this
  package. The accuracy golden gate and a parser equivalence dump compare
  emitted facts; a behavior change here is a defect, not a refactor.

## Common changes and how to scope them

- Recognize a new interface-satisfaction shape: extend the relevant
  `goCollect*`/`goMark*` function in `flows.go`, keeping the additive,
  `AppendUniqueImportAlias`-based evidence contract.
- Recognize a new function-value or closure-capture shape: extend
  `goCollectFunctionValuesFromExpression` in `helpers.go`, adding a fixture in
  root's dead-code test suite (`internal/parser/golang/*dead_code*_test.go`)
  first.
- Add a new gathered node kind to `CollectRoots`: add the `case` in the
  gathering `shared.WalkNamed` switch, a new `gathered*` slice, and a
  resolution loop over it in the same function, preserving the gather-then-
  resolve pattern (issue #4920) — never reintroduce a second full-tree walk.

## Failure modes and how to debug

- A forward reference stops resolving: check that its declaration map is
  populated during the gathering walk (not lazily during resolution) — the
  whole point of gather-then-resolve is that every declaration is known
  before any resolution loop runs.
- `TestParseFullTreeWalkCount` (root's `walk_count_test.go`) fails after a
  change here: a new `shared.WalkNamed` call was added inside a resolution
  loop instead of during the single gathering walk, reintroducing a removed
  full-tree re-walk.
- `TestDeadCodeCrossKindSameKeyOrdering` (root's
  `dead_code_gather_resolve_cross_kind_test.go`) fails: a resolution loop's
  order in `CollectRoots` changed. Confirm the reorder is intentional before
  updating that test's expected order.

## Do not change without review

- The single-gathering-walk-then-in-memory-resolution structure of
  `CollectRoots` (issue #4920); do not reintroduce a second or third
  full-tree walk for a new evidence kind.
- The resolution loop order in `CollectRoots` (locked in by root's
  `TestDeadCodeCrossKindSameKeyOrdering`).
- The imported-vs-local interface target distinction in
  `goMarkConcreteTypeForInterfaceTarget` (`flows.go`).
