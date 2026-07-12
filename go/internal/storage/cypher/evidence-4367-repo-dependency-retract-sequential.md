# Evidence: repo-dependency retract runs sequentially; RUNS_ON templates drop the chained path (#4367, #5116 siblings)

## Problems (both measured on the pinned v1.1.11 before the fix)

1. **RUNS_ON wrote and retracted nothing.** The write and retract templates
   traversed `(repo)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(i:WorkloadInstance)`
   as one path pattern. Probed over Bolt HTTP on a lean
   `timothyswt/nornicdb-cpu-bge:v1.1.11` container: the exact write template
   created 0 edges and the exact retract deleted 0, while the same traversal
   split into two single-hop MATCHes sharing the workload variable wrote 1 and
   deleted 1 on the same data. NornicDB v1.1.11 does not match a multi-hop
   path pattern with a direction reversal; production RUNS_ON truth was never
   materialized.
2. **The grouped retract under-applied.** `executeRepoDependencyRetractStatements`
   dispatched its three statements (typed repository relationships, RUNS_ON,
   evidence artifacts) through `ExecuteGroup` — one managed transaction. The
   live regression on v1.1.11 failed with
   `retract: in-scope DEPENDS_ON gone: count = 1, want 0` (the first grouped
   statement never applied — the same deterministic failure #5128 measured on
   the SQL retract). Statement shapes were already safe (single-label
   Repository anchors, relationship-type disjunction); only the grouping was
   broken.

## Fixes

- The RUNS_ON write template and both retract variants split the traversal
  into shared-variable single-hop MATCHes (probed shape).
- The repo-dependency retract always runs its three statements sequentially
  through the existing `executeRepoDependencyRetractStatementsSequential`
  path, each in its own auto-commit transaction, keeping the per-statement
  sanitization and role logging. The grouped branch is removed.

## Benchmark Evidence:

Failing-then-green live regression on the pinned production backend (behavior
change — the old paths returned wrong graph truth):

```bash
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17690 \
go test ./internal/replay/offlinetier/ -run 'TestReducerRepoDependencyEdgeRetractGraphTruth|TestReducerRuntimeEdgeRetractGraphTruth' -count=1
```

- RED 1 (write): `write: in-scope RUNS_ON present: count = 0, want 1` on the
  chained template.
- RED 2 (retract, after the write fix): `retract: in-scope DEPENDS_ON gone:
  count = 1, want 0` on the grouped path.
- GREEN: ok 3/3 runs (~1.1–1.3s package wall) after both fixes. The
  repo-dependency test writes and retracts all six typed repository
  relationships, RUNS_ON, and the evidence-artifact family
  (HAS_DEPLOYMENT_EVIDENCE, EVIDENCES_REPOSITORY_RELATIONSHIP,
  TARGETS_ENVIRONMENT with the artifact DETACH-deleted as designed); the
  runtime test covers HANDLES_ROUTE, RUNS_IN, INVOKES_CLOUD_ACTION, and the
  workload DEPENDS_ON. Out-of-scope controls survive and endpoint nodes stay.

Cost shape: three bounded statements in three auto-commit transactions instead
of one grouped transaction — the same fixed-fan-out class as the code-call,
inheritance, and SQL retract fixes. The prior grouped path bought its single
transaction by not deleting the first statement's edges, and the prior RUNS_ON
templates did no work at all, so there is no correct faster baseline to
regress against.

## Observability Evidence:

No-Observability-Change for signal names. The sequential path keeps the
existing per-statement `shared edge retract statement completed` log with
statement_role, repo_count, and duration_seconds fields, statement
sanitization, and `WrapRetryableNeo4jError` classification; the grouped-mode
log line no longer fires because the grouped mode is gone. No metric name,
span, queue stage, worker knob, or status field changes.
