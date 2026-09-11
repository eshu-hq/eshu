# internal/reducer/code/function/summary

## Purpose

Persists one generation's durable value-flow function summaries (issue
#6061). `Handler.Handle` loads the raw `code_function_summary`
Effects, recomputes their content versions through a `flow.Store`, and
upserts the resulting snapshot. The upsert is idempotent on `FunctionID`, so
re-running a generation converges rather than duplicating.

When the optional source and graph-id loader/writers are wired, it also
persists that generation's param-level taint sources (`code_function_source`)
and the `FunctionID`->graph-uid map, which the cross-repo value-flow fixpoint
needs alongside the summaries. When the optional fixpoint projector is wired
it runs after those durable writes complete, so graph projection cannot race
ahead of persistence.

## Ownership boundary

**Owns:** function-summary/source/graph-id fact decoding
(`ExtractEffects`,
`ExtractGraphIDs`,
`ExtractSources`), the materialization handler
(`Handler`), and its additive domain definition
(`Definition`).

**Does not own:** the value-flow fixpoint solver and its cache
(`code/value`), which this package's `ValueFlowFixpointProjector` interface
calls after summary/source/graph-id persistence completes.

## Exported surface

| symbol | what it is |
|---|---|
| `Definition` | the additive domain definition for `code_function_summary` |
| `Handler` | the domain handler |
| `Loader` / `Writer` | the required summary fact-loader/durable-snapshot-writer ports |
| `SourceLoader` / `SourceWriter` | the optional param-level taint-source ports |
| `GraphIDLoader` / `GraphIDWriter` | the optional `FunctionID`->graph-uid ports |
| `ValueFlowFixpointProjector` | the optional post-persistence fixpoint-projection port |
| `ExtractEffects` / `ExtractGraphIDs` / `ExtractSources` | the typed-decode extraction seams, each returning its per-fact `factdecode.QuarantinedFact` batch |

The reducer root wires `Definition()` and `Handler` in
`defaults_additive_domains_incident_code.go`, and keeps the
`reducer.CodeFunctionSummary*`/`CodeFunctionSource*`/`CodeFunctionGraphID*`/
`ValueFlowFixpointProjector` spellings through the code-function-summary
stanza of `compat_decode.go`. `cmd/reducer/wiring_handlers.go` constructs the
concrete Postgres-backed loaders/writers this package's interfaces are
satisfied by.

## Dependencies

`internal/facts` (`Envelope`), `internal/parser/interproc` (`Source`,
`Port`, `Slot`), `internal/parser/summary` (aliased `parsed` — `FunctionID`,
`Effects`, `Snapshot`, `Store`, `ParamSink`, `CallArgFlow`; the alias exists
because this package's own name is also `summary`),
`internal/reducer/code/value` (unaliased —
`FixpointProjectionResult`, the fixpoint projector's result type),
`internal/reducer/contract` (`Intent`, `Result`, `DomainDefinition`,
`OwnershipShape`, `DomainCodeFunctionSummary`), `internal/reducer/factdecode`
(`QuarantinedFact`, `PartitionDecodeFailures`, `RecordQuarantinedFacts`,
`InputInvalidSubSignals`), `internal/reducer/payloadcore`
(`DerefStringTrimmed`, `DerefInt`), `internal/reducer/schemadecode`
(`DecodeCodeFunctionSummary`, `DecodeCodeFunctionSource`), and
`internal/telemetry` (`Instruments`). No dependency on the reducer root.

## Telemetry

No dedicated metric instrument. `Handle` emits one structured log,
"code function summary persistence completed", with `scope_id`,
`generation_id`, `repo_id`, `full_snapshot`, `function_count`,
`source_count`, `graph_id_count`, `input_invalid_facts`,
`fixpoint_finding_count`, `fixpoint_graph_rows`, and
`fixpoint_unresolved_endpoint_count`. Per-fact quarantines go through
`factdecode.RecordQuarantinedFacts`, which records the
`ReducerInputInvalidFacts` counter when `Instruments` is wired.

## Gotchas / invariants

- **A full-snapshot intent's repo_id must match every effect's derived repo,
  or `Handle` fails the intent.** `durableFunctionRepo` derives the repo from
  the `\x1f`-delimited `FunctionID` prefix; a mismatch signals a
  misrouted/misscoped intent, not a partial write.
- **The graph-id view's quarantines are discarded, not double-counted.**
  `ExtractGraphIDs` reads the SAME
  `code_function_summary` facts `ExtractEffects`
  already quarantined; `Handle` only records the summary-effects view's
  quarantines on `input_invalid_facts`.
- **The fixpoint projector runs LAST, after every durable write.** Ordering
  it before summary/source/graph-id persistence would let graph projection
  race ahead of the data it reads.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/code/function/README.md` — the `function/` namespace parent
- `go/internal/reducer/code/value/README.md` — the value-flow fixpoint sibling this package's projector calls into
- `docs/internal/design/reducer-target-tree.md` — the #6061 restructure

No-Regression Evidence: #6061 moves function-summary fact decoding,
materialization, and additive-domain registration out of the reducer root
into this new package, without changing any field, exported behavior, wire
string, or call order. `CodeFunctionSummary*`/`CodeFunctionSource*`/
`CodeFunctionGraphID*` exported identifiers dropped their prefix per
`docs/internal/naming.md` (for example `CodeFunctionSummaryLoader` ->
`Loader`, `CodeFunctionSummaryMaterializationHandler` ->
`Handler`); `codeFunctionSummaryDomainDefinition` became the
exported `Definition`. The reducer root keeps every one of the prior
spellings through the code-function-summary stanza of `compat_decode.go`, so
no external caller needed a source change. Measured from `go/`, with
`GOROOT` unset: `go build ./...`, `go vet ./internal/reducer/...
./cmd/reducer/... ./internal/storage/...`, and `go test ./internal/reducer/...
./cmd/reducer/... ./internal/storage/cypher/... ./internal/replay/...
-count=1` all exited 0 on this branch. `git diff --check` exited 0.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph, or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The structured-log message and its fields
listed under Telemetry above are unchanged; only the package that owns the
code moved.
