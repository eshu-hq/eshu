# Interprocedural Cross-Repo Taint

## Purpose

`interproc` solves interprocedural, cross-repo taint as reachability over a port
graph and reports source-to-sink paths. It is the final layer of the value-flow
taint engine (epic #2705): it composes function summaries across call edges,
crosses repository boundaries, and closes the closure and field-flow
false-negative classes that summary-only engines miss. A sink may be a correlated
cloud fact, terminating a code-to-cloud reachability path.

## Ownership boundary

This package owns the port-graph model, the taint fixpoint with the kind-set
sanitizer rule, and the weakly-connected-component partitioning that makes the
solver concurrent and race-free. It does NOT build the port graph from source — a
language analysis and the correlation engine produce ports, edges, sources,
sinks, and cloud-sink flags and feed them as a `Program`. It does not persist
anything or decide canonical truth; findings are evidence with confidence and
provenance.

## Exported surface

See `doc.go` for the godoc contract. The surface is:

- `Program`, `Port`, `Slot`/`SlotKind`, `Edge`, `Source`, `Sink`, `Sanitizer` —
  the graph model. `Sink.Cloud` marks a code-to-cloud terminal.
- `Solve(program, limits) Result` — the serial solver.
- `SolvePartitioned(program, limits) Result` — the concurrent, per-component
  solver, identical in result to `Solve`.
- `Finding`, `Result`, `Limits`, `DefaultLimits` — the bounded output.

## Dependencies

Standard library only (`runtime`, `sort`, `sync`). No storage, graph backend, or
telemetry dependencies.

## Telemetry

None. The solver is pure; a reducer that drives it owns metrics and spans.

## Performance

No-Regression Evidence: this is a new, self-contained package with no runtime
callers on the hot path yet — pure in-memory computation with no graph backend,
database, or network I/O, so there is no existing path to regress. Baseline and
after are therefore identical; backend/version is N/A (no backend); the input
shape is one in-memory port graph per parse and there are no queue or row counts
(nothing is persisted). `Solve` is a single monotone worklist over the port
graph. `SolvePartitioned` splits the program into weakly-connected components
(the conflict key) and solves them concurrently, bounded by GOMAXPROCS; it is a
pure function of disjoint sub-programs, proven equal to the serial `Solve` and
data-race free under `go test -race` (`TestPartitionedEqualsSerial`,
`TestPartitionedEqualsSerialAtCap`). The change is safe because findings are
bounded with counted overflow and concurrency touches only per-component local
state with no shared mutation.

No-Observability-Change: the package emits no metrics, spans, logs, or status;
a future reducer that drives it owns telemetry. Overflow is surfaced as data on
the `Result` for the caller to record.

## Gotchas / invariants

- **Closures and fields are named-slot ports.** Closing the two false-negative
  classes is a lowering concern: emit a named-slot port and the edges into and
  out of it. The engine treats them like any value position.
- **Kind-set sanitizers, intersected at merges.** A sink fires unless its kind is
  neutralized on every path. A sanitizer port neutralizes kinds for the value
  from that port onward.
- **Partition is by weakly-connected component.** Taint cannot cross a component,
  so per-component solving is correct and parallelizable. `SolvePartitioned` must
  always equal `Solve`; the partitioned path is exercised under `-race`.
- **Determinism.** Sources are seeded in sorted order for stable origin
  attribution; findings are sorted; output is capped with counted overflow.
- **Confidence is fixed (0.6)** for interprocedural findings — lower than
  intraprocedural because it composes summaries across calls.

## Related docs

- Epic #2705 (value-flow taint engine), child issue #2730 (this pass).
- Consumes function summaries (#2729) and the intraprocedural taint model
  (#2728); the cloud sink joins code-to-cloud reachability (epic #2710).
