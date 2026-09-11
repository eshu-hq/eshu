# internal/reducer/code/value

## Purpose

Solves the cross-repo value-flow fixpoint that produces the
`reducer/code-interproc-fixpoint` `TAINT_FLOWS_TO` evidence source: a
distinct, generation-independent evidence stream kept separate from the
direct `code_interproc_evidence` rows `taint` materializes per
generation (issue #6061).

`FixpointEvidenceLoader` composes durable function summaries, param
sources, the `FunctionID`->graph-uid map, and graph-backed cloud sink
targets into an `interproc.Program`, solves it (optionally through a durable
component store so a restart or second replica reuses unchanged weak
components), and resolves finding endpoints through the graph-uid map.
`FixpointEvidenceProjector` then retracts and rewrites the full
fixpoint-owned evidence source, using a separate uid namespace
(`taint.ExtractInterprocFixpointEvidenceRows`) so a fixpoint-solved
edge can never collide with a direct-fact edge in the graph writer's
`MERGE`.

## Ownership boundary

**Owns:** the value-flow fixpoint solver and its snapshot/durable-restart
path (`FixpointCache`, `SolveSnapshotIncrementalDurable`),
Program assembly from active CALLS/summaries/sources
(`BuildProgram`, `ProgramAssemblyRunner`), the
evidence-loading/projection pair (`FixpointEvidenceLoader`/
`FixpointEvidenceProjector`), and the graph-backed cloud sink target
loader (`GraphCloudSinkTargetLoader`).

**Does not own:** the generation-scoped stale-evidence sweep in the child
package `cleanup/` (`code/value/cleanup/runner.go`). It only reaches
`taint`'s writer/ledger surface and does not import this package.

**Owns but does not use:** `BackfillStateMarker` (`backfill_state_marker.go`),
moved here from the reducer root under #6609. Its only caller is the root's
`projected_source_edge_backfill` family, which names it through the
`CodeValueFlowBackfillStateMarker` alias in `compat_projection.go`.
Also does not own `InterprocEvidenceHandler` or the
direct (non-fixpoint) `code_interproc_evidence`/`code_taint_evidence`
handlers, ports, or ledgers — those are `taint`.

## Exported surface

| symbol | what it is |
|---|---|
| `FixpointEvidenceLoader` / `FixpointEvidenceProjector` | compose durable summaries/sources/graph-ids/cloud-sinks into a solved Program, then retract+rewrite the fixpoint evidence source |
| `FixpointProjectionResult` | the projector's outcome (finding/graph-row/unresolved-endpoint counts); `code/function/summary`'s `MaterializationHandler` names it through its own `ValueFlowFixpointProjector` interface |
| `FunctionSummarySnapshotLoader` / `FunctionSourceSnapshotLoader` / `FunctionGraphIDSnapshotLoader` / `FunctionCloudSinkTargetLoader` | the loader's four input ports |
| `FixpointCache` / `NewFixpointCache` / `FixpointCacheStats` | the in-process weak-component cache and its stats |
| `FixpointComponentStore` | the durable component-cache store port (Postgres-backed in production) |
| `SolveProgramIncremental` / `SolveProgramIncrementalDurable` / `SolveSnapshotIncrementalDurable` | the three solve entry points (in-memory only, in-memory+durable store, and durable-snapshot-partitioned) |
| `BuildProgram` / `ProgramInput` / `CallEdge` / `ProgramAssemblyStats` | pure Program assembly from active CALLS + persisted summaries |
| `ProgramInputLoader` / `ProgramAssemblyRunner` / `ProgramAssemblyRunnerConfig` / `ProgramAssemblyResult` | a bounded batch-loader runner over `BuildProgram`, not yet wired into `cmd/reducer`'s production path |
| `GraphCloudSinkTargetLoader` / `CloudSinkTargetsCypher` / `CloudSinkTarget` | the graph-backed cloud sink target loader and its pinned Cypher (backend-conformance corpus asserts on it by equality) |
| `GraphQueryRunner` | locally-declared port (see Dependencies) |

The reducer root wires `FixpointEvidenceProjector` (through the root spelling
`ValueFlowFixpointEvidenceProjector`) in `cmd/reducer/value_flow_wiring.go`
(`newValueFlowFixpointProjector`), and `code/function/summary`'s
`MaterializationHandler` (`code/function/summary/handler.go`) calls it
through that package's own `ValueFlowFixpointProjector` interface after
summaries, sources, and graph ids are durably persisted, so graph projection
cannot race ahead of that write. `internal/storage/postgres/value_flow_program_loader.go`
and `code_interproc_evidence_loader.go` construct the concrete durable
loaders/component store this package's types compose.

## Dependencies

`internal/parser/interproc` (`Program`/`Result`/`Source`/`Sink`/`Port`, the
fixpoint solver itself), `internal/parser/summary` (`FunctionID`/`Effects`),
`internal/parser/valueflow` (`BuildProgram`, used by the durable-snapshot
solve path — note this is a *different* package also named `valueflow`;
Go's own-package name is never an identifier inside itself, so importing it
here is unambiguous), `internal/exposure` (`SinkSpec`/`MatchSink` for cloud
sink target matching), `internal/cpubudget`, `internal/reducer/code/taint`
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
`code/taint/graph_ports.go` for the identical precedent.

## Telemetry

No dedicated metric instrument. `FixpointEvidenceLoader.LoadCodeInterprocEvidence`
emits one structured log, `"value-flow fixpoint evidence loaded"`, with
`scope_id`, `generation_id`, `summary_count`, `source_count`,
`cloud_sink_count`, `finding_count`, `overflow_count`,
`fixpoint_component_count`, `fixpoint_assembled_components`,
`fixpoint_recomputed_components`, `fixpoint_reused_components`,
`fixpoint_durable_reused_components`, and `unresolved_endpoint_count`.
`ProgramAssemblyRunner.ProcessOnce` emits `"value-flow program
assembly completed"` with `input_count`, `summary_count`,
`call_edge_count`, `program_edge_count`, `source_count`, `sink_count`,
`skipped_missing_identity`, `skipped_missing_summary`,
`skipped_unconfirmed_call_flow`, and `duration_seconds`. Both are logged
only when a `Logger` is wired and (for the assembly runner) only when at
least one input was processed. The projector's graph write/retract calls go
through `taint`'s writer (`internal/storage/cypher.CodeInterprocEvidenceWriter`,
wired by `cmd/reducer`'s `canonical_graph_writers.go`), which dispatches
through the shared `InstrumentedExecutor` every canonical/reducer-owned
Neo4j writer uses (`observed_service_wiring.go`). That records
`eshu_dp_neo4j_query_duration_seconds` (histogram, `operation=write` for
`Execute` or `write_group` for `ExecuteGroup`), `eshu_dp_neo4j_batch_size`
and `eshu_dp_neo4j_batches_executed_total` per UNWIND batch, and a
`neo4j.execute`/`neo4j.execute_group` span — not
`eshu_dp_postgres_query_duration_seconds`, which is a Postgres-only
histogram (`internal/telemetry/instruments.go:3930-3937`) unrelated to
graph writes. Verified against `go/internal/telemetry/instruments.go` (no
`value_flow`/`fixpoint`/`cloud_sink`-named instrument exists there).

## Gotchas / invariants

- **`GraphQueryRunner` is intentionally re-declared here, not imported.**
  Do not "fix" this by importing the reducer root — see Dependencies above.
- **The fixpoint uid namespace must stay separate from the direct
  `code_interproc_evidence` namespace.** `FixpointEvidenceProjector`
  calls `taint.ExtractInterprocFixpointEvidenceRows`, not
  `ExtractInterprocEvidenceRows`; unifying them would let a
  fixpoint-solved edge collide with (and silently overwrite) a direct-fact
  edge in the graph writer's `MERGE`.
- **The projector retracts the whole fixpoint evidence source, not a scoped
  slice.** The solve reads global durable summary/source state, so
  `ProjectValueFlowFixpointEvidence` retracts by evidence source (or, when a
  `Ledger` is wired, by the ledger's enumerated source uids) rather than a
  triggering scope's last-stamped rows — see the doc comment on
  `ProjectValueFlowFixpointEvidence`. This method name keeps its
  `ValueFlow` infix on purpose: `code/function/summary`'s
  `ValueFlowFixpointProjector` interface requires it verbatim, so renaming it
  here would break that structural-typing contract.
- **The ledger record must happen before the graph write**, when a `Ledger`
  is wired, mirroring `taint`'s own invariant (issue #4893).
- **`ProgramAssemblyRunner` is not production-wired.** It exists as
  a bounded batch-loader driver over `BuildProgram` but nothing in
  `cmd/reducer` constructs one yet; do not assume it runs in production
  without checking the wiring first.
- **`LoadValueFlowFixpointComponents`/`StoreValueFlowFixpointComponents`
  (on `FixpointComponentStore`) also keep their `ValueFlow` infix on
  purpose**, matching `internal/storage/postgres.ValueFlowFixpointComponentStore`'s
  method names (an external, structurally-satisfying implementer this
  package does not own).

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/code/taint/README.md` — the direct (non-fixpoint) sibling this package writes through
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
`internal/backendconformance/corpus_value_flow*.go`, and (at the time of
this move) the reducer root's own `code_function_summary_materialization.go`
(since moved to `code/function/summary/handler.go`) — kept their existing `reducer.` spelling through the value-flow stanza of
`compat_projection.go`'s type aliases and forwarding functions, so none
needed a source change. Measured from `go/`, with `GOROOT` unset and
`GOCACHE` pointed at this worktree: `go build ./...`, `go vet ./...`,
`go test ./internal/reducer/... -count=1` (30 subpackages, this package's
own suite included), `go test ./cmd/reducer ./internal/storage/postgres
./internal/query -count=1`, and `go test ./internal/backendconformance
./internal/replay/costcounting ./internal/projector/... -count=1` each
exited 0 on the branch. `git diff --check` exited 0. Binary output was not
compared and no such claim is made here.

**Package-clause rename addendum (same issue #6061):** the package clause
changed from `valueflow` to `value`, and every exported `ValueFlow*`
identifier dropped that prefix (for example `ValueFlowFixpointCache` ->
`FixpointCache`, `BuildValueFlowProgram` -> `BuildProgram`), except the two
noted under Gotchas above that keep it to satisfy an external structural
contract (`code/function/summary`'s `ValueFlowFixpointProjector` interface
and `postgres.ValueFlowFixpointComponentStore`'s method names). The reducer
root keeps every one of the prior `ValueFlow*` spellings through the
value-flow stanza of `compat_projection.go`, so no root caller needed a
source change; the two direct importers (`code/function/summary` and
`compat_projection.go` itself) were updated to the new names and dropped
their now-needless `valueflow` import alias. Wire strings, domains, entity
keys, evidence sources, and telemetry names are unchanged. Measured from
`go/`, with `GOROOT` unset: `go build ./...`, `go vet ./internal/reducer/...
./cmd/reducer/... ./internal/storage/...`, and `go test ./internal/reducer/...
./cmd/reducer/... ./internal/storage/cypher/... ./internal/replay/...
-count=1` all exited 0 on this branch. `git diff --check` exited 0.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The structured-log messages and fields
listed under Telemetry above are unchanged; only the package that owns the
code moved.
