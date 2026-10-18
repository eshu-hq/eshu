# Cloud AWS security-group package guidance

## Ownership

- This package owns the security-group writers: endpoint nodes (CIDR
  blocks, prefix lists, endpoints) and the reachability edge writer with
  its projected-source ledger for UID-anchored retracts.
- The parent `cypher` package owns the shared `Executor`, `Statement`,
  `Operation`, `DefaultBatchSize`, and retry classification. This leaf
  imports the parent as `sourcecypher` for those contracts only; the parent
  never imports this leaf.
- The `edge/materialized` registry reads this leaf's exported retract and
  identity constants directly, never a copy of their text.
- `go/cmd/reducer` constructs these writers directly.

## Invariants

- Keep every Cypher template byte-identical to the reviewed move. Cypher
  edits require the graph-truth and performance evidence in the root agent
  guidance.
- Row helpers (`CloneRowWith`, `ValidateStaticGraphToken`, `ChunkStrings`)
  and the posture-existence filter (`FilterRowsToExistingCloudResourceUIDs`)
  are shared root contracts imported as `sourcecypher`; this leaf defines
  none of its own.
- `recordingExecutor` in `*_test.go` is a leaf-local copy of the root test
  fake; keep the copy behavior-identical.
- Preserve parameterized UNWIND/MATCH/MERGE templates and their
  relationship identity. Keep the `WrapRetryableNeo4jError` wrapping so
  transient failures stay queue-retryable.
