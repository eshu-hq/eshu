# Cloud GCP relationship package guidance

## Ownership

- This package owns the GCP cloud-resource relationship edge writer and
  its retract dispatch.
- The parent `cypher` package owns the shared `Executor`, `Statement`,
  `Operation`, `DefaultBatchSize`, and retry classification. This leaf
  imports the parent as `sourcecypher` for those contracts only; the parent
  never imports this leaf.
- `go/cmd/reducer` constructs this writer directly.

## Invariants

- Keep every Cypher template byte-identical to the reviewed move. Cypher
  edits require the graph-truth and performance evidence in the root agent
  guidance.
- `recordingExecutor` in `*_test.go` is a leaf-local copy of the root test
  fake; keep the copy behavior-identical.
- Row helpers (`CloneRowWith`, `ValidateStaticGraphToken`, `ChunkStrings`)
  and the posture-existence filter (`FilterRowsToExistingCloudResourceUIDs`)
  are shared root contracts imported as `sourcecypher`; this leaf defines
  none of its own.
- Preserve parameterized UNWIND/MATCH/MERGE templates and their
  relationship identity. Keep the `WrapRetryableNeo4jError` wrapping so
  transient failures stay queue-retryable.
