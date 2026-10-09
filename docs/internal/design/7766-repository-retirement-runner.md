# Repository Retirement: The Runner, Deliverable 2 (#7766)

Issue: #7766
Companions: [Repository Retirement](7766-repository-retirement.md) (the design
and phase 1), [Concurrency Contract](7766-repository-retirement-concurrency.md)
(barrier, invariants), [Reducer Reopen And Replay Writers](7766-repository-retirement-reducer-writers.md)
(2a and 2q), [Table Census](7766-repository-retirement-table-census.md) (what 2d,
2e, and phase 3 remove), and
[Proof And Rollout](7766-repository-retirement-proof-and-rollout.md).

Binding inputs: the arbiter rulings on
[#7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598) and
[the prove-first results](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964),
and the arbiter ruling, round 3 (to be posted on #7766).
Source check: origin/main 3b03f018e, 2026-10-09.

Deliverable 2 completes what Deliverable 1 starts. Deliverable 1 leaves the
scope `pending`, fenced, and reported `retiring`. The runner takes it through
the graph retraction, the purge, the residue census, and phase 3, so the
tombstone reaches `complete` and the repository reports `retired`. It needs
P1q, P9a to P9d, and P10' before code lands (see the
[prove-first table](7766-repository-retirement-proof-and-rollout.md#prove-first-table)).

## Phase 2: Graph, Then Bounded Purge

The runner claims rows from `repository_retirements_runnable_idx` with
`FOR UPDATE SKIP LOCKED`, `lease_owner`, and `claim_until` (60s, heartbeat).
Every step persists `phase` and `graph_step_cursor`. The order is
**2a, 2q, 2b, 2b', 2c, 2d, 2e, 2z**. The invariants I1 to I4 in the
[concurrency contract](7766-repository-retirement-concurrency.md#invariants) are
what make the order safe.

| Step | State, blocked reason | Phase | Where specified |
| --- | --- | --- | --- |
| 2a, projector-lease wait | `blocked/projector_lease_live` | `quiesce` | [Reducer writers](7766-repository-retirement-reducer-writers.md#2a-and-2q) |
| 2q, reducer quiesce | `blocked/reducer_lease_live`, `blocked/claim_fence_busy` | `quiesce` | Same |
| 2b, intent delete, then the barrier | none | `barrier` | [Concurrency](7766-repository-retirement-concurrency.md#2b-and-2b) |
| 2b', lease-epoch wait | `blocked/shared_lease_live` | `barrier` | Same |
| 2c, graph | `repairing_graph` | `graph_retract` | Below |
| 2d, purge | `running`, `blocked/scope_lock_busy` | `purge` | Below |
| 2e, repo-keyed leftovers | `running` | `purge` | Below |
| 2z, residue census | `running`, `failed/graph_residue` | `census` | Below |

2q re-runs on every runner resume while the phase is before `purge`, because a
resumed runner cannot assume the queue stayed quiet.

- **2c, graph (`state=repairing_graph`):** see Graph Retraction. Each statement
  is idempotent. The cursor advances after each statement commits.
- **2d, purge (`state=running`):** each batch is one transaction:
  1. `SELECT ... FROM ingestion_scopes WHERE scope_id = ANY FOR NO KEY UPDATE SKIP LOCKED`.
     This is the `retention:<scope_id>` domain, and it conflicts with
     `generationRetentionCandidateQuery`'s `FOR UPDATE OF scope SKIP LOCKED`
     (`generation_retention_sql.go:61-68`). On a miss: `blocked/scope_lock_busy`, retry.
  2. Select up to `BatchGenerationLimit=100` generations of the scopes, oldest
     first, all `superseded`. Count them with `generationRetentionRowCountsQuery`
     and apply `BatchRowLimit=100000` with the same one-generation exception
     (`generation_retention.go:27-28`, ADR 2248).
  3. Run the ADR 2248 cascade (`generation_retention.go:437-471`):
     shared intents → unroutable intents → `content_file_references` →
     infra repo lock → `content_entities` → `infra_resource_entities` →
     `content_files` → changed-since ledger → `scope_generations`. The last
     step cascades every table with a generation FK: `fact_records`,
     `fact_work_items`, `shared_projection_acceptance`
     (`migrations/011:2-5`), and the rest (the
     [census](7766-repository-retirement-table-census.md#purged-at-2d-by-the-generation-cascade) lists them).
  4. `rows_deleted = rows_deleted + batch` in the same transaction, then commit.
- **2e, repo-keyed leftovers, chunked 10,000 by ctid.** Tables with no
  generation FK and a repo or scope key that retention does not reach. The
  [census](7766-repository-retirement-table-census.md#removed-at-2e)
  holds the full list and each predicate. The core set:
  - `content_files`, `content_entities`, `content_file_references` `WHERE repo_id=$1`
    (`content_files_repo_path_idx`, `content_entities_repo_idx`, `migrations/004:57-60`);
  - `repository_refs WHERE repo_id=$1` (`004:46-55`);
  - `repository_selection_observations` and `repository_reindex_requests` `WHERE scope_id = ANY`.
- **2z, residue census** (before phase 3):
  - Postgres: 0 rows of the scopes in `fact_work_items`,
    `shared_projection_intents`, `shared_projection_unroutable_intents`, and
    `shared_projection_acceptance`.
  - Graph: the P10 census, anchored by repo id. The runtime census must be
    index-anchored; P10' proves it (`PROFILE`, db hits at most 2x at 10x store).
    If it cannot be anchored, restrict it to the label and edge families in the
    plan table below.
  - Dirty: re-run 2c once and re-census.
  - Still dirty: `state=failed`, `failure_class=graph_residue`, the census in
    `failure_details`. A re-issue resumes at 2c.
  - A retirement never reaches `complete` with residue.

  The full table coverage is a CI test, not a runtime query (see the census).

## Phase 3: Complete, One Transaction

1. Lock the scope rows `FOR UPDATE`.
2. Assert no generation remains.
3. **Pre-count the cascaded tables the tombstone reports.** After the scope rows
   are locked and before the DELETE, count each table the census marks
   `cascaded at phase 3, pre-counted` by `scope_id = ANY`: the four grant tables
   (`tenant_scope_grants`, `tenant_repository_grants`,
   `identity_role_scope_targets`, `identity_role_repository_targets`) and the
   other cascaded tables. Fold the counts into `rows_deleted`. A cascade reports
   no counts of its own, so without this step the tombstone would be silent
   about what it dropped.
4. `DELETE FROM ingestion_scopes` (ref scopes first). The FK cascade removes
   every table the census marks cascaded. It is a longer list than
   `projector_scope_claim_fences` alone.
5. Set `state='complete'`, `phase='done'`, `retired_at=now()`, then commit.

**Grants are deliberately dropped.** `tenant_scope_grants` and
`tenant_repository_grants` reference `ingestion_scopes(scope_id) ON DELETE
CASCADE` (`migrations/006c_tenant_workspace_grants.sql:34`, `:62`), and
`identity_role_scope_targets` and `identity_role_repository_targets` do the same
(`006g_identity_oidc_login.sql:58`, `:83`; the repository targets also cascade
from the scope targets at `:93-94`). Access fails closed, which is the right
default for an authorization table whose `policy_revision_hash` may have moved.
Restoring a snapshot on re-admission would re-grant access to content the
retirement erased. See the
[operator surface](7766-repository-retirement-surfaces.md#operator-surface) for
the counts and the wording. No production caller of `UpsertScopeGrant` or
`UpsertRepositoryGrant` exists outside `storage/postgres/tenant`, so nothing
re-derives a dropped grant. The writers of the two identity target tables are
NOT_CHECKED.

**Crash, restart, and failure handling.**

Crash or restart: the lease expires, and another runner reclaims the row and
resumes at `phase` and the cursor:
- each 2d batch committed its deletes and its counts together, so a re-run
  selects only the generations that remain;
- a graph statement re-run deletes nothing;
- 2q and 2b re-run to zero. `intent_barrier_at` is set once, after the delete
  commits, and a re-run never moves it earlier;
- graph counts are at most once, because a statement that commits before its
  cursor persists is not re-counted. This is the same caveat as 7324.

Failure handling:
- transient errors (40001, 40P01, 55P03, retryable graph errors) back off
  exponentially, capped at 5 min;
- non-transient errors (`failure_class` bounded) go to `failed` after 5 attempts;
- 2z residue goes to `failed/graph_residue` without retrying;
- re-issuing the retire request (new key) resets `failed` → `pending` on the
  same row.

## Graph Retraction

The API holds no graph write credential
(`graph-rebuild-from-facts.md:71-72`), so the graph steps run in the reducer.
The order is reducer-owned edges, then projector-owned nodes, then the
Repository node:

| Step | Reused path | Removes |
| --- | --- | --- |
| G1 | `EdgeWriter.RetractEdges` (`edge/writer/retract.go:33-131`) per repo-keyed domain, rows from `buildRepoDependencyRetractRows` (`reducer/repo_dependency_projection_replay.go:285`) and siblings: repo_dependency, submodule_pin, codeowners, workload_dependency, deployable_unit_edges, code_calls, handles_route, runs_in, invokes_cloud_action, shell_exec, sql_relationships, inheritance_edges | Source-anchored, `evidence_source`-scoped edges the repo owns, including its outgoing edges into other Repositories (rule 2 holds) |
| G2 | `RetractEdges(rationale_edges, buildRationaleRepoRetractRows)` (`reducer/rationale_delta_scope.go:143`) | `EXPLAINS` |
| G3 | `RetractEdges(documentation_edges, buildDocumentationScopeRetractRows(scope_ids))` (`reducer/documentation_edge_delta_scope.go:287`) | `DOCUMENTS` by `section.scope_id` |
| G4 | `RetractSingleRepoRelationshipEdgesCypher`, `RetractSingleRepoRunsOnEdgesCypher`, `RetractSingleRepoEvidenceArtifactsCypher` (`canonical_relationships.go:351-382`) | Repo-anchored relationship edges and evidence artifacts |
| G5 | `CanonicalNodeWriter.buildRetractStatements` + `buildEntityRetractStatements` (`canonical_node_writer_retract.go:19-146`) with `RepoID`, sentinel `GenerationID="retired:<retirement_id>"`, no files, `FirstGeneration=false`, `DeltaProjection=false`, `Drain=true`; plus `canonicalNodeRetractParametersCypher` (`canonical_node_cypher.go:110-113`) with paths paged from `content_files` (still present, because 2c precedes 2d) | Every projector-owned File, Directory, entity, and Parameter node of the repo, and every relationship on them |
| G6 | New statement (below) | Demotes the Repository node |

Which evidence sources each G1 domain is retracted under comes from one closed
table in `go/internal/reducer/retirement/plan.go`. Coverage is enforced by
`TestRetirementRetractPlanClassifiesEveryDomain`. It iterates the reducer domain
set (`reducer/contract/domain.go:17-180`) and requires each domain to be one of:
- `retract_by_repo`;
- `removed_by_node_retract` (edges only incident to G5 nodes);
- `not_repository_scoped`, with a reason (cloud, IAM, Kubernetes).

It is the same shape as `TestWholeScopeRetractDomainsCoversFencedSet`, so a
domain added later fails the test until someone classifies it.

**Repository node decision: demote, then the orphan sweep reaps it.**

```cypher
MATCH (r:Repository {id: $repo_id})
WHERE r.evidence_source = 'projector/canonical'
SET r.evidence_source = 'retirement/repository', r.path = null, r.local_path = null,
    r.scope_id = null, r.generation_id = $sentinel_generation_id
```

Reasons:
- **Leaving the node to the sweep alone does not work.** A retired node is
  `projector/canonical` (`canonical_node_cypher.go:124-129`), and the sweep's
  Repository guard excludes exactly that value (`orphan_sweep.go:410-412`).
- **A direct delete does not work either.**
  - `DETACH DELETE` breaks contract A, because other repos' incoming edges remain.
  - A plain `DELETE` errors on Neo4j while relationships remain.
  - Making the delete conditional needs a relationship-existence predicate,
    and those are mis-evaluated on the pinned NornicDB (`orphan_sweep.go:94-104`).
- **Demotion only writes properties in the projector-owned set**, so
  `TestRepositoryPropertyWritersStayInsideTheProjectorUpsert`
  (`canonical_repository_property_writers_test.go:132`) holds. It adds no
  delete, so rule 1 of `TestRepositoryIncomingEdgesAreDeletedOnlyByTheirOwners`
  is unchanged.
- **Clearing `path` keeps the node out of `canonicalNodeRepositoryPathCleanupCypher`**
  (`canonical_node_cypher.go:120-122`).

**Contract A and stubs:**
- Incoming edges owned by other repositories stay until those owners retract
  by `evidence_source`.
- While an owner still names the id, `repo_dependency` and `submodule_pin`
  MERGE onto the demoted node. Their `ON CREATE SET` does not fire, so the node
  stays `retirement/repository`, and `eshu_dp_canonical_repository_stubs_created_total`
  does not move.
- Once every owner has retracted, the sweep marks the node and, after the TTL
  (7 days, `orphan_sweep.go:17`), deletes it with a plain `DELETE`. This is the
  existing 7324 path.
- If an owner names the id again after the reap, the documented path-less stub
  (`resolver/cross-repo`) re-appears and is counted.
- Read surfaces resolve a node's state from the tombstone first, so a stub under
  a retired id still reads `retired`.
- On re-admission, the canonical upsert `MERGE` by id re-promotes the node and
  rewrites every owned property, which adopts the existing incoming edges.
