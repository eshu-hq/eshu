# Repository Retirement: Fenced, Idempotent, Operator-Driven (#7766)

Issue: #7766
Companions: [Proof And Rollout](7766-repository-retirement-proof-and-rollout.md)
holds the prove-first table and results, concurrency proof, test plan, and PR
breakdown. [Concurrency Contract](7766-repository-retirement-concurrency.md)
holds the phase 1 lock budget, the intent-delete barrier, the invariants, and
the commit gate.
Binding inputs: the
[arbiter ruling for #7765 and #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6073882598)
(Option C: signal only, plus one operator-driven retire primitive), the arbiter
ruling on the #7766 prove-first results ([posted on #7766](https://github.com/eshu-hq/eshu/issues/7766#issuecomment-6082885964)), ADR 2248
([retention semantics](2248-retention-semantics-generations-facts-content.md)),
design [7324](7324-cross-scope-writer-rearm.md),
`docs/public/reference/hosted-retention-deletion-policy.md`, and
`docs/public/operate/graph-rebuild-from-facts.md`.

Status: proposed design, amended on 2026-10-09. P1 and P9 failed as originally
designed, and the design changed per the ruling on the
[Prove-first results](7766-repository-retirement-proof-and-rollout.md#prove-first-results).
No code lands until the remaining bars in the
[Prove-first table](7766-repository-retirement-proof-and-rollout.md#prove-first-table)
pass. See also
[Corrections](7766-repository-retirement-proof-and-rollout.md#corrections-to-the-arbiter-ruling):
seven premises of the first ruling and nine items from the results review.

Source check: origin/main 195337b97, 2026-10-08; amendments re-checked against
16c8c2a36, 2026-10-09.

## Purpose

Eshu has no operator path to remove an indexed repository. A deselected or
duplicate repository (the #7766 report: a stale filesystem copy plus a remote
copy with the same name) keeps its scope, generations, facts, content, and
graph nodes. It keeps answering as indexed. This design adds one
service-level retire primitive. The admin route and the CLI call it, and any
future opt-in policy would call it unchanged. It is fenced against every
ingest path, purges in bounded batches, retracts the graph through existing
paths, and leaves a durable tombstone that read surfaces report as `retired`.

## Decision Summary

- The marker is a new table, `repository_retirements`, keyed by the canonical
  repo id (`ingestion_scopes.partition_key`). One open row covers the
  default-branch scope and every `@ref` scope of the repository. The table has
  no FK to `ingestion_scopes`, so the tombstone outlives the scope row.
- Phase 1 runs in the API, in one transaction. It takes the existing
  per-repository advisory lock in exclusive mode. It reuses
  `WaitForReducerDrain` -> `AcquireReducerClaimFence` -> `AssertRetirementFenced`.
  It marks every non-superseded generation `superseded` and writes the marker.
  Superseding the generations is what fences the projector. No claim SQL changes.
  A precheck bounds the request, and the table lock waits 1 s with a jittered
  retry ([lock budget](7766-repository-retirement-concurrency.md#phase-1-lock-budget)).
- Phases 2 and 3 run in a reducer runner. Phase 2 first deletes the shared
  intents and waits out the shared-projection leases claimed before the delete
  (the barrier). It then retracts the graph through existing retract paths and
  purges Postgres in ADR 2248 batches
  under the `retention:<scope_id>` conflict domain, which is the scope-row lock.
  Phase 3 deletes the scope rows and marks the tombstone `complete` in the
  same transaction.
- The Repository node is never deleted by the primitive. It is demoted to a
  non-projector stub shape, and the existing `Repository` orphan sweep reaps it
  once its other owners retract (see Graph Retraction).
- Re-admission is a fresh first generation. It sets `readmitted_at` inside the
  ingest transaction that re-admits, and it emits
  `outcome=readmitted_after_retirement`.

## Durable Marker And Writers

`ingestion_scopes.status` cannot carry the marker. The ON CONFLICT arm of the
upsert rewrites it on every commit (`storage/postgres/ingestion_queries.go:79-85`, value bound at `:249`).

The migration takes the next free number (167 at this check;
`go/internal/storage/postgres/migrations/166_repository_selection_observations.sql` is the latest).
It also adds `shared_projection_partition_leases.claimed_at`, the lease epoch
the phase 2 barrier reads (see
[Shared-Projection Barrier](7766-repository-retirement-concurrency.md#shared-projection-barrier)):

```sql
CREATE TABLE IF NOT EXISTS repository_retirements (
    retirement_id TEXT PRIMARY KEY,            -- 'rr_' || sha256(repo_id||':'||requested_at)[:16]
    repo_id TEXT NOT NULL,                     -- ingestion_scopes.partition_key
    scope_id TEXT NOT NULL,                    -- default-branch scope
    repo_slug_key TEXT NOT NULL DEFAULT '',    -- lower(payload->>'repo_slug'), webhook gate only
    state TEXT NOT NULL CHECK (state IN ('pending','running','blocked','repairing_graph','complete','failed')),
    phase TEXT NOT NULL CHECK (phase IN ('fenced','graph_retract','purge','finalize','done')),
    graph_step_cursor INTEGER NOT NULL DEFAULT 0,
    blocked_reason TEXT NOT NULL DEFAULT '' CHECK (blocked_reason IN
      ('','projector_lease_live','shared_lease_live','scope_lock_busy','graph_unavailable')),
    failure_class TEXT NOT NULL DEFAULT '',
    reason_code TEXT NOT NULL CHECK (reason_code IN ('operator_retired')),
    reason_hash TEXT NOT NULL, actor_class TEXT NOT NULL, actor_id_hash TEXT NOT NULL DEFAULT '',
    idempotency_key_hash TEXT NOT NULL, scope_id_hash TEXT NOT NULL,
    generation_ids_hash TEXT NOT NULL, generations_fenced INTEGER NOT NULL,
    rows_deleted JSONB NOT NULL DEFAULT '{}'::jsonb,  -- {table: count}, summed per batch
    graph_nodes_deleted BIGINT NOT NULL DEFAULT 0,
    graph_relationships_deleted BIGINT NOT NULL DEFAULT 0,
    intent_barrier_at TIMESTAMPTZ NULL,        -- set after 2b's intent delete commits
    lease_owner TEXT NULL, claim_until TIMESTAMPTZ NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0, next_attempt_at TIMESTAMPTZ NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
    retired_at TIMESTAMPTZ NULL, readmitted_at TIMESTAMPTZ NULL
);
ALTER TABLE shared_projection_partition_leases ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ NULL;
CREATE UNIQUE INDEX IF NOT EXISTS repository_retirements_open_repo_idx
    ON repository_retirements (repo_id) WHERE readmitted_at IS NULL;
CREATE INDEX IF NOT EXISTS repository_retirements_runnable_idx
    ON repository_retirements (next_attempt_at)
    WHERE state IN ('pending','running','blocked','repairing_graph');
CREATE INDEX IF NOT EXISTS repository_retirements_scope_idx ON repository_retirements (scope_id);
```

An open row (`readmitted_at IS NULL`) in any state except `complete` refuses
ingest. A `complete` row is a tombstone. The next ingest re-admits the
repository and stamps `readmitted_at`. The `ingest refusal` column below gives
the bounded refusal each writer returns.

| Writer | Check point | Ingest refusal |
| --- | --- | --- |
| Collector commit: `CommitScopeGeneration` / `CommitClaimedScopeGeneration` (`ingestion.go:118-143`), bootstrap-index (`cmd/bootstrap-index/bootstrap_collector_commit.go:79`), ingester's in-process path, collector dead-letter replay (`recovery.go:288-311` re-commits) | Inside `commitScopeGeneration`, after the shared advisory lock (`ingestion.go:219`), folded into `upsertIngestionScope` (`:231`) so the check and the write share one snapshot (see Commit Gate in the concurrency contract) | `repository_retiring`. Roll back, drain the stream, return nil. This mirrors the finalized-skip branch (`ingestion.go:237-251`), so there is no dead letter. |
| Webhook handoff (`collector/repo/git/webhook_trigger_selector.go:142`) | After `repositoryIDsFromWebhookTriggers`, match on `repo_slug_key` for open, non-`complete` rows | `MarkTriggersFailed(..., "repository_retiring", ...)`, a new constant beside `:36-40`. A slug-form miss still hits the commit fence. |
| Projector claim (`projector_queue_claim_sql.go:158-179`) | No SQL change. Phase 1 supersedes the generations, so the #7130 branch supersedes claimable rows. Heartbeat and Ack refuse (`projector_queue_sql.go:230-262`), and replay is fenced (`recovery.go:119`). | Existing `projector_superseded_by_newer_generation` / ack-superseded classes. |
| Reducer claim | Phase 1 deletes the claimable reducer rows of the fenced generations under the claim fence | No row is left to claim |
| `recover-generations` / `refinalize` (`rebuild/reset/reset.go:63-100`) | New first CASE arm: `EXISTS` open row → skip. Named unknown scope with a `complete` row (`refinalize.go:262-267`) | `skip_reason=repository_retiring` / `repository_retired`, new `recovery.SkipReason*` |
| Repository reindex (`query/admin/reindex_repository.go:68`) | After `gitDefaultScopeMatch`, look up the open row | 400 problem `"<sel>": repository is retiring (state <s>)`. Phase 1 also deletes the scope's `repository_reindex_requests` row. |
| Deferred backfill (`ingestion_backfill.go:333`, under the same exclusive repo lock) | Filter repo ids that have an open row from `loadGenerations` | skipped; `writer=deferred_backfill` refusal count |

Backfill must be fenced explicitly. `latestGenerationCTE`
(`latest_generation_cte.go:41-52`) falls back to the newest generation of any
status, so clearing `active_generation_id` does not hide a retiring scope from
backfill or from freshness resolution. Two other writers are exempt: the
`ingestion_scopes` writers in `vulnerability_suppression_store.go:26` and
migration 115 write non-git scopes.

## Fence Sequence

### Phase 1: mark and fence, one transaction

The API runs this inside its existing 6-minute response window, the same bound
as `recover-generations` (`graph-rebuild-from-facts.md`). The
[lock budget](7766-repository-retirement-concurrency.md#phase-1-lock-budget)
justifies the waits, bindings, and request bound.

1. Read the generation set of every scope with `partition_key = ANY($repo_ids)`.
   The read takes no locks. This mirrors `ReadAffectedGenerations`
   (`refinalize.go:227-271`). Build `reset.Generations`.
   - **Precheck.** Count non-superseded generations and reducer rows matching
     step 7d's predicate. Over 5,000 generations or 20,000 rows in total: 409
     `request_too_large` with per-repo counts.
2. `reset.WaitForReducerDrain` (`refinalize.go:330-366`, 5 min bound) runs
   while the transaction holds no locks.
3. Take `lockstore.AcquireDeferredMaintenanceRepoExclusiveLocks(repo_ids)`
   (`lock/deferred_maintenance.go:66-79`, sorted). The same key is held in
   shared mode by every commit (`ingestion.go:219`), so this waits for in-flight
   commits of these repos and blocks new ones. It runs under
   `set_config('lock_timeout','5min',true)`.
4. Re-read the generation set under the lock. It is now authoritative,
   because no commit for these repos can land. If it grew, run step 2 again. If
   it exceeds the step 1 budget, abort with the same 409.
5. `SELECT ... FROM ingestion_scopes WHERE partition_key = ANY($1) ORDER BY scope_id FOR NO KEY UPDATE`.
   - The scope row is locked first, before any generation or work row (postgres `AGENTS.md`, Ack lock order).
   - NO KEY UPDATE, not UPDATE, keeps FK `KEY SHARE` inserts compatible. A
     concurrent refinalize that holds the table lock and inserts work for this
     scope (`refinalize.go:80-132`) proceeds instead of deadlocking.
6. `set_config('lock_timeout','1s',true)`, then `reset.AcquireReducerClaimFence`
   (`refinalize.go:373-392`): `LOCK TABLE fact_work_items IN EXCLUSIVE MODE`, then
   recheck by `scope_id = ANY`. On `55P03`: roll back, sleep a jittered 100 to
   500 ms, and re-run steps 1 to 8. After a 30 s total budget, return 409
   `blocked` / `claim_fence_busy`.
7. Writes (7f, the shared-lease horizon, was removed; letters are kept so the
   proof outputs still map):
   - (a) `UPDATE scope_generations SET status='superseded', superseded_at=$now WHERE scope_id = ANY AND status IN ('pending','active','failed')`;
   - (b) `UPDATE ingestion_scopes SET active_generation_id = NULL`;
   - (c) projector rows in `pending`/`retrying`, or `claimed`/`running` with an expired lease → `superseded` with `failure_class='repository_retired'`, bound to the step-4 non-superseded `(scope_id, generation_id)` pairs;
   - (d) `DELETE` reducer rows of the scopes (`scope_id = ANY`) in `pending, retrying, failed, dead_letter`, plus expired `claimed`/`running`. The guard is `NOT (status IN ('claimed','running') AND claim_until > clock_timestamp())`, the predicate of `refinalize.go:142-151`. 7d stays in phase 1: after 7b nulls the active pointer, the reducer claim would otherwise pick these rows up;
   - (e) `DELETE FROM repository_reindex_requests WHERE scope_id = ANY`;
   - (g) `INSERT INTO repository_retirements ... ON CONFLICT DO NOTHING` on the open-repo index. An existing open row is returned unchanged, which makes a re-issue idempotent.
8. `reset.AssertRetirementFenced(retired = generations superseded + reducer rows deleted)` (`refinalize.go:401-418`), then commit.

The `rebuild/reset` invariant "never widen the reducer delete past `succeeded`"
protects live leases during a rebuild. Step 7d keeps it: it never deletes a
live-lease row, and it also deletes rows of already-superseded generations that
phase 2d's cascade would delete anyway.

Up to 25 repositories go in one request, all or nothing. That is the request
cap; the precheck is the cost bound. Errors:
- `InflightReducersError`, `55P03` at step 3, and `40P01` → 409 `blocked`, rolled back, no marker;
- `55P03` at step 6 retries, then 409 `blocked` / `claim_fence_busy`; over budget → 409 `request_too_large`;
- the idempotency ledger stays `in_progress`, as in `generations.go:118-122`. Retrying with a new key is safe, because step 7g resumes the same row.

### Phase 2: graph, then bounded purge (reducer runner)

The runner claims rows from `repository_retirements_runnable_idx` with
`FOR UPDATE SKIP LOCKED`, `lease_owner`, and `claim_until` (60s, heartbeat).
Every step persists `phase` and `graph_step_cursor`. The order is
**2b, 2b', 2a, 2c, 2d, 2e**. The first three are specified in
[Shared-Projection Barrier](7766-repository-retirement-concurrency.md#shared-projection-barrier);
the invariants I1 to I4 there are what make the order safe.

- **2b, intent delete, then the barrier:** delete `shared_projection_intents`
  and `shared_projection_unroutable_intents` by `repository_id` and by
  `scope_id`, in chunks of 10,000 rows. Assert zero remain, commit, then set
  `intent_barrier_at` in a later statement. This runs before any acceptance row
  can go (I3), so retirement creates no orphan intents.
- **2b', lease-epoch wait (`state=blocked`, `shared_lease_live`):** until no live
  shared-projection lease was claimed at or before `intent_barrier_at`. A
  pre-barrier cycle has finished its writes by then, and 2c retracts them.
- **2a, projector-lease wait (`state=blocked`, `projector_lease_live`):** no
  projector row of the scopes has `claim_until > now()`. This only converges,
  because nothing new can be claimed after phase 1.
- **2c, graph (`state=repairing_graph`):** see Graph Retraction. Each statement is
  idempotent. The cursor advances after each statement commits.
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
     step cascades `fact_records`, `fact_work_items`, `shared_projection_acceptance`
     (`migrations/011:2-5`), and the rest.
  4. `rows_deleted = rows_deleted + batch` in the same transaction, then commit.
- **2e, repo-keyed leftovers, chunked 10,000 by ctid:**
  - `content_files`, `content_entities`, `content_file_references` `WHERE repo_id=$1`
    (`content_files_repo_path_idx`, `content_entities_repo_idx`, `migrations/004:57-60`);
  - `repository_refs WHERE repo_id=$1` (`004:46-55`);
  - `repository_selection_observations` and `repository_reindex_requests` `WHERE scope_id = ANY`.

### Phase 3: complete, one transaction

1. Lock the scope rows `FOR UPDATE`.
2. Assert no generation remains.
3. `DELETE FROM ingestion_scopes` (ref scopes first). This cascades
   `projector_scope_claim_fences`.
4. Set `state='complete'`, `phase='done'`, `retired_at=now()`, then commit.

**Crash, restart, and failure handling** (batch-level resume, retry classes,
`failed` → `pending` on re-issue) are in
[Runner Crash And Retry](7766-repository-retirement-concurrency.md#runner-crash-and-retry).

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

## Read Surfaces And OpenAPI

The bounded `index_state` is `indexed | stub | retiring | retired`, derived in
this order:
1. an open tombstone in a non-`complete` state → `retiring`;
2. a `complete` tombstone → `retired`;
3. otherwise `evidence_source = 'projector/canonical'` → `indexed`;
4. else `stub`.

Retiring and retired rows also carry `retirement` = {`state` (policy enum),
`retired_at`, `retirement_id`}.

| Surface | Change |
| --- | --- |
| GET `/api/v0/repositories` (`query/repository/handler.go:137-219`) | Graph path: add `r.evidence_source` to the page query and look up tombstones for the page ids only (`WHERE repo_id = ANY($page_ids)`, in a new `retirement_state.go`; the dir carries `//nolint:dirgate`, `doc.go:16`). Content path (`content_reader_repository_catalog.go:16-32` reads `ingestion_scopes WHERE scope_kind='repository'`): `UNION ALL` the complete tombstones that have no scope row. Nothing is dropped from the list; every row is labelled. `total` stays the node count. |
| MCP `list_indexed_repositories` (`mcp/dispatch_repositories.go:74`, routes to the above) | Picks up the fields; the tool description in `tools_codebase.go:227` names `index_state` (mcp-schema-drift). |
| `/api/v0/index-status`, MCP `get_index_status` (`query/status.go:225-245`) | `repository_count` keeps its query. Adds `repository_count_by_state{indexed,stub}` from `MATCH (r:Repository) WITH r.evidence_source = 'projector/canonical' AS idx RETURN idx, count(*)`. Adds a `repository_retirements` block: counts by state, `oldest_pending_age_seconds`, `last_completed_at`. Counts only, no ids. |
| Freshness (`status/repository_freshness.go:183-205`) | New verdict `retired`, rule 0, from a tombstone lookup by `repo_id` that runs before `repositoryFreshnessResolveQuery`. The snapshot gains `Retirement *RepositoryFreshnessRetirement`. |
| Console (`apps/console/src/api/repoCatalog.ts`) | Badge from `index_state`; retired and retiring rows are muted, not hidden. |

OpenAPI changes:
- `paths/repository/freshness.go:40`: add `retired` to the verdict enum, plus a `retirement` object;
- the repositories list schema: add `index_state` and `retirement`;
- the index-status schema: add the new blocks;
- new admin fragments go in `paths/status/repository_retirement.go` (`paths/status/admin.go` is 460 lines).

## Operator Surface

Admin routes live in a new leaf, `query/admin/retirement/`, because the admin
`AGENTS.md` sends new families to their own leaf. The root router wires them.

- `POST /api/v0/admin/repository-retirements` with body
  `{"repositories":[sel,...1..25], "reason":str, "idempotency_key":str, "dry_run":bool}`.
  - **Selectors:** resolved with `MatchRepositories` and `gitDefaultScopeMatch`
    (`reindex_repository.go:55-131`, `content_reader_repository_catalog.go:145-171`
    matches repo id, `scope_id`, name, slug, path, local path). The request is
    all or nothing, and a 400 names every bad selector. A scope id
    disambiguates the duplicate case.
  - **Required fields:** `reason` and `idempotency_key` are mandatory.
    `ClaimReplayIdempotency` uses fingerprint `sha256(sorted scope_ids)`, and
    `CompleteReplayIdempotency` follows the semantics in
    `query/admin/generations.go:84-128`.
  - **Auth:** admin (all-scopes) only. Scoped tokens get 403 (`generations.go:97-101`).
  - **Responses:**
    - 202 `{status:"accepted", retirements:[{retirement_id, repository_id, scope_id, state:"pending", requested_at}], idempotency_key, duplicate}`;
    - 200 dry-run preview;
    - 409 for `blocked` (`inflight_reducers`, `lock_timeout`, `claim_fence_busy`), `request_too_large` (with per-repo counts), key reuse, or in progress.
- `GET /api/v0/admin/repository-retirements/{retirement_id}` and
  `GET /api/v0/admin/repository-retirements?state=&limit=&cursor=` return the
  readback:
  - `retirement_id`, `repository_id`, `scope_id`;
  - `state` (`pending|running|blocked|repairing_graph|complete|failed`), `phase`, `blocked_reason`, `failure_class`, `attempt_count`;
  - `requested_at`, `updated_at`, `retired_at`, `readmitted_at`;
  - `rows_deleted_by_table`;
  - `graph{nodes_deleted, relationships_deleted, repository_node: demoted|absent|readmitted}`.
- **Dry run:** writes no ledger row and no marker, but emits an audit event.
  Per repository it returns:
  - `ref_scope_count` and `generations_by_status`;
  - per-table row counts from cheap index counts by scope/repo key, under a 10s
    `statement_timeout`, with `counts_partial` on timeout;
  - `live_leases{projector,reducer}`;
  - a graph `{file_count, incoming_foreign_edges:[{type, evidence_source, count}]}`
    from anchored reads on `Repository {id}`;
  - `selection.state`, plus `will_readmit:true` when the repo is still `selected`.
    Retirement does not change selection, so the next sync re-admits it.
- **CLI:** `eshu admin repository retire <sel>... --reason --idempotency-key [--dry-run]`,
  `eshu admin repository retirement-status <id>|--state`, and
  `eshu admin repository retirement-candidates`.
  - Request builders go in `internal/cli/admin/retirement.go` and cobra in
    `cmd/eshu/admin.go`, because `cmd/eshu` already has 40 non-test files.
  - With no key given, the CLI generates one, as `replay` does (`admin.go:88`).
- **Tombstone row:** the marker row itself, holding the safe hashes, bounded
  reason code, actor class, counts by table, and lifecycle timestamps. Raw ids
  appear only in admin readbacks, which is where `reindex` already returns them.

## Re-Admission And In-Flight Syncs

The commit gate is folded into the scope upsert: an open non-`complete` row
refuses the commit with `repository_retiring`, and a `complete` row re-admits as
a first generation (`outcome=readmitted_after_retirement`). The gate SQL and the
interleavings are in the
[Commit Gate](7766-repository-retirement-concurrency.md#commit-gate-folded-into-the-scope-upsert)
and
[Re-Admission](7766-repository-retirement-concurrency.md#re-admission-and-in-flight-syncs)
sections of the concurrency contract.

## Candidates

`GET /api/v0/admin/repository-retirements/candidates?limit=(1..200)&cursor=<scope_id>`
is read-only. It never feeds the primitive.

The SQL is a prefilter, and `selection.Summarize`
(`scope/selection/summary.go:74-144`) re-runs on the page's rows (PK reads) as
the authority:

```sql
WITH live AS (SELECT * FROM repository_selection_observations
  WHERE evaluated_at + make_interval(secs => liveness_window_seconds) >= $1),
agg AS (SELECT scope_id, bool_or(state='selected') AS any_sel,
  bool_and(state_cycle_count >= 2 AND evaluated_at - state_since >= interval '5 minutes') AS confirmed,
  max(state_since) AS newest_exclusion FROM live GROUP BY scope_id)
SELECT a.scope_id, a.newest_exclusion FROM agg a
JOIN ingestion_scopes s ON s.scope_id = a.scope_id AND s.scope_kind = 'repository'
WHERE NOT a.any_sel AND a.confirmed AND a.scope_id > $2
  AND NOT EXISTS (SELECT 1 FROM scope_generations g WHERE g.scope_id = a.scope_id
                  AND g.observed_at > a.newest_exclusion)
  AND NOT EXISTS (SELECT 1 FROM repository_retirements r
                  WHERE r.scope_id = a.scope_id AND r.readmitted_at IS NULL)
ORDER BY a.scope_id LIMIT $3
```

## Telemetry

Instruments go in `telemetry/instruments_operator_repository_retirement.go`.
The existing `instruments_repository_retirement.go` is the 7324 counter.

| Signal | Definition |
| --- | --- |
| `eshu_dp_repository_retirements_total{outcome}` | `accepted`, `refused`, `completed`, `failed`, `readmitted_after_retirement` |
| `eshu_dp_repository_retirement_ingest_refusals_total{writer}` | `collector_commit`, `webhook_handoff`, `deferred_backfill`, `recover_generations`, `reindex` |
| `eshu_dp_repository_retirement_step_duration_seconds{phase}` | `fence`, `graph_retract`, `purge_batch`, `finalize` |
| Spans | `admin.repository_retire` and `reducer.repository_retirement_step` (attr `phase`), registered through a contract subpackage step (telemetry `AGENTS.md`) |
| Logs | WARN `repository retirement completed`: `scope_id`, `retirement_id`, `generation_count`, ≤10 `generation_ids` + `generation_ids_hash`, `rows_deleted`, `nodes_deleted`, `relationships_deleted`. WARN `repository ingest refused: retiring` with `writer`. WARN readmitted. |
| Audit | `governanceaudit.EventTypeRepositoryRetirement = "repository_retirement"` beside `audit.go:56`, added to `validEventType` (`:327-342`), `ScopeClassRepository`. Reason codes `repository_retire_{accepted,preview,idempotent_replay,refused_missing_reason,refused_missing_idempotency_key,refused_unauthorized,refused_selector,refused_key_reused,refused_request_too_large,blocked}`. |
| Status | Readback fields (see Operator Surface) and the index-status block (see Read Surfaces And OpenAPI) |

Every row lands in `docs/public/observability/telemetry-coverage.md`, the
`telemetry-coverage` gate, `docs/public/reference/telemetry/metrics-reducer-storage.md`,
`logs.md`, and `traces.md`. The metrics doc entry must distinguish the new
counter from `eshu_dp_canonical_repository_retirements_total`.

## Risks And Rejected Alternatives

**Risks:**
- **Fleet-wide claim pause.** The `EXCLUSIVE` table lock blocks unrelated
  claim, heartbeat, and enqueue statements for the section, as refinalize does.
  Measured (P1 blocking runs, n=10 each, design-form statements): realistic
  5,000-generation scope p50 47.9 ms, max 99.4 ms; stalled scope p50 115.5 ms,
  max 229.1 ms (blocked statements max 230.5 ms). The precheck bounds the work.
- **Barrier waits.** A worker that renews forever holds 2b' in
  `blocked/shared_lease_live` with the lease named, never failing automatically.
  NULL `claimed_at` rows block until their first re-claim. Wait length: P9d.
- **Retry, snapshot, gate shape.** A step 6 retry restarts step 2's drain wait.
  The precheck is a snapshot, and step 4 is authoritative. The gate moves
  `upsertIngestionScope` from `Exec` to `Query`; `LIMIT 1` relies on the
  partial unique open-repo index.
- **Retired id lingers in the ingester catalog.** The catalog read covers every
  `repository` fact (`ingestion_queries.go:19-24`), and the in-process catalog
  cache may hold a retired id until restart. Other repos' evidence then names
  it and produces truthful stubs.
- **Name confusion with the 7324 counter.** The new `eshu_dp_repository_retirements_total` sits beside `eshu_dp_canonical_repository_retirements_total`, which counts the path-conflict delete. The metrics doc must keep them apart.
- **Retiring a still-selected repo bounces it back.** The dry run warns.

**Rejected:**
- **`ingestion_scopes.status` as the marker.** It is overwritten on every upsert.
- **A predicate in `claimProjectorWorkQuery`.** It is a hot path, and superseding the generations already fences the projector.
- **The shared-lease horizon, or claiming every shared partition.** The first
  is unsound under heartbeats (P9H), and the second pauses all shared
  projection fleet-wide. A claim epoch replaces both.
- **Dropping 7c, or a separate gate statement.** The first moves an unmeasured
  lock sweep into an unrelated claim; the second fails the 1% bar.
- **DETACH DELETE, or a conditional delete, of the Repository node.** See Graph Retraction.
- **Postgres purge before graph.**
  - G5 needs `content_files` paths, and G3 needs scope ids.
  - A permanent graph failure after the purge would leave a graph that Postgres
    cannot account for (`graph-rebuild-from-facts.md:55`).
  - The policy's privacy-first order is a stated non-goal here.
- **Graph retraction in the API.** The API holds no write credential.
- **A new per-repository lock, or serialising workers.** The existing advisory key already linearises commits.
- **Deleting webhook trigger rows.** They are delivery history.
- **A single-transaction purge.** It is unbounded.

## Open Questions

- Should the other per-repository routes (context, story, stats) return the
  policy's tombstone envelope instead of 404 after completion? A proposed
  follow-up, not required by the ruling.

## Non-Goals

These follow the arbiter ruling: no auto-retire, no selection change, no policy
engine, no tenant offboarding, no privacy guarantee beyond the proof, no
retention sweep, and no observation-row sweep (#7774).
