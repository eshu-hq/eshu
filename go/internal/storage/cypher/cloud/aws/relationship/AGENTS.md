# Cloud AWS workload-relationship package guidance

## Ownership

- This package owns the workload-to-cloud relationship edge writer and its
  retract dispatch.
- The parent `cypher` package owns the shared `Executor`, `Statement`,
  `Operation`, `DefaultBatchSize`, and retry classification. This leaf
  imports the parent as `sourcecypher` for those contracts only; the parent
  never imports this leaf.
- The `edge/materialized` registry reads this leaf's exported retract and
  identity constants directly, never a copy of their text.
- `go/cmd/reducer` constructs this writer directly.

## Invariants

- Keep every Cypher template byte-identical to the reviewed move. Cypher
  edits require the graph-truth and performance evidence in the root agent
  guidance.
- `recordingExecutor` and `dispatchRouteRecorder` in `*_test.go` are
  leaf-local copies of the root test fakes. Test fakes cannot be imported
  across the package split, so each side carries its own copy; keep the
  copies behavior-identical.
- Row helpers (`CloneRowWith`, `ValidateStaticGraphToken`, `ChunkStrings`)
  and the posture-existence filter (`FilterRowsToExistingCloudResourceUIDs`)
  are shared root contracts imported as `sourcecypher`; this leaf defines
  none of its own.
- Preserve parameterized UNWIND/MATCH/MERGE templates and their
  relationship identity. Keep the `WrapRetryableNeo4jError` wrapping so
  transient failures stay queue-retryable.
