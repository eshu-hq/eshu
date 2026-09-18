# AGENTS.md — internal/reducer/workloadinstance

Scoped instructions for this package. The root `AGENTS.md` and `CLAUDE.md`
still apply.

- This is a reducer family package. It may import `reducer/crossscope` and the
  standard library. It must never import the parent `internal/reducer`
  package; the graph port is the local `GraphQueryRunner` interface.
- Keep `existingAnchorsCypher` in lockstep with the USES writer's endpoint
  MATCH in `internal/storage/cypher/workload_cloud_relationship_writer.go`. If
  the two diverge, "ready" stops meaning "the writer will bind" and the gate
  either stalls or lets no-op writes through.
- The bound must stay elapsed-time. `NotReadyFailureClass` is enrolled in
  `nonCountingReducerRetryFailureClasses`, which freezes `attempt_count`.
- Keep `NotReadyFailureClass` declared in the same file as its
  `FailureClass()` method: the go/ast enrollment guard resolves the returned
  constant per file.
