# Kubernetes Node Writers

## Purpose

This package materializes `kubernetes_live` facts into canonical graph nodes:
`NamespaceNodeWriter` writes `KubernetesNamespace` nodes (binding an
`Environment` node only for environment-bound rows) and retracts stale
`TARGETS_ENVIRONMENT` edges and absent nodes; `WorkloadNodeWriter` writes
`KubernetesWorkload` nodes. Both write through the backend-neutral
`Executor` seam with batched `UNWIND` statements.

## Ownership boundary

The leaf owns the writer types, the Cypher templates, row shaping, batching,
and dispatch order. The parent `cypher` package owns the shared contracts
(`Executor`, `GroupExecutor`, `Statement`, operation/metadata keys,
`DefaultBatchSize`, `BuildBatchedStatements`, retry wrapping).

## Exported surface

- `NewNamespaceNodeWriter` / `NamespaceNodeWriter` — namespace upserts,
  stale-edge retract, absent-node sweep
- `NewWorkloadNodeWriter` / `WorkloadNodeWriter` — workload upserts
- `CanonicalKubernetesNamespaceUpsertCypher`,
  `CanonicalKubernetesNamespaceWithEnvironmentUpsertCypher`,
  `RetractKubernetesNamespaceStaleTargetsEnvironmentCypher` — templates the
  edge/materialized sibling reads through

See `doc.go` for the package contract.

## Dependencies

Parent `cypher` package only (contracts listed above). The parent never
imports this leaf; callers migrate directly with no root facade.

## Telemetry

None. The writers dispatch through the existing `Executor` instrumentation;
statement metadata keys are unchanged.

## Gotchas / invariants

- An unbound namespace row (empty `environment`) must never reach the
  with-environment template: no `Environment` node, no
  `TARGETS_ENVIRONMENT` edge (#5434).
- The stale-edge retract dispatches through sequential `Execute`, never
  `ExecuteGroup` (NornicDB managed-transaction DELETE under-applies).
- `MERGE` is on the stable `uid` identity only; mutable properties are
  `SET` separately so retries converge.
- `buildBatchedRetractStatements` duplicates the parent helper (flux-seam
  precedent): unexported helpers cannot cross the package split, and the
  root copy stays for the canonical retract path.

## No-Regression Evidence

No-Regression Evidence: the move changes no statement text, parameter shape,
row cloning, batching, or dispatch order — `docs/internal/evidence/6694-kubernetes-node-writers.md`
pins each one with the focused suites below.

`go test ./internal/storage/cypher/kubernetes -count=1` (moved writer,
retract, order, group-dispatch, and graph-truth tests) plus the repointed
reducer, ifa, queryplan, and replay suites prove the leaf emits identical
statements to the pre-move root writers on the same inputs.

No-Observability-Change: dispatch, `GroupExecutor` atomicity, statement
metadata, and retry wrapping are unchanged; this package adds no metric
name, metric label, worker, queue domain, runtime knob, backend branch, or
new graph-write route.

## Related docs

- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/nornicdb-pitfalls.md`
- `go/internal/storage/cypher/README.md`
