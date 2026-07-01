# Evidence: repository retract bound relationship expansion

Scope: repository dependency retract Cypher used by the reducer shared
projection path. This slice changes the Eshu query shape so each repository id
is resolved through the indexed `Repository {id}` anchor before expanding
repository relationships. It is paired with NornicDB PR #230, which adds the
backend adjacency delete path needed for this shape to avoid graph-wide
relationship expansion.

## Performance evidence

Performance Evidence: baseline current-main full-corpus validation after #4391
showed repository dependency retract cycles spending 298s for 3 intents, 304s
for 3 intents, and 152s for 1 intent while the `repo_dependency` queue still had
about 648-651 pending rows in an 895-repository run. A focused backend probe
against the same NornicDB-backed graph shape showed
`MATCH (source_repo)-[rel]->()` from one bound `Repository` id returning 0 rows
after about 100s, and the repository relationship-type subset returning 0 rows
after about 81s. An exact source-target pair probe timed out after 60s. Those
measurements isolate the long pole to bound-node relationship expansion before
the delete can prove there is no stale relationship.

After this Eshu change, focused query-shape tests prove the emitted Cypher
resolves each repository id first:

```cypher
UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})
MATCH (source_repo)-[rel:DEPENDS_ON|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|USES_MODULE|READS_CONFIG_FROM]->(:Repository)
WHERE rel.evidence_source = $evidence_source
DELETE rel
```

The old `WHERE source_repo.id IN $repo_ids` filter-after-expansion shape is no
longer emitted. The companion NornicDB PR #230 regression seeds 50 unrelated
repositories plus one source/target pair and one `DEPLOYS_FROM` edge, runs the
same bound-source delete shape, deletes exactly 1 relationship, leaves final
`DEPLOYS_FROM` count 0, and asserts no more than 1 outgoing adjacency probe.
The backend/version proof is NornicDB branch `codex/eshu-repo-edge-expansion` at
commit `61e05b41` over base `d7cddad9`; the Eshu proof is this branch's PR head.

Full-corpus terminal runtime is intentionally not claimed by this note. The
remaining acceptance proof is to rerun the 895-repository full-corpus validation
with NornicDB PR #230 or an equivalent backend image present, then record queue
drain, repository dependency retract duration, shell-exec retract duration,
supply-chain impact duration, and search-vector sweep timings on #3624/#3586.

## No-regression evidence

No-Regression Evidence:

```bash
cd go && go test ./internal/storage/cypher -run 'TestBuildRetractRepoDependencyEdgesStatement|TestEdgeWriterRetractEdgesRepoDependencyDispatch|TestEdgeWriterRetractEdgesWorkloadDependencyDispatch' -count=1
```

This proves the repository retract builder and dispatch paths emit the anchored
split shape while preserving workload dependency retract dispatch.
`cd go && go test ./internal/storage/cypher -count=1` proves the full
storage/cypher package remains green after the split. On the backend side,
`go test -tags 'noui nolocalllm' ./pkg/cypher ./pkg/storage -count=1` passes
with the new adjacency regression and existing storage edge tests.

The changed Eshu statements preserve correctness boundaries: relationship
deletes remain scoped by `evidence_source`, repository ids are still supplied by
the reducer intent, target labels remain `Repository`, and the downstream
workload `RUNS_ON` cleanup still runs after the repository relationship retract
through an indexed `Repository {id: repo_id}` anchor. No worker count, batch
size, retry policy, or serialization knob is changed.

## Observability

No-Observability-Change: this is a Cypher shape change inside existing canonical
retract statements. It adds no metric name, span name, structured log key,
status row, queue domain, runtime knob, or backend branch in Eshu. Operators
continue to diagnose the path through the existing reducer projection-cycle
logs, shared-projection backlog and status queries, canonical write statement
summaries, graph query timings, and queue completion counters. The required
post-backend full-corpus proof should compare the same existing fields before
and after: `duration_seconds`, `retract_duration_seconds`, queue pending and
completed counts, graph/backend request duration, and terminal readiness/API
readback.
