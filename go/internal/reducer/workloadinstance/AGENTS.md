# AGENTS.md — internal/reducer/workloadinstance

Scoped instructions for this package. The root `AGENTS.md`
still applies.

- This is a reducer family package. It may import `reducer/contract`,
  `reducer/crossscope`, `go/pkg/log`, and the standard library. It must never import the parent `internal/reducer`
  package; the graph port is the local `GraphQueryRunner` interface.
- Keep `existingAnchorsCypher` in lockstep with the USES writer's endpoint
  MATCH in `internal/storage/cypher/workload_cloud_relationship_writer.go`. If
  the two diverge, "ready" stops meaning "the writer will bind" and the gate
  either stalls or lets no-op writes through.
- The bound must stay elapsed-time on the ledger anchor. `NotReadyFailureClass`
  is enrolled in `nonCountingReducerRetryFailureClasses`, which freezes
  `attempt_count`, and a superseding generation replaces the queue row.
- The handler must commit before returning `NotReadyError` and call
  `Wait.Finish` only after the commit. Ledger keys come from `AnchorKey`;
  change it and `ParseAnchorKey` together.
- Keep `NotReadyFailureClass` declared in the same file as its
  `FailureClass()` method: the go/ast enrollment guard resolves the returned
  constant per file.
