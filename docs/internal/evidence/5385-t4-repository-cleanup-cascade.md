# #5385 T4: repository-cleanup retirement cascade (performance and observability note)

T4 wave: `buildRepositoryCleanupStatements`
(`go/internal/storage/cypher/canonical_node_writer_phases.go`) now emits the
retirement cascade from migration doc section 2 ahead of the path-conflict
retirement: one `DETACH DELETE` per owned label (`Workload`,
`WorkloadInstance`, `Endpoint`), each re-deriving the retired ids from the
same path anchor + id guard
(`MATCH (r:Repository {path: $path}) WHERE r.id <> $repo_id
MATCH (n:<Label> {repo_id: r.id}) DETACH DELETE n`), scoped by label +
`repo_id`, never by node id. Templates live in
`go/internal/storage/cypher/canonical/node/templates.go`
(`RepositoryRetirement{Workload,WorkloadInstance,Endpoint}CascadeCypher`).
The cascade is backend-independent (no `OPTIONAL MATCH` + `WITH collect`
composition, so the #8173 NornicDB defect does not apply): the bare writer
emits the same three statements and only the path-conflict tail differs.
Each statement carries its own parameter map and a per-leg summary key
(`repository_cleanup lookup=path_conflict cascade=<Label>`); the retirement
capture still keys on the path-conflict Cypher text, so attribution,
`eshu_dp_canonical_repository_retirements_total`, and the dropped-edge split
are unchanged.

Endpoint-leg index decision: the leg seeks the new `endpoint_repo_id` index
(`CREATE INDEX endpoint_repo_id IF NOT EXISTS FOR (e:Endpoint) ON
(e.repo_id)`, shared DDL in `go/internal/graph/schema_tables_indexes.go`),
next to `workload_repo_id` and `workload_instance_repo_id`. The alternative
— an `Endpoint` label scan per retirement — was rejected: the cascade is a
hot-path graph write and the scan cost grows with the whole `Endpoint`
population, not the retired scope. Schema fingerprint chain bumped
additively for both backends (previous tips stay admitted: the index changes
no `MERGE`/`MATCH` identity and an old writer never touches it).

## Performance Evidence:

New hot-path graph writes: the three cascade deletes (bounded by the retired
`repo_id` via its label's `repo_id` index) plus one additive DDL index with
no transaction-scope, batch-size, worker-count, or queue/lease change. The
T4 live proof below is the before/after on the touched path: before this
wave the writer emitted no owned-label delete (the gap this wave closes);
after it, each leg deletes exactly the retired scope's nodes with a
`NodeIndexSeek` plan and single-digit db hits.
Proof:

- `go test ./internal/storage/cypher/ -run TestLiveRepositoryRetirementCascade -count=1`
  green on the pinned Neo4j (see Live proof): retirement deletes exactly the
  scope's 1+1+1 nodes/edges per label (1->0 settled reads), sibling scope
  untouched, no orphan edges, regeneration under the live id keeps its
  cross-scope `USES` and `DOCUMENTS` edges.
- `PROFILE` operator trees (measured, `TestLiveRepositoryRetirementCascadePlansT4`):
  every leg anchors `NodeUniqueIndexSeek(Locking) UNIQUE r:Repository(path)`
  and seeks `NodeIndexSeek RANGE INDEX n:<Label>(repo_id)`; no `AllNodesScan`
  and no `NodeByLabelScan` on any leg. Exact trees:

  Workload leg:

  ```
  ProduceResults
    EmptyResult
      DetachDelete n
        Eager
          Apply
            Filter NOT cache[r.id] = $repo_id
              NodeUniqueIndexSeek(Locking) UNIQUE r:Repository(path) WHERE path = $path
            NodeIndexSeek RANGE INDEX n:Workload(repo_id) WHERE repo_id = cache[r.id]
  ```

  WorkloadInstance and Endpoint legs are the same tree with
  `n:WorkloadInstance(repo_id)` and `n:Endpoint(repo_id)` seeks
  (measured rows=1 dbhits=2 on both; Workload dbhits=5 for its 4 dropped
  edges plus the node).
- `go test ./internal/storage/cypher/... ./internal/graph/... ./internal/projector/... -count=1`
  green; `go vet` clean on the touched packages; `gofumpt`/`git diff --check`
  clean.

## No-Observability-Change:

This wave adds no metric, span, log, or status signal. The cascade
statements flow through the existing `InstrumentedExecutor` write-count
entries with per-leg summary keys; the retirement report (outcome counter,
dropped-edge split, `canonical repository retired` log, fingerprint
observer) still keys on the path-conflict statement only, so row 834 of
`docs/public/observability/telemetry-coverage.md` stays accurate with no new
row. A retirement that drops owned nodes still logs one WARN with the backend
`relationships_deleted` count; per-leg delete counts are visible in the
statement summaries, not in a new registered metric. The tripwires for a
regression (leaked name-only keys, handle-resolution pressure) remain the
ladder wave's `eshu_dp_workload_identity_collision_total{mode}` and
`eshu_dp_workload_handle_resolution_total{surface,outcome}`.

## Live proof (T4)

Backend: pinned `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
via `docker-compose.live-backend-neo4j.yml` on moved ports (Bolt
`127.0.0.1:27687`, HTTP `127.0.0.1:24747`, project `t4cascade`, native
`linux/arm64` platform — the default `linux/amd64` does not boot on this
host). Production schema applied through
`graph.SchemaStatementsForBackend(neo4j)` with all indexes awaited ONLINE.
Fixture: two repositories with colliding workload name `api`, full
generations through the production writer, one `Workload` + one
`WorkloadInstance` + one `Endpoint` each plus `DEFINES`/`INSTANCE_OF`/
`EXPOSES_ENDPOINT` x2, one cross-scope `USES`, one `DOCUMENTS`, and one
`SAME_NAME` edge across the colliding pair. Counts are settled reads (two
consecutive equal reads).

- Retirement: alpha re-onboarded under a new id on the same path.
  `Workload`/`WorkloadInstance`/`Endpoint` under the retired `repo_id` read
  1->0; retired `Repository` 1->0; re-onboarded `Repository` 1; zero
  relationships touch any retired id; zero nodes carry the retired `repo_id`.
- Sibling/regeneration: beta re-projected under its own id keeps 1/1/1
  nodes, its `USES` (1) and `DOCUMENTS` (1) edges, and sheds the `SAME_NAME`
  edge (0) with the deleted alpha node.
