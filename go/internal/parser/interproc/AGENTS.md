# AGENTS.md - internal/parser/interproc guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: port-graph model, kind-set rule, partitioning
3. `interproc.go` - the `Program`/`Port`/`Finding` types and finding ordering
4. `solve.go` - `Solve`, `SolvePartitioned`, the `solveAll` fixpoint, kind-set
   merge
5. `partition.go` - weakly-connected-component partitioning (union-find)
6. `solve_test.go` - cross-repo, closure, field-flow, kind-set sanitizer, cloud
   sink, partition-equals-serial (run with -race), and overflow proofs

## Invariants this package enforces

- Language neutral: standard library only; no parser, storage, graph, or
  telemetry imports. A lowering and the correlation engine supply the Program.
- Closures and object fields are named-slot ports; the engine handles them like
  any value position. Closing the false-negative classes is a lowering concern.
- Kind-set sanitizer model with intersection at merges: a sink fires unless its
  kind is neutralized on every path reaching it.
- `SolvePartitioned` must always equal `Solve`. Both call the same pure
  `solveAll`; the only difference is which sub-graph each call sees.
- Partition by weakly-connected component (the conflict key). Taint never crosses
  a component, so concurrent per-component solving is correct and race-free.
- Deterministic, bounded output: sorted sources, sorted findings, counted
  overflow.

## Common changes and how to scope them

- Change taint semantics: add a case to `solve_test.go` first (it drives `Solve`
  with hand-built Programs), then edit `solveAll`/`mergeState`.
- Add a flow class (e.g. global variables): model it as a port/edge shape in the
  lowering; the engine needs no change. Add a fixture proving it.
- Tune partition granularity: edit `partition.go`; keep
  `TestPartitionedEqualsSerial` (and its -race run) green.

## Failure modes and how to debug

- `SolvePartitioned` != `Solve`: a component boundary split a real flow, or the
  cap/sort differs. Both must sort then cap identically (`capFindings`).
- Missing cross-repo/closure/field finding: the lowering did not emit the port or
  edge. Dump the Program's edges; the engine only follows edges it is given.
- Wrong-kind sanitizer suppresses a sink: the kind check in `solveAll`'s sink
  loop is testing membership against the wrong set. Verify against
  `st.neutralized`.
- Data race under -race: shared state across goroutines. `solveAll` must touch
  only its argument and local maps; never a shared slice index from two
  goroutines.

## Do not change without review

- The weakly-connected-component partition correctness (it is what makes
  concurrent solving safe) or the kind-set intersection-at-merge rule.
