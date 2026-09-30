# #7446 Repository stub counter and #7445 harness cleanup

Refs #7324, #7443, #7445, #7446.

## What happens

After the path-conflict retirement (#7324) deletes a different-id
`Repository`, a later `repo_dependency` or `submodule_pin` write that names the
retired id re-creates it. Both upserts MERGE the target `Repository` by id
(`BatchCanonicalRepoDependencyUpsertCypher` and the typed repository
relationship upserts in `go/internal/storage/cypher/canonical.go` and
`canonical_relationships.go`; `BatchCanonicalSubmodulePinEdgeCypher` in
`canonical_submodule_edges.go`). The new node has no `path`, so the retirement
never matches it again.

The arbiter ruling on #7446 keeps MERGE-by-id: a MATCH-only target would
reintroduce the ordering dependency and the O(in-degree) re-arm the #7324
ruling rejected, and on NornicDB a bare-MATCH UNWIND write silently drops the
row (`docs/public/reference/nornicdb-pitfalls.md`). The stub is truthful while
an owner asserts an edge into it, and reapable after: the owners retract by
`evidence_source` (`RetractRepoDependencyEdgesCypher`,
`RetractSubmodulePinEdgesCypher`) and the `Repository` orphan sweep deletes
it. What was missing was an operator signal, plus test cleanup for the stub.

Root-Cause Evidence: the live test in PR #7443 logged, on Neo4j, under both
executor shapes: `OBSERVED stub recreation after retirement: 1 Repository
node(s) under the retired id repository:wd7285-1790765427325659000-payments-rekeyed:
[map[evidence_source:resolver/cross-repo incoming:[PINS_SUBMODULE DEPENDS_ON]
path:<nil>]]`. No counter or log recorded the creation. The #7445 harness
cleanup matched `n.id STARTS WITH $prefix` (`wd7285-<nonce>`), but the stub id
is `repository:wd7285-<nonce>-...` with no path or uid, so it survived every
run. RED on `origin/main` 9be9bf9c9 with the new assertion:
`repository_path_conflict_live_test.go:244: prefix-scoped Repository nodes
after cleanup = 1, want 0`. The stub left by the `atomic_group` shape was
still present when the `phase_group` shape ran.

## Change

- #7445: the shared live-harness cleanup adds
  `OR n.id STARTS WITH 'repository:' + $prefix`, and the path-conflict test
  asserts no prefix-scoped `Repository` survives cleanup.
- #7446: `eshu_dp_canonical_repository_stubs_created_total{writer}` (closed
  `writer`: `repo_dependency`, `submodule_pin`) counts the backend's
  `NodesCreated` for the MERGE-by-id upserts. The edge writer stashes a
  filtered `WriteCountsCollector` per execution unit (an `ExecuteGroup` call
  or one `Execute`), as the #7324 retirement counter does, and forwards every
  entry to any collector already on the context. It counts only the last
  k entries, k being the unit's stub-capable statements, so a driver retry
  does not double-count. Each non-zero statement logs
  `canonical repository stub created` at INFO with `writer`,
  `evidence_source`, `nodes_created`, and, for a one-row statement,
  `source_repo_id`, `target_repo_id`, `generation_id` with `attributed=true`.
  A multi-row statement lists up to 10 candidate pairs with
  `attributed=false`, because the backend counts per statement, not per row.

## Proof

Live, `neo4j:2026-community` (digest-pinned image), own container on
`127.0.0.1:38697`:

```text
ESHU_NEO4J_URI=bolt://127.0.0.1:38697 ESHU_NEO4J_DATABASE=neo4j ESHU_LIVE_GRAPH_BACKEND=neo4j \
  go test ./internal/reducer -tags live_nornicdb_answer_truth \
  -run 'TestLiveRepositoryPathConflict|TestLiveRepositoryStub' -count=1 -v
--- PASS: TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges (1.31s)
    --- PASS: .../atomic_group (0.64s)
    --- PASS: .../phase_group (0.46s)
--- PASS: TestLiveRepositoryStubReapedAfterOwnerRetracts (0.42s)
```

The stub leg now asserts, per shape: the counter is `repo_dependency=1`,
`submodule_pin=0` (the pin MERGEs the node the dependency already created);
one INFO log attributed to the retired target; exactly one node under the
retired id, `path` null, `evidence_source = resolver/cross-repo`, incoming
types `{DEPENDS_ON, PINS_SUBMODULE}`; one `Repository` at the path;
`QueryRepoDependencies` returns one `DEPENDS_ON` whose `target_id` is the
retired id the graph holds. A write to the live new id leaves the counter
unchanged and still one `Repository` at the path.

RED with the wiring disabled (report returns early):
`eshu_dp_canonical_repository_stubs_created_total repo_dependency=0
submodule_pin=0, want 1 and 0` in both shapes. The lifecycle test passed in
that run too: retract-then-sweep is existing behaviour, and the test is
contract proof, not a regression.

Hermetic (`go/internal/storage/cypher/edge/writer/repository_stubs_test.go`):
per statement shape (atomic group, sequential, both writers), a group
retried three times counting once, a live target counting zero, a
non-reporting executor logging the DEBUG `not counted` line with no counter,
and forwarding to an outer collector. Mutations: disabling the report fails
the counter and log cases; dropping the last-k window reports 3 for the
retried group.

No-Regression Evidence: the writer Cypher is unchanged. The counter reads
counts the Bolt executors already return (`ReportWriteCounts` in
`cmd/reducer/neo4j_wiring.go` runs for every statement today). The added work
per execution unit of a `repo_dependency` or `submodule_pin` write is one
context value, one mutex-guarded slice append per statement, and one pass
over those entries after commit; other domains skip the capture entirely
(`repositoryStubWriter` returns false). No query, lock, or transaction is
added, and nothing is serialized.

Observability Evidence: `eshu_dp_canonical_repository_stubs_created_total`
(closed `writer`: `repo_dependency`, `submodule_pin`; constants in
`go/internal/telemetry/instruments_repository_retirement.go`), the
`canonical repository stub created` INFO log, and the DEBUG
`canonical repository stubs not counted: executor reported no write summary`
line with `stubs_counted=false`. The reducer's Bolt runner reports `NodesCreated`
on Neo4j and NornicDB alike (`neo4jSessionRunner` in `cmd/reducer/neo4j_wiring.go`
calls `ReportWriteCounts` for every backend; #6783 measured `NodesCreated` correct
on both), so the counter emits on both. Only Neo4j has a live proof; the live file
is pinned to Neo4j in `specs/live-tests.v1.yaml`. With differential capture on,
the recorder replaces the collector below the edge writer and the counter stays
at zero with the DEBUG line. Documented in
`docs/public/reference/telemetry/metrics-reducer-storage.md` and the shared
edge row of `docs/public/observability/telemetry-coverage.md`.
