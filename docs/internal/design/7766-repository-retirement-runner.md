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
the arbiter ruling, round 3 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6084044833)),
and the arbiter ruling, round 4 ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6085855231)), section 3.
Source check: origin/main f4f14a4096, 2026-10-19, re-anchored from
c88c3806a (see the [corrections](7766-repository-retirement-corrections.md#re-anchor-at-f4f14a4096)).

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
**2a, 2q, 2b, 2b', 2c, 2d, 2d', 2e, 2z**. The invariants I1 to I4 in the
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
| 2d', lease settle | `running`, `next_attempt_at` in the future | set by the last 2d batch: `census` | Below |
| 2e, repo-keyed leftovers | `running` | `census` | Below |
| 2z, residue census | `running`, `failed/postgres_residue`, `failed/graph_residue` | `census` | Below |

2q re-runs on every runner resume while the phase is before `purge`, because a
resumed runner cannot assume the queue stayed quiet.

- **2c, graph (`state=repairing_graph`):** see Graph Retraction. Each statement
  is idempotent. The cursor advances after each statement commits. On first
  entry, before G1, the runner reads the repository root and stores it in
  `graph_repo_root`, in the transaction that persists the cursor. It is never
  overwritten, so a re-run of 2c after G6 has cleared `Repository.path` and 2d/2e
  have deleted `content_files` still has it (see
  [Parameter nodes](#parameter-nodes-g5p)). An empty root goes to `failed`,
  `failure_class=repository_root_unknown`. A root shared with another
  Repository skips G5p and is recorded, not failed (same section).
- **2d, purge (`state=running`):** each batch is one transaction:
  1. `SELECT ... FROM ingestion_scopes WHERE scope_id = ANY FOR NO KEY UPDATE SKIP LOCKED`.
     This is the `retention:<scope_id>` domain, and it conflicts with
     `generationRetentionCandidateQuery`'s `FOR UPDATE OF scope SKIP LOCKED`
     (`generation_retention_sql.go:157-164`). On a miss: `blocked/scope_lock_busy`, retry.
  2. Select up to `BatchGenerationLimit=100` generations of the scopes, oldest
     first, all `superseded`. Count them with `generationRetentionRowCountsQuery`
     and apply `BatchRowLimit=100000` with the same one-generation exception
     (`generation_retention.go:27-28`, ADR 2248).
  3. Run the ADR 2248 cascade by reusing retention's step list, not a copy of
     it (`generation_retention.go:401-445` at f4f14a4096): shared intents →
     unroutable intents → `content_file_references` → infra repo lock →
     `content_entities` → infra orphans → `content_files` → changed-since
     ledger → fact-key sidecars (`factkeys.DeleteGenerations`, #7420) → lock the
     cascaded work rows in C order (#8261) → `scope_generations`. Retention
     also deletes the batch's search-index rows in committed batches before its
     transaction (`generation_retention.go:92`, #7427), because migration 191
     dropped the terms table's generation FK. The runner runs that pre-delete
     for its batch before step 1. The last step cascades every table that still
     has a generation FK: `fact_records`,
     `fact_work_items`, `shared_projection_acceptance`
     (`migrations/011:2-5`), and the rest (the
     [census](7766-repository-retirement-table-census.md#purged-at-2d-by-the-generation-cascade) lists them).
  4. Delete the batch's rows from `relationship_evidence_facts`,
     `relationship_candidates`, and `resolved_relationships` by
     `generation_id = ANY` the batch's ids. Retention has no deleter for them;
     the [census](7766-repository-retirement-table-census.md#relationship-tables)
     explains why this runs here and not in 2e.
  5. `rows_deleted = rows_deleted + batch` in the same transaction, then commit.
- **2d', lease settle.** The last 2d batch is the one whose post-delete check
  finds no remaining generation of the scopes, or an empty batch when the
  previous one removed exactly `BatchGenerationLimit`. When it commits, set `phase='census'`
  and `next_attempt_at = clock_timestamp() + ReducerQueue.LeaseDuration` in the
  same transaction. The runner's claim honours `next_attempt_at` through
  `repository_retirements_runnable_idx`, so 2e and 2z run no earlier than one
  reducer lease after the last cascade. A cascaded fresh-id row can have a
  handler that claimed it before the cascade and writes after it: the workload
  materializer writes the graph directly through `CypherExecutor.ExecuteCypher`
  (`reducer/workload_materializer.go:18-19`, `:109`), not through intents. A
  heartbeat on a vanished row affects 0 rows and returns `ErrReducerClaimRejected`
  (`storage/postgres/reducer_queue.go:391-415`), and the reducer service cancels
  the handler context on the first periodic failure
  (`reducer/service_heartbeat.go:98-124`; interval `LeaseDuration/2`,
  `cmd/reducer/main.go:426`, `main_helpers.go:49`; lease 1 min, `cmd/reducer/main_helpers.go:45`). So a
  handler whose row the cascade removed is cancelled within `LeaseDuration/2` plus
  one in-flight statement, and the lease TTL is the bound the lease contract
  gives. No new column and no new `blocked_reason`: the cost is one pause of
  about one lease (1 min by default) in a background runner. Residual: a handler
  that ignores cancellation past its lease is outside the lease contract, as
  every lease in the system today. Placing 2e after the settle also lets it sweep
  repo-keyed rows a late writer produced.
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
    `shared_projection_acceptance`; 0 intents and unroutable intents with
    `repository_id = $repo` under any scope; and 0 rows of the three
    relationship tables with `source_repo_id = $repo`. Other repositories'
    reducers write the last two (see the writer table in the main design).
  - Graph: the P10 census, anchored by repo id, plus a count of `Parameter`
    nodes under the G5p predicate (skipped when the root is shared), because
    Parameters carry no `repo_id`. The
    runtime census must be index-anchored; P10' proves it (`PROFILE`, db hits at
    most 2x at 10x store). If it cannot be anchored, restrict it to the label
    and edge families in the plan table below.
  - Dirty: re-run once from the owning step, 2b for Postgres residue (it also
    re-runs 2b', 2c, and the purge) and 2c for graph residue, then re-census.
  - Still dirty: `state=failed`, `failure_class=postgres_residue` or
    `graph_residue`, the census in `failure_details`. A re-issue resumes at 2b
    or 2c to match.
  - A retirement never reaches `complete` with residue that a lease-abiding
    writer could have produced; 2z is the census that proves it.

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
- the settle rides in the last 2d batch's transaction, so a crash resumes at
  `census` with `next_attempt_at` already set and the lease wait is not skipped;
- a graph statement re-run deletes nothing;
- 2q and 2b re-run to zero. `intent_barrier_at` is set once, after the delete
  commits, and a re-run never moves it earlier;
- graph counts are at most once, because a statement that commits before its
  cursor persists is not re-counted. This is the same caveat as 7324.

Failure handling:
- transient errors (40001, 40P01, 55P03, retryable graph errors) back off
  exponentially, capped at 5 min;
- non-transient errors (`failure_class` bounded) go to `failed` after 5 attempts;
- 2z residue goes to `failed/postgres_residue` or `failed/graph_residue`
  without retrying;
- re-issuing the retire request (new key) resets `failed` → `pending` on the
  same row, in phase 1's step 7g, and keeps `phase` and the cursor.

## Graph Retraction

Graph writes belong to the reducer, and the recovery code holds no graph write
credential (`graph-rebuild-from-facts.md:71-72`), so the graph steps run in the
reducer.
The order is reducer-owned edges, then projector-owned nodes, then the
Repository node:

| Step | Reused path | Removes |
| --- | --- | --- |
| G1 | `EdgeWriter.RetractEdges` (`storage/cypher/edge/writer/retract.go:34-132`) per repo-keyed domain, rows from `buildRepoDependencyRetractRows` (`reducer/repo_dependency_projection_replay.go:286`) and siblings: repo_dependency, submodule_pin, codeowners, workload_dependency, deployable_unit_edges, code_calls, handles_route, runs_in, invokes_cloud_action, shell_exec, sql_relationships, inheritance_edges | Source-anchored, `evidence_source`-scoped edges the repo owns, including its outgoing edges into other Repositories (rule 2 holds) |
| G2 | `RetractEdges(rationale_edges, buildRationaleRepoRetractRows)` (`reducer/rationale_delta_scope.go:144`) | `EXPLAINS` |
| G3 | `RetractEdges(documentation_edges, buildDocumentationScopeRetractRows(scope_ids))` (`reducer/documentation_edge_delta_scope.go:289`) | `DOCUMENTS` by `section.scope_id` |
| G4 | `RetractSingleRepoRelationshipEdgesCypher`, `RetractSingleRepoRunsOnEdgesCypher`, `RetractSingleRepoEvidenceArtifactsCypher` (`canonical_relationships.go:355-386`) | Repo-anchored relationship edges and evidence artifacts |
| G5 | `CanonicalNodeWriter.buildRetractStatements` + `buildEntityRetractStatements` (`storage/cypher/canonical_node_writer_retract.go:17-144`) with `RepoID`, sentinel `GenerationID="retired:<retirement_id>"`, no files, `FirstGeneration=false`, `DeltaProjection=false`, `Drain=true` | Every projector-owned File, Directory, and entity node of the repo, and every relationship on them |
| G5f | New repo-anchored File retract, `Drain=true`: `MATCH (f:File) WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' DETACH DELETE f`. G5's file retract anchors on the Repository node (`storage/cypher/canonical/node/templates.go:11-13`), which the path-conflict cleanup may already have deleted (`:148-153`). No `File.repo_id` index exists; PR 6 adds one. | Files of the retired id that G5 cannot reach. `File.path` is unique (`graph/schema_tables.go:20`), so a file the live owner also projects carries the live id and is not matched. |
| G5p | New prefix-bound Parameter retract, `Drain=true` (see [Parameter nodes](#parameter-nodes-g5p)) | Every projector-owned Parameter node under the repository root, of any generation |
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

### Parameter Nodes (G5p)

A `Parameter` node has no `repo_id`. Its key is `(name, path,
function_line_number)` (`storage/cypher/canonical/node/templates.go:415-422`,
`graph/schema_tables.go:151`; NornicDB drops composite constraints,
`graph/schema.go:141-148`), and its `path` is the repository root joined to the
file's relative path (`projector/canonical/builder.go:409-422`, `qualifyPath` at
`:458-466`). The existing retract, `node.RetractParametersCypher`
(`storage/cypher/canonical/node/templates.go:110-113`), takes an exact path
list. That list is only known while the paths are: G6 clears
`Repository.path`, and 2d and 2e delete `content_files`. So a 2c re-run on the
2z dirty path could not rebuild the list, and a census by `repo_id` never sees a
Parameter. G5p is anchored on the root instead:

```cypher
MATCH (p:Parameter)
WHERE p.path STARTS WITH $root_prefix
  AND p.evidence_source = 'projector/canonical'
  AND NOT any(n IN $nested_root_prefixes WHERE p.path STARTS WITH n)
DETACH DELETE p
```

- **`$root_prefix`** is `graph_repo_root + '/'`. The root is read once, at 2c
  entry, with the precedence `BuildMaterialization` uses: the repository fact's
  `path` in the scope's newest generation, else the scope's `metadata.repo_path`
  (`projector/canonical/builder.go:47`, `:64-71`).
- **A shared root skips G5p.** Two repository ids can hold one path: the
  path-conflict cleanup deletes the old Repository node when a repository is
  re-keyed or moved (`storage/cypher/canonical_node_writer_phases.go:44-112`),
  and the old scope stays in Postgres. That stale copy is what an operator
  retires. Parameters share one key across both ids, so the live owner's
  Parameters sit under the same root. When another Repository's `path` equals
  the root or is a parent of it, the runner skips G5p and the 2z Parameter
  count, and records `parameter_retract=skipped_root_shared` with the other
  repository id in the census. Parameters that only the retired copy wrote stay;
  no key reaches them, and they are a stated residual.
- **`$nested_root_prefixes`** is `path + '/'` of every other Repository whose
  `path` starts with `$root_prefix`, read just before each G5p batch through the
  `repository_path` uniqueness index (`graph/schema_tables.go:17`). A checkout
  nested inside the retired one keeps its Parameters.
- **Empty root fails closed.** A projector Run with no root writes bare relative
  paths (`projector/canonical/builder.go:459-461`). Those collide across repositories under the
  uniqueness key, so no path predicate can scope them to one repository.
- **G5p also reaps Parameters that full refreshes already leaked.** The
  full-refresh retract names only the current files' paths
  (`storage/cypher/canonical_node_writer_retract.go:33-37`, `:102-112`). The entity retract
  deletes a removed file's `Function` and its `HAS_PARAMETER` edge, and the
  Parameter is left with no edges. That is from source, not run live. It
  predates #7766, and G5p removes it for the retired repository only.
- **Index.** On Neo4j the only index on `Parameter.path` is the composite
  uniqueness index, which leads with `name`, so a `path`-only predicate cannot
  seek it. The single-property `Parameter.path` index is NornicDB-only
  (`graph/schema_tables_indexes.go:336`). PR 6 adds a Neo4j range index on
  `Parameter.path`, and P5 and P10' prove the prefix seek.
- **Replay.** The `NOT` term makes `isIdempotentRetractStatement` false
  (`storage/cypher/writer.go:179-193`), the same as the existing G5 retracts'
  `generation_id <> $generation_id`. It is not the #8359 edgeless-reap shape
  either (`storage/cypher/edgeless_reap.go:37-58` refuses `DETACH`), so
  `AllStatementsAreReplaySafe` (`storage/cypher/writer.go:111-125`) refuses it.
  The runner's cursor owns the retry, not the executor's group replay.
- **Residuals.** A repository whose root moved between generations keeps
  Parameters under the old root, and neither retract names them. A checkout
  nested under the root and first indexed between the nested-root read and the
  batch loses its Parameters until its next generation re-merges them. A
  Parameter whose relative path is empty has the bare root as its path
  (`qualifyPath`), which the prefix misses; no extractor is known to emit one
  (NOT_CHECKED).

**Repository node decision: demote, then the orphan sweep reaps it.**

```cypher
MATCH (r:Repository {id: $repo_id})
WHERE r.evidence_source = 'projector/canonical'
SET r.evidence_source = 'retirement/repository', r.path = null, r.local_path = null,
    r.scope_id = null, r.generation_id = $sentinel_generation_id
```

Reasons:
- **Leaving the node to the sweep alone does not work.** A retired node is
  `projector/canonical` (`storage/cypher/canonical/node/templates.go:197-202`),
  and the sweep's Repository guard excludes exactly that value
  (`storage/cypher/sweep/orphan/store.go:431-436`, applied through
  `sweep/orphan/guards.go:75-80`).
- **A direct delete does not work either.**
  - `DETACH DELETE` breaks contract A, because other repos' incoming edges remain.
  - A plain `DELETE` errors on Neo4j while relationships remain.
  - Making the delete conditional needs a relationship-existence predicate.
    The sweep forbids them (`storage/cypher/sweep/orphan/store.go:103-113`).
    #8359 proved one form, `NOT EXISTS { MATCH (n)-[]-() }`, correct on the
    pinned NornicDB and admits it only for the semantic writer's edgeless reap
    (`storage/cypher/edgeless_reap.go:8-58`). Even in that form, a guarded
    Repository delete breaks rule 1 of
    `TestRepositoryIncomingEdgesAreDeletedOnlyByTheirOwners`
    (`canonical_repository_incoming_edge_guard_test.go:167-176`), and it cannot
    fire while another repository's incoming edge remains.
- **Demotion only writes properties in the projector-owned set**, so
  `TestRepositoryPropertyWritersStayInsideTheProjectorUpsert`
  (`canonical_repository_property_writers_test.go:134`) holds. It adds no
  delete, so rule 1 of `TestRepositoryIncomingEdgesAreDeletedOnlyByTheirOwners`
  is unchanged.
- **Clearing `path` keeps the node out of the path-conflict cleanup.** Both
  shapes, `node.RepositoryPathCleanupCypher` (Neo4j,
  `storage/cypher/canonical/node/templates.go:148-153`) and
  `node.RepositoryPathCleanupBareCypher` (NornicDB, `:162-164`), and the #5385
  Workload, WorkloadInstance, and Endpoint cascade (`:182-195`) anchor on
  `MATCH (r:Repository {path: $path})`, which a null `path` never matches.

**Contract A and stubs:**
- Incoming edges owned by other repositories stay until those owners retract
  by `evidence_source`.
- While an owner still names the id, `repo_dependency` and `submodule_pin`
  MERGE onto the demoted node. Neither changes its owned properties, so the node
  stays `retirement/repository`. `repo_dependency`'s `ON CREATE SET` does not
  fire, and `eshu_dp_canonical_repository_stubs_created_total` does not move.
  `submodule_pin` has no `ON CREATE SET` on its target
  (`canonical_submodule_edges.go:29-35`), so a stub it creates after the reap
  has no `evidence_source`, and the sweep's `IS NOT NULL` guard never reaps it.
  That predates #7766.
- Once every owner has retracted, the sweep marks the node and, after the TTL
  (7 days by default, `cmd/reducer/config.go:96`, overridable through
  `ESHU_GRAPH_ORPHAN_SWEEP_TTL`), deletes it with a plain `DELETE`. This is the
  existing 7324 path. Since #8371 the sweep reports the deleted Repository ids
  (`sweep/orphan/store.go:177-187`), which reopens `deployable_unit_correlation`
  work keyed on them; whether that is wanted for a retired id is NOT_CHECKED.
- If an owner names the id again after the reap, the documented path-less stub
  (`resolver/cross-repo`) re-appears and is counted.
- Read surfaces resolve a node's state from the open tombstone first
  (`readmitted_at IS NULL`), so a stub under a retired id still reads `retired`,
  and a re-admitted repository reads `indexed`.
- On re-admission, the canonical upsert `MERGE` by id re-promotes the node and
  rewrites every owned property, which adopts the existing incoming edges.
