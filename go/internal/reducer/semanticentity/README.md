# semanticentity

Turns parser-emitted `content_entity` facts into canonical semantic-entity
graph nodes (Annotation, Typedef, TypeAlias, TypeAnnotation, Component,
Module, ImplBlock, Protocol, ProtocolImplementation, and the per-language
Variable/Function subsets that qualify) and writes them through the graph
backend.

This package moved out of the flat `internal/reducer` root under issue #6061.
It is a domain family: it owns one handler and the extraction pipeline behind
it, and nothing else in the reducer depends on its internals.

## Purpose

`semanticentity` is the reducer-side half of semantic-entity materialization:
it decides which `content_entity` facts are semantic entities, shapes them
into canonical rows, and drives the canonical write (and delta-scoped
retract) through a `SemanticEntityWriter`. The canonical Cypher-backed writer
that satisfies that interface lives in `internal/storage/cypher`
(`semantic_entity.go`), not here.

## Ownership boundary

This package owns:

- deciding whether a `content_entity` fact qualifies as a semantic entity
  (`isSemanticEntityType` and its per-language helpers in
  `materialization_helpers.go`);
- shaping qualifying facts into canonical `SemanticEntityRow` values in a
  deterministic sort order;
- delta-scoping the write/retract to the changed and deleted files a delta
  generation reports (`delta_scope.go`);
- publishing the `semantic_nodes_committed` graph-projection phase after a
  successful write, with a durable repair enqueue on publish failure.

It does not own the canonical graph write itself (the `SemanticEntityWriter`
implementation in `internal/storage/cypher`) or the reducer's queue/worker
machinery that claims and retries the `semantic_entity_materialization`
domain.

## Exported surface

| symbol | file | what it does |
|---|---|---|
| `SemanticEntityMaterializationHandler` | `materialization.go` | the reducer handler the runtime registers for `semantic_entity_materialization` |
| `SemanticEntityWriter` | `materialization.go` | the canonical graph-write sink the handler writes through |
| `SemanticEntityRow` | `materialization.go` | one canonical semantic-entity row |
| `SemanticEntityWrite` / `SemanticEntityWriteResult` | `materialization.go` | the write request/outcome shape the handler and writer exchange |
| `ExtractSemanticEntityRows` | `materialization.go` | extracts every repo's semantic rows from a generation's facts |
| `ExtractSemanticEntityRowsForRepo` | `materialization.go` | the same extraction, filtered to one repo acceptance unit |
| `GraphProjectionPhaseRepairQueue` / `GraphProjectionPhaseRepair` | `graph_ports.go` | local structural port for the durable repair queue, see below |

See `doc.go` for the godoc-rendered package contract.

## Dependencies

Imports point strictly downward. This package reaches `reducer/contract`
(aliased `reducercontract`), `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `internal/facts` and `pkg/log`, and it never imports
the parent `internal/reducer` package. The dependency runs the other way: the
root's handler catalog (`defaults_domain_catalog.go`) constructs
`SemanticEntityMaterializationHandler` and wires its `FactLoader`, `Writer`,
`PriorGenerationCheck` and `PhasePublisher` fields, plus `RepairQueue` when the
root repair queue is present (`defaults_domain_catalog.go:91-106`).

`GraphProjectionPhaseRepairQueue` and `GraphProjectionPhaseRepair` are
declared locally in `graph_ports.go` rather than imported from the reducer
root: the root's `GraphProjectionPhaseRepairQueue` (`graph_projection_phase_repair.go`)
is still shared production logic for families that have not moved out of
root yet (`workload_materialization_handler.go`,
`graph_projection_phase_repair_runner.go`,
`workload_materialization_repo_phase.go`). Unlike the codetaint ports this
pattern follows, this interface's `Enqueue` method takes a named struct
parameter, and Go requires exact type identity for that, not just a matching
method set — the root's concrete repair queue cannot satisfy this package's
`GraphProjectionPhaseRepairQueue` directly, even though every
`GraphProjectionPhaseRepair` field matches. The root wires it through
`semanticEntityRepairQueueAdapter`
(`internal/reducer/semantic_entity_repair_queue_adapter.go`), which converts
between the two named repair-row types field-by-field; only when
`handlers.GraphProjectionRepairQueue` is non-nil, to avoid handing the
handler a non-nil adapter interface wrapping a nil queue.
`graphProjectionPhaseRepairsFromStates` is a byte-for-byte copy of the root's
`GraphProjectionPhaseRepairsFromStates` body for the same reason
`GraphProjectionPhaseRepair` is declared locally.

A handful of one-line forwarders that lived alongside the family in the
former `semantic_entity_*.go` root files stayed in root instead of moving,
because other root families that have not moved out yet still call them by
their unqualified root spelling: `payloadMap`, `semanticPayloadString`,
`semanticPayloadStringSlice`, `semanticQualifyDeltaPath`,
`semanticDeltaPayloadBool`, `deltaScopeRepositorySet`, and
`applyRepoRefreshDeltaScope` now live in
`internal/reducer/shared_payload_delta_compat.go`. This package calls the
shared-tier functions they forward to directly instead of reaching back into
root for them. In practice that means `payloadcore` only: the two
`sharedintent` forwarders were cross-family helpers that merely lived in the
old `semantic_entity_delta_scope.go`, and this family's own logic never called
them, so `sharedintent` is not among this package's imports.

## Telemetry

This package registers no metric instrument of its own. The
`semantic_entity_materialization` domain runs as a standard reducer
execution covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`, under the `reducer.run` span. The
domain is an attribute on those metrics rather than a span of its own, and
the span carries no domain attribute either, so isolate this family through
the domain-tagged metrics and the structured log below rather than by
filtering traces.

`SemanticEntityMaterializationHandler.Handle` emits one "semantic entity
materialization completed" structured log per execution, carrying
`fact_count`, `repo_count`, `row_count`, `skip_retract`,
`delta_projection`, `delta_file_count`, and the
`load_facts_duration_seconds` / `extract_duration_seconds` /
`retract_decision_duration_seconds` / `graph_write_duration_seconds` /
`phase_publish_duration_seconds` / `total_duration_seconds` per-stage
timings.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Nearly every hunk in the moved production files is
package-clause and import requalification: symbols the reducer root used to
supply as one-line forwarders (`Intent`, `Result`, `FactLoader`,
`GraphProjectionPhasePublisher`, and the fact-kind/loader helpers) are now
imported from the shared-tier leaf that already owned them
(`reducer/contract`, `reducer/factload`, `reducer/gpphase`). The
`GraphProjectionPhaseRepairQueue`/`GraphProjectionPhaseRepair` port and the
`graphProjectionPhaseRepairsFromStates` conversion are declared locally
because the root's versions are still shared with families that have not
moved, with the conversion body copied byte-for-byte; the root wires the two
named types together with the new `semanticEntityRepairQueueAdapter`. The
family's own extraction, delta-scoping and metadata-shaping logic is
unchanged. Verified
locally on this branch: `go build ./internal/reducer/...` and
`go vet ./internal/reducer/semanticentity ./internal/reducer` both exit 0;
`go test ./internal/reducer/semanticentity ./internal/reducer -count=1`
passes.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span,
or log field. This package registers no instrument; the reducer executions
that wrap it, the span over them, and the structured-log fields listed above
are the same before and after the move.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a
  symbol the root defines, that symbol is either already a forwarder to a
  shared-tier package (call the shared-tier function directly) or genuinely
  root-owned logic shared with families that have not moved yet (declare a
  structurally identical local port in `graph_ports.go`, following the
  pattern `codetaint/graph_ports.go` established).
- **`payloadMap`, `semanticPayloadString`, `semanticPayloadStringSlice`,
  `semanticQualifyDeltaPath`, `semanticDeltaPayloadBool`,
  `deltaScopeRepositorySet`, and `applyRepoRefreshDeltaScope` are not here.**
  Their prefix looks like this family, but they are cross-family forwarders
  other root domains still call unqualified; they live in
  `internal/reducer/shared_payload_delta_compat.go`. Do not reintroduce a
  local copy — call the shared-tier function they forward to.
- **`GraphProjectionPhaseRepairQueue` here is narrower than the root's.** It
  declares only `Enqueue`, the one method `SemanticEntityMaterializationHandler`
  calls, not the root's full `Enqueue`/`ListDue`/`Delete`/`MarkFailed` set
  the repair runner needs. Narrowing the method set is not enough on its own:
  a wider implementation satisfies this interface only if its `Enqueue` takes
  `[]semanticentity.GraphProjectionPhaseRepair`. The root's takes the root's
  own struct, and Go requires exact type identity in a method signature, which
  is why `semanticEntityRepairQueueAdapter` exists. Do not delete it.
- **Delta scoping is per repository**, carried on each generation's
  repository fact (`delta_generation`, `delta_relative_paths`,
  `delta_deleted_relative_paths`). Never treat it as scope-wide.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
