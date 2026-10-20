# Semantic Entity Writer

## Purpose

This package writes reducer semantic rows (Annotation, Typedef, TypeAlias,
TypeAnnotation, Component, Module, ImplBlock, Protocol,
ProtocolImplementation, Variable, semantic Function) into the canonical graph.
`SemanticEntityWriter` retracts stale nodes for the touched repositories and
upserts the current rows, in whole-scope, delta, and per-file narrowed modes.

## Ownership boundary

The leaf owns the writer, the Cypher templates, row shaping, retract
dispatch, and grouped-transaction splitting. The parent `cypher` package owns
the `Executor`/`Statement` seam (`Statement`, `Operation`, statement metadata,
`WrapRetryableNeo4jError`, batching defaults) this package builds on. The
parent never imports this package: root callers (`cmd/reducer` wiring,
backend-conformance corpus) reach the writer through this import path.

## Exported surface

- `SemanticEntityWriter`, `NewSemanticEntityWriter` and the four row-shape
  constructors (`WithParameterizedRows`, `WithBatchedProperties`,
  `WithMergeFirstRows`, `WithCanonicalNodeRows`), `WithLabelScopedRetract`,
  `WithMaxGroupRows`, `WithEntityLabelBatchSize`, `WithGenerationCheck`
  (per-group generation fence, `writer_fence.go`)
- `DefaultMaxGroupRows` — UNWIND row cap per grouped transaction
- `SemanticEntityEvidenceSource` and the `RetractSemanticEntityContainment*`
  templates plus `SemanticEntityContainmentSourceLabels` /
  `SemanticEntityContainmentTargetLabels` /
  `SemanticEntityContainmentUpsertCypher` — the containment family shared with
  the materialized-edge registry

See `doc.go` for the package contract.

## Dependencies

- `internal/storage/cypher` (as `sourcecypher`) — executor and statement seam
- `internal/reducer/code/semantic` — `EntityWrite` / `EntityRow` input contract

## Generation fence (#8628)

`WithGenerationCheck` arms a per-group generation fence on the writer: before
every dispatched group — including the first group and retract-only writes,
on both the grouped and the per-statement fallback path — the writer probes
the write's stamped (`ScopeID`, `GenerationID`) generation. A stale verdict
aborts with a partial `EntityWriteResult` (`CanonicalWrites` counts
dispatched labeled upsert rows only) and the `semantic.ErrGenerationSuperseded`
sentinel; a probe error (including `GenerationNotYetActiveError`) aborts
wrapped fail-closed; a stale verdict on an unstamped write errors fail-closed;
a nil check keeps the legacy unfenced path. Each probe is one read-only
primary-key lookup on `scope_generations`: no new lock, worker, batch, retry,
or Cypher change.

## Telemetry

None new. Writes flow through the existing `Executor` dispatch, statement
summaries, retry wrapping, and failure logs; the oversized-index-key skips
emit the existing `eshu_dp_graph_oversized_index_keys_skipped_total` counter.
The fence itself emits no signal: a fenced abort surfaces as the writer's
error return (existing failure logs), and the handler's `mid_write`
superseded log covers the operator-visible exit.

## Gotchas / invariants

- Preserve every Cypher template byte-identically unless a change explicitly
  carries backend and performance evidence.
- Retracts lead upserts in statement order so retract+upsert commits atomically
  on group-capable executors.
- The retract Cypher is the `WHERE ... IN` shape, not UNWIND-batched
  `MATCH ... DELETE` (which no-ops inside a NornicDB managed transaction).
- `WrapRetryableNeo4jError` wraps every error path so transient failures
  requeue instead of dead-lettering.
- Delta projection requires file paths (fail-closed); retract statements
  carry no row parameters and never force a group split on their own.

## Related docs

- `docs/public/reference/cypher-performance.md`
- `go/internal/storage/cypher/README.md`
