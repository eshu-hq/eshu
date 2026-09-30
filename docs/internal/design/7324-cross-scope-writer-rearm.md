# Cross-Scope Writers And Repository Re-Projection (#7324)

Status: Accepted (arbiter ruling, 2026-09-29). Refs #7285, #7304, #7321.

## Decision

Contract A is binding: a projector or canonical write never deletes, detaches
or recreates a Repository node it re-projects, and never deletes a
relationship whose target is a Repository unless the statement is owned by
the writer of that relationship (anchored on the source, scoped by
`evidence_source`). No general re-arm signal (option B) is built.

## Why not B

- Same-id re-projection is `MERGE (r:Repository {id}) SET <owned props>`
  (`canonical_node_cypher.go`), so no incoming edge is lost and a re-arm would
  repair a deletion the contract forbids, hiding any writer that breaks it.
- A per-node re-arm fans out O(in-degree) reducer re-runs on every projector
  retry and generation; `repo_dependency` is the global resolver queue.
- `cross_scope_completion_events` is domain-to-domain for three consumers
  (`reducer/crossscope/dependencies.go`), not node-to-writer. Legitimate
  re-arms already exist: `scope reset`/refinalize reopens shared intents;
  repo_dependency schedules a fenced workload_materialization replay.

Same-id edge loss does not reproduce on current main: #7304 removed the by-id
delete, and `TestLiveRepositoryRetryKeepsReducerEdges` proves every reducer
family survives a retry, a new full generation and (since this change) a
delta generation (`docs/internal/evidence/7285-repository-cleanup-keeps-reducer-edges.md`).

## The one exception

`canonicalNodeRepositoryPathCleanupCypher` retires a DIFFERENT-id Repository
at the same path (`repository_path` is UNIQUE, `graph/schema_tables.go`). It
runs on non-first, non-delta projections and drops every relationship on the
retired node. It is kept, counted and logged: the backend's write summary for
that statement is recorded on
`eshu_dp_canonical_repository_retirements_total{outcome}` and the
`canonical repository retired` log (WARN when relationships were deleted,
with scope_id, generation_id, repo_id, path, nodes_deleted,
relationships_deleted, deletes_counted, outcome). That line appears only when
the statement deleted a node. A steady-state projection logs nothing at INFO
or WARN. When the executor reported no write summary, the writer logs a
distinct DEBUG line (`canonical repository retirement not counted: executor
reported no write summary`, `deletes_counted=false`) and moves no counter.
It never claims a retirement it cannot see.

It is the one delete that drops edges with a Repository or removes a
projected one. The only other Repository delete in the tree is the orphan
sweep (`BuildSweepOrphanNodesStatement`, `orphan_sweep_writes.go`). That
statement is a plain `DELETE n`, not DETACH, and it only applies to Repository
stubs that are disconnected and not projector-owned
(`evidence_source <> 'projector/canonical'`). It drops no edge and never
removes a projected Repository, so contract A holds. Its label is built at run
time with `fmt.Sprintf`, however, so the static guard cannot see it. That blind
spot is listed in the guard's doc comment.

### Label refinement on measured evidence

The ruling named the outcomes `clean | dropped_incoming_edges`. The shipped
values are `clean | dropped_relationships`. This is a deliberate refinement,
approved by the coordinator, and is recorded here so the final arbiter merge
review sees the deviation. The reason: `RelationshipsDeleted` counts both
directions. A retired node still carries its own projector edges, because the
retirement runs in phase B, before the new projection re-points files and
directories. The live proof measured a retired node of degree 20: 11 seeded
incoming reducer edges plus 9 projector edges (6 `REPO_CONTAINS`, 3 depth-0
`CONTAINS`). Keyed on `RelationshipsDeleted > 0`, `dropped_incoming_edges`
would fire on every real retirement and would never specifically mean
"incoming". `relationships_deleted` is therefore documented as a
both-direction count. The per-direction and per-type split needs a read seam
in the canonical writer and stays a follow-up. A pre-delete of the retired
node's projector edges was considered and rejected as out of scope: it adds
Cypher, and the count would still include outgoing reducer edges such as
`DEFINES`.

### How the count is collected

The writer stashes a filtered `WriteCountsCollector`
(`NewFilteredWriteCountsCollector`) on the context of only the statements that
include the cleanup: the main atomic group on the `GroupExecutor` path, and the
`repository_cleanup` phase on the phase-group and sequential paths. The
production Bolt executors call `ReportWriteCounts` for every statement:
`cmd/projector`, `cmd/bootstrap-index`, `cmd/reducer` and, since review
finding F1, `cmd/ingester`, whose in-process projector runs its own canonical
writer (`Execute`, the plain group path and the file-group probe path;
pinned by `TestIngesterNeo4jExecutorReportsWriteCountsOnEveryStatementPath`
and `TestIngesterCanonicalWriterReportsPathConflictRetirement`). The collector
keeps the entry whose Cypher equals the constant.
It forwards every entry to any collector already stashed, such as the #6783
differential recorder. The last entry wins, because driver and
`RetryingExecutor` retries re-run and re-report the statement and the report
happens only after the statement's transaction committed. First-generation and
delta writes build no cleanup statement and install nothing.

### Stub re-creation after retirement (asserted since #7446)

The arbiter listed stub re-creation as unverified. The live proof records what
Neo4j actually does. After the retirement, running the real
`CanonicalRepoDependencyUpsertCypher` and `BatchCanonicalSubmodulePinEdgeCypher`
with the retired id as target re-creates one Repository node under the retired
id. That node has `path = null` and `evidence_source = resolver/cross-repo`
(the repo_dependency `ON CREATE SET`), and its incoming edges are
`[PINS_SUBMODULE, DEPENDS_ON]`. It was observed identically under both executor
shapes. Because the node has no path, the path-conflict retirement never
matches it again, so it persists as a path-less orphan stub until a writer
stops naming the retired id. This is a documented gap of contract A.

The #7446 arbiter ruling keeps it: the writers keep MERGE-by-id (MATCH-only
would reintroduce the ordering dependency and the O(in-degree) re-arm this
note rejects, and on NornicDB a bare-MATCH UNWIND write silently drops). The
stub is truthful while an owner asserts an edge into it and reapable after:
owners retract by `evidence_source`, then the `Repository` orphan sweep
deletes it. `eshu_dp_canonical_repository_stubs_created_total{writer}` and
the `canonical repository stub created` log now count it, and the live test
asserts the stub's shape, the count and the reap
(`docs/internal/evidence/7446-repository-stub-counter.md`). A re-point or
re-arm stays gated on production retirement counts.

## Lock order and transaction scope

The retirement is one autocommit or grouped statement with no read-modify-write
and no new lock. It does not serialise anything.

- `GroupExecutor` (Neo4j projector path): the cleanup runs inside the single
  main managed transaction, before the repository upsert and every other phase.
  It takes the `repository_path` unique-index seek lock
  (`NodeUniqueIndexSeek(Locking)`), then the node and relationship write locks
  of the retired node, then the phases that follow. A driver retry of the
  transaction function repeats the whole group, the cleanup included. The
  cleanup is idempotent (the second run matches nothing), and the report uses
  the last attempt's counters.
- `PhaseGroupExecutor` (NornicDB path) and sequential fallback: the
  `repository_cleanup` phase is its own all-retract phase, executed as an
  autocommit statement and committed before the `repository` upsert phase. A
  failure in a later phase requeues the whole write. On retry the cleanup
  matches nothing (the node is gone), so the retirement is reported once, at
  the commit that deleted it.
- At most once, not exactly once: if the retirement commits but the driver
  call still returns an error (for example, a lost acknowledgement), the write
  is retried. The retried cleanup matches nothing, so that retirement is never
  counted. On the atomic path, a group that fails after the cleanup ran rolls
  the delete back and is not reported
  (`TestCanonicalNodeWriterDoesNotReportARolledBackAtomicRetirement`).
- Conflict domain: the `(Repository {path})` unique-index entry for this path,
  which the cleanup reaches through a locking seek. This change adds no
  statement and no lock to that domain: it only reads the write summary the
  executor already produced. The behaviour of two different-id projections of
  one path racing on that entry predates this change and was not measured
  here.

## Enforcement

- Static guard `TestRepositoryIncomingEdgesAreDeletedOnlyByTheirOwners`
  (`go/internal/storage/cypher`). It scans every string constant expression
  under go/internal and go/cmd, folding same-package constant concatenations.
  Rule 1: any statically visible delete of a Repository-bound variable must
  whitespace-normalise to the path-conflict constant. The runtime-labelled
  orphan sweep is the documented exception above. Rule 2: any delete of a relationship whose
  arrow-head endpoint is a Repository must carry `rel.evidence_source =`. The
  seeded violation fails both rules (RED) and the clean tree passes (GREEN).
  At landing the scan reached 227 strings, 22 with DELETE, 1 node delete and
  6 incoming deletes, all evidence-scoped. Its blind spots are listed in the
  test's doc comment.
- Live matrix (Neo4j, `live_nornicdb_answer_truth`): the incoming families
  DEPENDS_ON, DEPLOYS_FROM, DISCOVERS_CONFIG_IN, PROVISIONS_DEPENDENCY_FOR,
  USES_MODULE, READS_CONFIG_FROM, EVIDENCES_REPOSITORY_RELATIONSHIP,
  DEPLOYMENT_SOURCE, CORRELATES_DEPLOYABLE_UNIT, PINS_SUBMODULE and BUILT_FROM
  survive a retry, a new full generation, a delta generation and stub adoption
  (`TestLiveRepositoryRetryKeepsReducerEdges`). The path-conflict retirement
  reports its dropped count
  (`TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges`,
  RED on main, GREEN here).

## Follow-ups

- Decide, from the retirement counts, between a narrow re-arm keyed on the
  retired id (`shared_projection_intents.payload->>'target_repo_id'`) and an
  in-transaction re-point of incoming edges to the new id.
- Per-direction and per-type dropped-edge breakdown once the canonical writer
  has a read seam.
- The path-less stub re-created under a retired id (observed above).
- Repair of edges already lost on ops-qa is an owner-consented replay.

No-Regression Evidence: the retry shape (same generation, FirstGeneration=false, DeltaProjection=false, 6 files, 3 directories, 17 reducer edge families on the Repository) was timed on neo4j:2026-community 2026.09.0 through `CanonicalNodeWriter.Write`, 40 timed writes per run after 5 warm-ups, 7 interleaved before/after pairs of test binaries (before = origin/main 182a31124 production sources, after = this change). Median of per-pair medians: atomic_group 29.815 ms before vs 30.339 ms after; phase_group 37.164 ms before vs 36.454 ms after. Median paired delta: -0.2 ms (atomic) and -0.3 ms (phase), inside host noise at load average 14-16. PROFILE of the unchanged cleanup statement: NodeUniqueIndexSeek(Locking) on repository_path, then Filter, then DetachDelete. Steady state is 0 rows and 1 db hit; a retirement of a degree-20 node is 23 db hits. Details are in `docs/internal/evidence/7324-repository-path-conflict-retirement.md`.

Observability Evidence: the new counter `eshu_dp_canonical_repository_retirements_total{outcome=clean|dropped_relationships}` and the `canonical repository retired` log (WARN with relationships dropped; fields scope_id, generation_id, repo_id, path, nodes_deleted, relationships_deleted, deletes_counted, outcome). The live Neo4j run logged `level=WARN nodes_deleted=1 relationships_deleted=20 deletes_counted=true outcome=dropped_relationships` and incremented the counter once per executor shape; a steady-state retry recorded nothing.
