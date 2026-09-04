# internal/reducer/valueflow

## Purpose

Solves the cross-repo value-flow fixpoint that produces the
`reducer/code-interproc-fixpoint` `TAINT_FLOWS_TO` evidence source: a
distinct, generation-independent evidence stream kept separate from the
direct `code_interproc_evidence` rows `codetaint` materializes per
generation (issue #6061).

`ValueFlowFixpointEvidenceLoader` composes durable function summaries, param
sources, the `FunctionID`->graph-uid map, and graph-backed cloud sink
targets into an `interproc.Program`, solves it (optionally through a durable
component store so a restart or second replica reuses unchanged weak
components), and resolves finding endpoints through the graph-uid map.
`ValueFlowFixpointEvidenceProjector` then retracts and rewrites the full
fixpoint-owned evidence source, using a separate uid namespace
(`codetaint.ExtractCodeInterprocFixpointEvidenceRows`) so a fixpoint-solved
edge can never collide with a direct-fact edge in the graph writer's
`MERGE`.

## Ownership boundary

**Owns:** the value-flow fixpoint solver and its snapshot/durable-restart
path (`ValueFlowFixpointCache`, `SolveValueFlowSnapshotIncrementalDurable`),
Program assembly from active CALLS/summaries/sources
(`BuildValueFlowProgram`, `ValueFlowProgramAssemblyRunner`), the
evidence-loading/projection pair (`ValueFlowFixpointEvidenceLoader`/
`ValueFlowFixpointEvidenceProjector`), and the graph-backed cloud sink target
loader (`GraphValueFlowCloudSinkTargetLoader`).

**Does not own:** `code_value_flow_stale_cleanup_runner.go` (reducer root) —
the generation-scoped stale-evidence sweep that only reaches `codetaint`'s
writer/ledger surface and has no dependency on this package. Also does not
own `code_value_flow_backfill_state_marker.go` (reducer root) — despite the
naming overlap, its only real caller is the still-in-root
`projected_source_edge_backfill` family; nothing in this package uses it.
Also does not own `CodeInterprocEvidenceMaterializationHandler` or the
direct (non-fixpoint) `code_interproc_evidence`/`code_taint_evidence`
handlers, ports, or ledgers — those are `codetaint`.

## Exported surface

| symbol | what it is |
|---|---|
| `ValueFlowFixpointEvidenceLoader` / `ValueFlowFixpointEvidenceProjector` | compose durable summaries/sources/graph-ids/cloud-sinks into a solved Program, then retract+rewrite the fixpoint evidence source |
| `ValueFlowFixpointProjectionResult` | the projector's outcome (finding/graph-row/unresolved-endpoint counts); reducer root's `CodeFunctionSummaryMaterializationHandler` names it through its own `ValueFlowFixpointProjector` interface |
| `FunctionSummarySnapshotLoader` / `FunctionSourceSnapshotLoader` / `FunctionGraphIDSnapshotLoader` / `FunctionCloudSinkTargetLoader` | the loader's four input ports |
| `ValueFlowFixpointCache` / `NewValueFlowFixpointCache` / `ValueFlowFixpointCacheStats` | the in-process weak-component cache and its stats |
| `ValueFlowFixpointComponentStore` | the durable component-cache store port (Postgres-backed in production) |
| `SolveValueFlowProgramIncremental` / `SolveValueFlowProgramIncrementalDurable` / `SolveValueFlowSnapshotIncrementalDurable` | the three solve entry points (in-memory only, in-memory+durable store, and durable-snapshot-partitioned) |
| `BuildValueFlowProgram` / `ValueFlowProgramInput` / `ValueFlowCallEdge` / `ValueFlowProgramAssemblyStats` | pure Program assembly from active CALLS + persisted summaries |
| `ValueFlowProgramInputLoader` / `ValueFlowProgramAssemblyRunner` / `ValueFlowProgramAssemblyRunnerConfig` / `ValueFlowProgramAssemblyResult` | a bounded batch-loader runner over `BuildValueFlowProgram`, not yet wired into `cmd/reducer`'s production path |
| `GraphValueFlowCloudSinkTargetLoader` / `ValueFlowCloudSinkTargetsCypher` / `ValueFlowCloudSinkTarget` | the graph-backed cloud sink target loader and its pinned Cypher (backend-conformance corpus asserts on it by equality) |
| `GraphQueryRunner` | locally-declared port (see Dependencies) |

The reducer root wires `ValueFlowFixpointEvidenceProjector` in
`cmd/reducer/value_flow_wiring.go` (`newValueFlowFixpointProjector`), and
`CodeFunctionSummaryMaterializationHandler`
(`code_function_summary_materialization.go`) calls it through the root's own
`ValueFlowFixpointProjector` interface after summaries, sources, and graph
ids are durably persisted, so graph projection cannot race ahead of that
write. `internal/storage/postgres/value_flow_program_loader.go` and
`code_interproc_evidence_loader.go` construct the concrete durable
loaders/component store this package's types compose.

## Dependencies

`internal/parser/interproc` (`Program`/`Result`/`Source`/`Sink`/`Port`, the
fixpoint solver itself), `internal/parser/summary` (`FunctionID`/`Effects`),
`internal/parser/valueflow` (`BuildProgram`, used by the durable-snapshot
solve path — note this is a *different* package also named `valueflow`;
Go's own-package name is never an identifier inside itself, so importing it
here is unambiguous), `internal/exposure` (`SinkSpec`/`MatchSink` for cloud
sink target matching), `internal/cpubudget`, `internal/reducer/codetaint`
(the direct evidence writer/ledger/uid-namespace surface the projector
writes through), and `internal/reducer/payloadcore` (`AnyToString`). No
dependency on the reducer root, and none of the root's other family
subpackages.

One root-owned interface this package's cloud sink loader needs
(`GraphQueryRunner`, the graph read port) is **locally redeclared** in
`graph_ports.go` rather than imported: it is shared by several other
families still in the reducer root, so it is not this package's to own, and
importing the root to reach it would violate the "a family never imports the
reducer root" rule. Go interfaces are satisfied structurally, so the same
concrete implementation `cmd/reducer` wires into root's other families also
satisfies this local declaration with no logic duplicated — see
`codetaint/graph_ports.go` for the identical precedent.

## Telemetry

No dedicated metric instrument. `ValueFlowFixpointEvidenceLoader.LoadCodeInterprocEvidence`
emits one structured log, `"value-flow fixpoint evidence loaded"`, with
`scope_id`, `generation_id`, `summary_count`, `source_count`,
`cloud_sink_count`, `finding_count`, `overflow_count`,
`fixpoint_component_count`, `fixpoint_assembled_components`,
`fixpoint_recomputed_components`, `fixpoint_reused_components`,
`fixpoint_durable_reused_components`, and `unresolved_endpoint_count`.
`ValueFlowProgramAssemblyRunner.ProcessOnce` emits `"value-flow program
assembly completed"` with `input_count`, `summary_count`,
`call_edge_count`, `program_edge_count`, `source_count`, `sink_count`,
`skipped_missing_identity`, `skipped_missing_summary`,
`skipped_unconfirmed_call_flow`, and `duration_seconds`. Both are logged
only when a `Logger` is wired and (for the assembly runner) only when at
least one input was processed. The projector's graph write/retract calls go
through `codetaint`'s writer, so they carry `codetaint`'s
`eshu_dp_postgres_query_duration_seconds` instrumentation, not a metric of
this package's own. Verified against
`go/internal/telemetry/instruments.go` (no `value_flow`/`fixpoint`/
`cloud_sink`-named instrument exists there).

## Gotchas / invariants

- **`GraphQueryRunner` is intentionally re-declared here, not imported.**
  Do not "fix" this by importing the reducer root — see Dependencies above.
- **The fixpoint uid namespace must stay separate from the direct
  `code_interproc_evidence` namespace.** `ValueFlowFixpointEvidenceProjector`
  calls `codetaint.ExtractCodeInterprocFixpointEvidenceRows`, not
  `ExtractCodeInterprocEvidenceRows`; unifying them would let a
  fixpoint-solved edge collide with (and silently overwrite) a direct-fact
  edge in the graph writer's `MERGE`.
- **The projector retracts the whole fixpoint evidence source, not a scoped
  slice.** The solve reads global durable summary/source state, so
  `ProjectValueFlowFixpointEvidence` retracts by evidence source (or, when a
  `Ledger` is wired, by the ledger's enumerated source uids) rather than a
  triggering scope's last-stamped rows — see the doc comment on
  `ProjectValueFlowFixpointEvidence`.
- **The ledger record must happen before the graph write**, when a `Ledger`
  is wired, mirroring `codetaint`'s own invariant (issue #4893).
- **`ValueFlowProgramAssemblyRunner` is not production-wired.** It exists as
  a bounded batch-loader driver over `BuildValueFlowProgram` but nothing in
  `cmd/reducer` constructs one yet; do not assume it runs in production
  without checking the wiring first.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/codetaint/README.md` — the direct (non-fixpoint) sibling this package writes through
- `docs/internal/design/package-restructure.md` — the #6061 restructure
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for `code_interproc_evidence`

No-Regression Evidence: #6061 moves the value-flow fixpoint cache, snapshot
solve, Program assembly, evidence loader/projector, and graph-backed cloud
sink loader out of the reducer root into this new package, without changing
any field, exported behavior, or call order. The one root-owned interface
this package needs (`GraphQueryRunner`) is locally redeclared with the
identical method set rather than imported, so every existing concrete
implementation still satisfies it with no new indirection.
`anyToString` (a root forwarder to `payloadcore.AnyToString`) was replaced
with a direct `payloadcore.AnyToString` call, since the forwarder itself
does not move with this family. Root-side callers —
`cmd/reducer/value_flow_wiring.go`, `internal/storage/postgres/value_flow_program_loader.go`,
`internal/storage/postgres/code_interproc_evidence_loader.go`,
`internal/backendconformance/corpus_value_flow*.go`, and the reducer root's
own `code_function_summary_materialization.go` — keep their existing
`reducer.` spelling through `value_flow_compat.go`'s type aliases and
forwarding functions, so none needed a source change. Measured from `go/`,
with `GOROOT` unset and `GOCACHE` pointed at this worktree: `go build ./...`,
`go vet ./...`, `go test ./internal/reducer/... -count=1` (30 subpackages,
this package's own suite included), `go test ./cmd/reducer
./internal/storage/postgres ./internal/query -count=1`, and `go test
./internal/backendconformance ./internal/replay/costcounting
./internal/projector/... -count=1` each exited 0 on the branch. `git diff
--check` exited 0. Binary output was not compared and no such claim is made
here.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The structured-log messages and fields
listed under Telemetry above are unchanged; only the package that owns the
code moved.
