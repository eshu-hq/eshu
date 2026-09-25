# Workload Identity: Migration, Rebuild, And Consumer Impact

Status: decided 2026-09-25 for issue #5385, companion to
[5385-workload-identity-compatibility.md](5385-workload-identity-compatibility.md),
which holds the identity model, the SDK contract, and the decision record.
This document carries the delete set, the ordered cutover, the probes that
must run before implementation, the required tests, consumer impact, and
telemetry. Measured numbers cite `docs/internal/measurements.jsonl` rows.

## 1. Measured basis, and which build it was measured on

The 5385 ledger rows were taken on the **pr290 build**
(`backend_version` `eshu-nornicdb-pr290:3722b483c02c`, the 1.2.1 line). The
**current pin** is `fix-500-e022384c`, which reports `NornicDB v1.3.3`
(`docker-compose.yaml`, `docker-compose.live-backend-nornicdb.yml`;
`docs/public/reference/nornicdb-pitfalls.md` records the version-string
history). Wherever this document says "measured", it means pr290 unless a
probe below has re-run it on fix-500.

- Node-level `DETACH DELETE` clears every edge family on both backends
  (`ledger:5385-retract-rebuild-node-detach-delete`). This is the one result
  the migration rests on, and it holds on Neo4j and on the measured NornicDB
  build alike.
- Relationship retract was inert on pr290
  (`ledger:5385-retract-rebuild-relationship-inert`). Its behaviour on
  fix-500 is **unverified on the current pin**; probe P1 settles it for all
  six measured shapes plus the cloud-`USES` scope-wide retract. Nothing in the
  cutover depends on the answer, but the staleness path in the compatibility
  doc (§2.3) chooses between a relationship retract and an instance rebuild on
  it, and if retract is inert the stale-`USES` hazard is recorded as open or
  the writer retracts by node.
- The 908-repository graph held 40 `Workload` and 33 `WorkloadInstance`
  nodes (key doc §3.1, unledgered). That is the population of the deleted
  labels, not a statement about rebuild cost: re-materialization re-runs
  every writer in §4 for every scope, and its wall time is unmeasured. Probe
  P3 measures it and a time bound is stated before the first live run.

## 2. Delete set and cascade

**Labels deleted wholesale in the cutover:** `Workload`, `WorkloadInstance`,
and `Endpoint`. `Endpoint` is in the set because its id is
`sha256(repoID|workloadID|path)` (`stableAPIEndpointID` in
`go/internal/reducer/projection_helpers.go`), so every endpoint id moves with
the workload id, and **no Endpoint retract exists** — an endpoint left behind
under the old digest would be an orphan that nothing ever reaps. `SAME_NAME`
edges need no entry: they are removed with their endpoints.

**Every production writer anchored on `:Endpoint`** (the §4 search, filtered
to `Endpoint`): the node `MERGE` and both `EXPOSES_ENDPOINT` templates in
`go/internal/reducer/workload_materializer.go` (workload materialization), and
`BatchCanonicalHandlesRouteEdgeUpsertCypher` in
`go/internal/storage/cypher/canonical_handles_route_edges.go`, which `MATCH`es
`(e:Endpoint {repo_id: row.repo_id, path: row.path})` and is dispatched by the
shared-projection domain `handles_route`, whose intents the
`code_call_materialization` claim domain emits (`BuildIntentRows` in
`go/internal/reducer/code/call/materialization/refresh.go`, written through
`IntentWriter.UpsertIntents`). `HANDLES_ROUTE` is anchored by `(repo_id,
path)`, not by the id that moves, so it is deleted with the node and comes back
only when its completed intents are reopened and drained again (§3 step 7); it
is row 4 of §4. The Postgres presence rows that gate it and `runs_in`
(`graph_endpoint_presence` keyspaces `api_endpoint_repo_path` and
`repo_workload`, keyed by `(repo_id, path)` and by `repo_id`) do not move and
survive the wipe; the #6184 write-target probe in
`go/internal/storage/cypher/edge/writer/unroutable.go` defers a batch whose
node is absent instead of completing it, so between steps 5 and 7 those edges
are deferred, not lost. Every other `:Endpoint` pattern in production Go is a
query reader (`go/internal/query/codequery/route_handlers.go`, `outlier.go`,
`repository/api_surface.go`, `impact/contract.go`) and is consumer impact, not
a writer.

**Cascade on repository cleanup — scoped to retirement, never to
regeneration.** `buildRepositoryCleanupStatements`
(`go/internal/storage/cypher/canonical_node_writer_phases.go`) dispatches two
templates on **every** generation of an existing repository that is neither
`FirstGeneration` nor `DeltaProjection`:
`canonicalNodeRepositoryIDCleanupCypher` (`MATCH (r:Repository {id: $repo_id})
DETACH DELETE r`) and `canonicalNodeRepositoryPathCleanupCypher`
(`MATCH (r:Repository {path: $path}) WHERE r.id <> $repo_id DETACH DELETE r`).
The id template is the ordinary regeneration path: it drops the repository
node, the upsert that follows recreates it, and the generation's own phases
and domains rewrite its edges. Cascading there would delete the repository's
`Workload`, `WorkloadInstance`, and `Endpoint` nodes on every regeneration,
and with them every edge written by a domain that does not re-run for that
generation — `USES` from an AWS scope, `DOCUMENTS` from a documentation scope —
until that scope happened to re-materialize. That is a loss, not cleanup, so
the id template stays as it is.

Only the path template knows a retired identity: the `r.id` values it matches
belong to a repository whose path was re-onboarded under a new `repo_id`
(`CanonicalRepositoryID` mints a new one for the new identity), which is the
rename case. The cascade runs on that condition and nothing else. Statement
order is part of the design: `canonicalNodeRepositoryPathCleanupCypher`
`DETACH DELETE`s the retired repository, after which its `r.id` is unrecoverable, so
the three cascade statements are ordered before the path template inside
`buildRepositoryCleanupStatements`. That function returns static `Statement`
values (`Operation`, `Cypher`, `Parameters` in
`go/internal/storage/cypher/writer.go`) and no executor feeds one statement's
rows to the next, so a separate `RETURN r.id` read followed by an
`UNWIND $retired_ids` delete would need a new mechanism. Each delete therefore
derives the retired ids itself, one combined statement per label:
`MATCH (r:Repository {path: $path}) WHERE r.id <> $repo_id MATCH (n:<Label>
{repo_id: r.id}) DETACH DELETE n`. It reads only `Repository` nodes the path
template has not yet deleted, so it depends on no earlier write in the phase.
`Workload.repo_id` and `WorkloadInstance.repo_id` are indexed
(`workload_repo_id`, `workload_instance_repo_id` in
`go/internal/graph/schema_tables_indexes.go`); `Endpoint` has only its `id`
index, so the `Endpoint` leg is a label scan until an `Endpoint.repo_id` index
is measured under probe P3. A repository dropped from its scope without a path
conflict has no projector signal today (`rg -i 'repositor(y|ies)[_ ]?(removed|
retired|retire|deleted)' -g '!*_test.go'` over `go/internal/projector`,
`go/internal/reducer`, and `go/internal/storage/cypher` matches nothing; without
the test exclusion it hits three `orphan_sweep_*_test.go` failure messages, none
a signal); its owned nodes stay, as the
shared node stays today, and an explicit removal signal is a follow-up on the
issue, not part of this cutover. Without the cascade a rename would leave a
full set of orphaned per-repo nodes under the retired id, with their
`SAME_NAME` edges still pointing at live nodes.

The cascade cannot land under the old key. With a name-only id a colliding
workload is one shared node whose `repo_id` is the last writer's, so a
`repo_id`-scoped delete would remove the other repository's node — the
`mutations.go` hazard the issue already retracted. It therefore ships in step
6 with the constructors, in the same binary, and never runs while the old key
is emitted. Replaying the affected domains per generation instead was
rejected: claims are partitioned by scope, so a repository's generation cannot
re-enqueue another scope's `USES` or `DOCUMENTS` writer without a new
cross-scope enqueue mechanism, and the retirement-scoped cascade needs none.

## 3. Ordered cutover (Decision D4: one-shot, drained)

**Why one-shot.** During a coexistence window, exact-id lookups (rung 1 of the
ladder) resolve the stale old nodes, per-label counts and the B-12 floor and
ceiling checks double, and `DEPENDS_ON` rows straddle the two shapes, so a
dependency trace can start on a new node and end on an old one. Those are
accuracy failures, not housekeeping. The migration deletes nodes because node
`DETACH DELETE` is proven on both backends and on the measured build; it does
not depend on relationship-retract behaviour, which is unverified on the
current pin.

**Steps.**

1. **Land the code with the old key still emitted**: handle ladder in query
   and MCP, `SAME_NAME` writer, `scope_id`/`generation_id` stamps, the SDK
   minor bump with fixtures, the reducer resolution step and the
   `workload_handle_ambiguous` skip, telemetry (§8), and the read-side
   conversions. The repository-cleanup cascade is **not** in this step: under
   the old key it would delete a colliding sibling (§2). What this step can prove:
   the collision detectors, because a two-repository Odù under the old key
   still produces one node and the counters fire. What it **cannot** prove:
   the `SAME_NAME` writer and the handle ladder, because under the old key
   there is only one node with a name. Those are proved in step 9.
2. **Hold the claim domains** in §4: every row marked `claim`, which
   includes `code_call_materialization`, `code_import_repo_edge`, and
   `package_source_correlation`, the emitters of the side-runner rows. The
   mechanism is lane reconfiguration, not a pause switch:
   `ESHU_REDUCER_CLAIM_DOMAINS` (`loadReducerClaimDomains` in
   `go/cmd/reducer/config.go`) sets `ReducerQueue.ClaimDomains`, an
   **include-list** that `claimDomainFilters`
   (`go/internal/storage/postgres/reducer_queue_helpers.go`) applies to the
   claim SQL, and a lane with it unset claims every domain. Holding means
   restarting every reducer lane with an explicit list that omits those
   domains; releasing, in step 7, means restoring the list. The include-list
   reaches nothing else. The two side runners (§4) are goroutines of every
   reducer process, take no domain list, and floor their worker count at one
   (`LoadConfig` in `go/internal/reducer/intents/shared/worker/config.go`
   rejects zero; `loadRepoDependencyProjectionWorkers` in
   `go/cmd/reducer/config_projection.go` accepts only 1, 2, or 4), so no
   configuration holds them. They go quiet by construction instead: both
   select only `shared_projection_intents` rows with `completed_at IS NULL`
   (`ListPendingDomainIntents`), so once their emitters are held and the
   backlog has drained (step 3) they have nothing to run until step 7 reopens
   it.
3. **Drain** both queues for the held set: every `fact_work_items` reducer
   row of a held domain leaves `claimed` and `running`, and the
   `shared_projection_intents` backlog for `handles_route`, `runs_in`, and
   `repo_dependency` reads zero outstanding and zero in flight, as
   `domain_backlogs` on `GET /api/v0/status` with an all-scopes token (the
   `shared_projection_pending` and `shared_projection_active_leases` reads in
   `go/internal/storage/postgres/status_queries.go`), or directly
   `SELECT projection_domain, count(*) FROM shared_projection_intents WHERE
   completed_at IS NULL GROUP BY 1`. The held set includes the service-catalog
   correlation domain, whose `service_evidence_key` embeds the instance id.
   **Read outage note:** while drained and until step 8 completes, every
   workload-rooted read (`/workloads/{id}/context` and `/story`,
   `/compare/environments`, runtime topology, `/catalog` workload rows, impact
   traces through a workload) returns empty or 404. The runbook announces the
   window and its bound before step 4.
4. **Retire durable rows keyed on the old ids, using the old ids**:
   `service_evidence_key` rows (`ServiceRuntimeEvidenceKey` embeds the
   instance id; key doc §6 item 8) and the persisted search handles
   (`go/internal/searchdocs/project.go`). Postgres and the search index
   **coexist** with the graph between this step and step 8: findings rows
   keep old handles in `workload_ids` until their generation regenerates, and
   search returns old handles until reindexed. Both are read through the
   ladder, so they resolve (or fail closed) rather than silently miss.
5. **`DETACH DELETE` by label**: `Workload`, `WorkloadInstance`, `Endpoint`.
   Not by old id — an edge that escaped an id-anchored delete would stay
   forever. Verify with a settled count read (the ledger rows record the
   stale-read trap on this backend).
6. **Flip the constructors** in `go/internal/workloadid` and roll the binary,
   with the retirement-scoped repository-cleanup cascade (§2) in the same roll
   so it never executes under the old key; update the pinned-format tests
   deliberately. This is the only irreversible edit in the sequence, and it is
   a code roll.
7. **Reopen and re-run every domain in §4 with one request.** Restore the
   lane lists from step 2, then `POST /api/v0/admin/recover-generations` with
   `all_scopes: true`, a `reason`, and a fresh `idempotency_key` (admin token;
   `recoverGenerationsRequest` in `go/internal/query/admin/generations.go`).
   This is the rebuild-from-facts path
   (`docs/public/operate/graph-rebuild-from-facts.md`):
   `RecoveryStore.RefinalizeScopeProjections`
   (`go/internal/storage/postgres/recovery.go`) reads the active generation of
   every active scope, waits for live reducer leases, re-enqueues projector
   work, and in the same transaction runs `reset.ApplyPreRetirement`
   (`go/internal/storage/postgres/rebuild/reset`), which deletes the
   `succeeded` reducer work so every claim domain re-runs, clears
   `completed_at` on those generations' `shared_projection_intents` so the
   side runners drain `handles_route`, `runs_in`, and `repo_dependency` again,
   and drops their `graph_projection_phase_state` rows so the readiness gates
   re-arm; it then supersedes the active relationship generations.
   Re-enqueuing the emitting claim domains would not do this on its own:
   `sharedintent.Build` derives `intent_id` from the acceptance unit,
   generation, partition key, domain, repository, scope, and source run, so a
   re-run reproduces the same ids, and `SharedIntentStore.UpsertIntents`
   keeps the existing `completed_at` on conflict, so the `HANDLES_ROUTE` and
   `RUNS_IN` intents would stay completed and the edges deleted in step 5
   would never return. No admin surface re-enqueues a single reducer domain;
   the smallest unit is a scope's active generation, so every domain re-runs,
   not only §4's. The §4 order is enforced by the runtime, not issued by the
   operator: the `handles_route` and `runs_in` presence rows survived step 5,
   so their batches are held by the presence gates and the #6184 probe until
   workload materialization has recommitted the nodes (§2). Old-shape ids
   never coexist with new-shape ids in the graph.
8. **Regenerate the golden artifacts** (§7), re-prove the
   `FetchWorkloadRuntimeTopology` plan pin (probe P4), and reindex search.
9. **Run the two-repository Odù against the new key** (probe P5): counters at
   zero, two nodes, one `SAME_NAME` edge, per-family edge counts equal to the
   pre-cutover baseline on the collision-free corpus.

**Rollback.** Roll the old binary and issue the step 7 `recover-generations`
request again with a fresh `idempotency_key`: the old key regenerates the old
nodes and edges from facts. Of the Postgres rows retired
in step 4, `service_evidence_key` rows **regenerate** from facts when the
service-catalog runtime domain re-runs; the search handles do not regenerate
from any §4 domain — `eshu_search_index_documents` is **reindexed** by
re-running `eshu_search_document` (§4 row 11), which the same request covers:
the same reindex as step 8, under the old binary. The delete in step 5 is not
the point of no return; facts are. Rollback after step 8 additionally reverts
the golden regeneration.

Step 1 is its own PR; 2–9 are one PR with one operator runbook and one stated
time bound.

## 4. Writers to re-enqueue, and the per-family count assertion

The delete set (§2) and the re-enqueue set are derived from the production
files whose Cypher binds a `Workload`, `WorkloadInstance`, or `Endpoint` node
pattern, at `2ae147cf9`:

```sh
rg -l --type go -g '!*_test.go' -g '!go/internal/query/**' \
  -g '!go/internal/backendconformance/**' \
  -e '^\s*(UNWIND .*)?(OPTIONAL )?(MATCH|MERGE) .*\(\w*:(Workload|WorkloadInstance|Endpoint)\b' \
  go/internal go/cmd
```

It lists 14 files. `go/internal/query` is excluded because its readers are
consumers (§7), and `backendconformance` because it is a test corpus. Three
files are outside the search and named by hand: the #6184 write-target probe in
`go/internal/storage/cypher/edge/writer/unroutable.go` and the Ifá gate in row
8 build their pattern with `fmt.Sprintf`, and
`go/internal/reducer/service_runtime_instance_lookup.go` (row 9) opens its
Cypher constant on the `const` line, which the line-anchored regex misses.

The files group by the domain that dispatches them and by how that domain
runs. `go/internal/reducer/contract/domain.go` declares two kinds of name: the
first `const` block is the claimable `Domain` set, which the reducer claim loop
runs from `fact_work_items` and `ESHU_REDUCER_CLAIM_DOMAINS` can hold; the
second block is the shared-projection names, reached two ways. **Side-runner**
rows are emitted by a claim handler into `shared_projection_intents` through
`SharedIntentStore.UpsertIntents`
(`go/internal/storage/postgres/shared_intents_upsert.go`) and drained by a
long-lived runner: `worker.Runner`
(`go/internal/reducer/intents/shared/worker/runner.go`,
`sharedProjectionDomains`) for `handles_route` and `runs_in`, and
`RepoDependencyProjectionRunner`
(`go/internal/reducer/repo_dependency_projection_runner.go`) for
`repo_dependency`; `reducer.Service` starts both in every reducer process
(`go/internal/reducer/service_side_runners.go`). **Inline** rows are built by a
claim handler and written in the same call through the edge writer under the
shared name, never queued: `workload_dependency` and `documentation_edges`.
Every `UpsertIntents` caller in the tree was checked at `2ae147cf9`; none emits
those two names, so the runner's entries for them drain nothing. The order
below is the dependency order the runtime enforces in step 7, not a sequence
the operator issues:

| Order | Domain (kind; runner and emitter) | Files from the search | Families |
| --- | --- | --- | --- |
| 1 | `workload_materialization` (claim) | `go/internal/reducer/workload_materializer.go`, `workload_materialization_repo_phase.go`, `workload_materializer_retract_instances.go`, `go/cmd/reducer/workload_instance_retraction_lookup.go` (retract reader), `go/internal/storage/cypher/canonical.go` | `Workload`, `WorkloadInstance`, `Endpoint`, `Platform` nodes; `DEFINES`, `INSTANCE_OF`, `EXPOSES_ENDPOINT`, `DEPLOYMENT_SOURCE`, `SAME_NAME`, and `RUNS_ON` (`batchRuntimePlatformRunsOnEdgeUpsertCypher` in `workload_materializer.go`, the edge's owning writer) |
| 2 | `workload_dependency` (inline in row 1: `workload_materialization_handler.go` calls `ReconcileWorkloadDependencyEdges`, then `WorkloadDependencyEdgeWriter.WriteEdges`; no queued row, so holding row 1 holds this) | `go/internal/storage/cypher/canonical.go` (`BatchCanonicalWorkloadDependencyUpsertCypher`), `go/cmd/reducer/workload_dependency_lookup.go` (retract reader) | `DEPENDS_ON` |
| 3 | `code_call_materialization` (claim; the emitter of rows 4 and 5: `materialization.Handler` in `go/internal/reducer/code/call/materialization/handler.go` appends `BuildIntentRows` and calls `IntentWriter.UpsertIntents`) | none of its own; its Cypher is rows 4 and 5 | see rows 4 and 5 |
| 4 | `handles_route` (side runner `worker.Runner`; presence-gated on `api_endpoint_repo_path`; #6184 probe) | `go/internal/storage/cypher/canonical_handles_route_edges.go` | `HANDLES_ROUTE` |
| 5 | `runs_in` (side runner `worker.Runner`; presence-gated on `repo_workload`; #6184 probe) | `go/internal/storage/cypher/canonical_runs_in_edges.go` | `RUNS_IN` |
| 6 | `repo_dependency` (side runner `RepoDependencyProjectionRunner`; emitted by the `code_import_repo_edge` and `package_source_correlation` claim domains through `RepoDependencyIntentWriter.UpsertIntents` in `code_import_repo_edge_handler.go` and `packages/correlation/source_handler.go`; the producer of its `RUNS_ON` rows is open, probe P7) | `go/internal/storage/cypher/canonical_relationships.go` | `RUNS_ON` (writer leg) |
| 7 | `documentation_materialization` (claim; writes the `documentation_edges` family inline through `EdgeWriter.WriteEdges` in `documentation_edge_materialization.go`; no queued row; no #6184 probe, §7) | `go/internal/storage/cypher/canonical_documentation_edges.go` | `DOCUMENTS` |
| 8 | `workload_cloud_relationship_materialization` (claim) | `go/internal/storage/cypher/workload_cloud_relationship_writer.go`, `go/internal/reducer/workloadinstance/lookup.go`; gate `go/internal/ifa/materializededges/workload_cloud_relationship.go` (asserts by id; outside the search) | `USES` |
| 9 | service-catalog correlation (claim domain `service_catalog_correlation`; its runtime family is `GraphServiceRuntimeInstanceLoader`, wired in `go/cmd/reducer/main.go` and gated on `ServiceMaterializationWriter` in `go/internal/reducer/defaults_additive_domains.go`) | `go/internal/reducer/service_runtime_instance_lookup.go` (outside the search) | `service_evidence_key` rows |
| 10 | `code_value_flow_refresh` (claim) | `go/internal/reducer/code/value/cloud_sink_loader.go`, `go/internal/reducer/code/value/affected/gate.go` | `INVOKES_CLOUD_ACTION` / `RUNS_IN` readers |
| 11 | `eshu_search_document` (claim) | none (Postgres, not Cypher): `GraphHandle{Kind: "workload"}` in `go/internal/searchdocs/semantic_context.go` | `eshu_search_index_documents` handles, the step 8 reindex |

Row 1 must complete before rows 4 and 5, whose presence gates and the #6184
probe bind against the recommitted nodes (§2); step 7's phase re-arm is what
makes those gates hold instead of answering for the wiped graph. The rest
follow in the listed order. The list is derived from the tree by the command
above, and the re-key PR re-runs it. It is still not self-proving: **the only
proof the list is complete is the per-family count assertion below.**

**Per-family count assertion** (in the Odù and the runbook): before step 5 and
after step 9, on the collision-free golden corpus, the counts of
`HANDLES_ROUTE`, `RUNS_IN`, `DOCUMENTS`, `INSTANCE_OF`, `RUNS_ON`, `USES`,
`DEPLOYMENT_SOURCE`, `DEPENDS_ON`, and `EXPOSES_ENDPOINT` edges must be equal,
`SAME_NAME` must read zero both times, and `Workload`, `WorkloadInstance`,
and `Endpoint` node counts must be equal. Any drift means a writer was missed
or over-reached; the equality is the proof the re-key changed identity and
nothing else.

## 5. How RUNS_ON and cloud USES scope after the re-key

**`RUNS_ON` scopes by construction.** `canonicalRunsOnUpsertCypher` walks
`(repo {id})-[:DEFINES]->(w)` then `(i)-[:INSTANCE_OF]->(w)`. With a per-repo
key the first hop reaches exactly one `w`, so the second reaches only that
repository's instances; no `i.repo_id` predicate is required, and none is
added, so the plan does not move. The retract pair
(`RetractRepoRunsOnEdgesCypher`, `RetractSingleRepoRunsOnEdgesCypher`) is the
same traversal and inherits the same scoping. Probe P5 asserts one `RUNS_ON`
per instance to its own platform.

**Cloud `USES` scopes by resolution.** `WorkloadCloudRelationshipUpsertCypherFormat`
stays as written. What changes is upstream: `row.workload_id` is the output of
the compatibility doc's ladder, so it is either one full id or the row was
skipped as `workload_handle_ambiguous`. The writer can no longer fan one
resource onto every tenant's instance of a name.

`go/internal/graph/mutations.go` no longer exists on `main`: it had no callers,
was retracted on the issue, and has been removed
(`rg ResetRepositorySubtreeInGraph go/` is empty at `2ae147cf9`).

## 6. Probes before implementation, and required tests

Each probe is a theory check under the repo's prove-the-theory rule. None has
run; each records a ledger row when it does.

| Probe | What it settles | Pass shape |
| --- | --- | --- |
| P1 | Relationship `DELETE` effectiveness on `fix-500-e022384c` for all six shapes the pr290 ledger row measured (bare `DELETE`, `RetractSingleRepoRunsOnEdgesCypher`, `DEFINES`, `DEPENDS_ON`, `RUNS_IN`, `DOCUMENTS`) plus the cloud-`USES` scope-wide retract (`RetractWorkloadCloudRelationshipEdgesCypher` as `commitEdges` dispatches it in `workload_cloud_relationship_materialization.go`), Neo4j control, settling loop. Until it runs, both documents say "unverified on the current pin". | 1 → 0 on both backends; if any shape is inert, the stale-`USES` hazard is recorded as open on the issue or the writer retracts by node |
| P2 | `PROFILE` of the Go-paired `SAME_NAME` `MERGE` (compatibility doc §1.3) against the rejected inline-plus-`WHERE` shape, and of one handle lookup, with the `workload_name` index present | the paired shape seeks the index and writes the expected rows; the rejected shape is recorded as the pitfall predicts |
| P3 | `DETACH DELETE` of the three labels plus the step 7 `recover-generations` re-run: wall time on the 908-repo store at the current pin, with the §4 per-family count assertion on the result. The rebuild runbook's latest measured clean run (`docs/public/operate/graph-rebuild-from-facts.md`, "What the rebuild does not restore") came back short one `WorkloadInstance`, its `Platform`, and four relationships, which is inside the labels this cutover deletes and is unexplained | exact seconds and a human duration, with the stated bound; every family count equal, or the delta root-caused before the live run |
| P4 | Re-prove the `FetchWorkloadRuntimeTopology` pin (`go/internal/query/entity/workload_runtime_topology.go`; `go/internal/queryplan/testdata/hot-cypher.yaml`, whose caveats retain the #5272 "about 75 times slower" repository-first finding and the Neo4j-only `NodeIndexSeek` proof) under the new `workload_id` values | pin updated; the `WorkloadInstance.workload_id` anchor still seeks; the 75x finding re-measured, not assumed |
| P5 | The two-repository Odù from the compatibility doc §2.4 on the new key | one `RUNS_ON` per instance, zero duplicate `Endpoint` nodes, one `SAME_NAME` edge, zero handle edges, per-family counts equal |
| P6 | `SAME_NAME` concurrent-`MERGE` contention: two workers writing the same ordered pair, at least 20 trials on the current pin, settled reads, retry observed | exactly one edge after every trial and an idempotent retry. **Gates the edge-versus-query-time choice:** duplicates in any trial make `SAME_NAME` gauge-only, with siblings computed at query time from the `workload_name` index under the grant filter |
| P7 | Which producer emits a `RUNS_ON`-typed `repo_dependency` intent. The writer leg exists (`repoDependencyRunsOnRows` in `go/internal/reducer/repo_dependency_projection_replay.go`; the `RunsOn` branch of `buildRowMap` in `go/internal/storage/cypher/edge/writer/writer.go`), but both emitters in the tree (`code_import_repo_edge.go`, `packages/correlation/consumption_repo_edge.go`) set `relationship_type` to `DEPENDS_ON`, and no other producer was found at `2ae147cf9`; this is not settled from code | `SELECT count(*) FROM shared_projection_intents WHERE projection_domain = 'repo_dependency' AND payload->>'relationship_type' = 'RUNS_ON'` on the 908-repo store. Zero: §4 row 6 is a dead leg and `RUNS_ON` belongs to row 1 alone. Non-zero: name the producer and add it to the held set in step 2 |

Required tests, each RED before its change and GREEN after:

| Test | Contract |
| --- | --- |
| T1 scoped-token isolation | A token granted only `alpha` reading `/catalog`, the entity map, and the change surface for `checkout` sees `same_name_siblings` = 0 and no `beta` `repo_id` anywhere in the response body. Requires an audit of every untyped or variable-length traversal that starts at `Workload` (`-[*]-`, `-[]-`, `-[:A|B]-` lists) so none can reach a sibling without the grant predicate. |
| T2 concurrent writers | The P6 shape as a permanent RED/GREEN: two scopes materializing same-named workloads concurrently on a live backend end with exactly one `SAME_NAME` edge for the pair, asserted by a settled read; the UNIQUE-conflict retry is observed, not assumed. |
| T3 matching semantics | For suppression, service-catalog links, AWS attributes, and documentation mentions (`target_entity_id`): full id resolves; unique handle resolves; a handle spanning two repositories applies to neither, links nothing, writes no edge, and is counted. |
| T4 repository cleanup cascade | **Retirement case:** re-onboarding a repository's path under a new `repo_id` (the path-conflict template) deletes the owned `Workload`, `WorkloadInstance`, and `Endpoint` nodes under the retired id; the sibling's nodes and its other `SAME_NAME` edges survive. **Regeneration case:** the same repository regenerates (a non-first, non-delta generation, so `canonicalNodeRepositoryIDCleanupCypher` runs) and its `Workload` nodes survive together with a `USES` edge written by an AWS-scope generation and a `DOCUMENTS` edge written by a documentation-scope generation; only `DEFINES` is dropped and rewritten. Both asserted by settled reads on the live backend. |
| T5 handle ladder | Exact id, unique handle, ambiguous handle with candidates, miss — across API, MCP, and reducer, one test table. |

## 7. Consumer impact

| Consumer | Impact | Disposition |
| --- | --- | --- |
| `GET /api/v0/workloads/{workload_id}/context`, `/story` | exact `w.id` match, 404 on miss | accept the handle via the ladder; add a `409`-class ambiguous response listing candidate `repo_id`s |
| `POST /api/v0/compare/environments` (`workload_id` body) | exact match, no name fallback | same ladder; `repo` optional narrowing |
| `FetchWorkloadRuntimeTopology` | pinned plan, `workload_id` anchor | probe P4 |
| `/catalog` read model (`go/internal/query/repository/catalog.go`, `content_reader_repository_catalog.go`, `repository_read_model_summary.go`) | merges rows by bare name; summary counts by name | key by `(repo_id, name)`; `same_name_siblings` counts admitted siblings only (D2) |
| supply-chain impact path and runtime context (`go/internal/query/supply/chain/impact/path.go`, `supply/chain/impact/runtime_context_store.go`) | carry `workload_ids` and resolve them to nodes | ladder on read |
| `reducer_supply_chain_impact_finding.workload_ids` readers using jsonb containment (`fact.payload->'workload_ids' ? $selector` in `go/internal/query/sbom_attestation_attachments.go`, `sbom_attestation_attachment_aggregates.go`, `supply/chain/advisory/evidence_sql.go`; the `supply_chain_impact_canonical_winners.workload_ids` GIN column read by `supply_chain_impact_canonical_winners_store.go`) | a selector holding an old handle matches nothing once rows regenerate | route the selector through the ladder before the SQL: resolve to full ids, then contain on those; an ambiguous handle fails closed |
| Documentation mentions: `BatchCanonicalDocumentationWorkloadEdgeCypher` (`go/internal/storage/cypher/canonical_documentation_edges.go`), fed by `CandidateRefs[0].ID` in `go/internal/reducer/documentation_edge_materialization.go` | exact `MATCH (target:Workload {id: row.target_entity_id})`; after the re-key a handle-form id matches nothing and the `MERGE` writes no edge, silently — this domain has no #6184 probe | resolve `target_entity_id` through the ladder before the write: full id or unique handle → `DOCUMENTS` edge; ambiguous → skipped as `workload_handle_ambiguous`, counted on `surface="reducer"`; the in-tree extractor's known-entity catalog (`go/internal/doctruth/extractor.go`) is fed full ids (T3) |
| Ifá materialized-edge gate (`go/internal/ifa/materializededges/workload_cloud_relationship.go`) | asserts `USES` endpoints by workload id | regenerate with the Odù |
| MCP `service/context` selectors | first-colon cut | handle forwarded whole; ambiguity surfaces in the `truth` envelope |
| Console (`ExplorerPage.tsx`, `eshuGraphDeploymentModel.ts`, `VulnerabilitiesReachable.tsx`, `ImpactDeploymentSummary.tsx`, `ExposureServiceSelector.tsx`) | build, display, bookmark, or prompt for handles | unchanged code; new ambiguity state in the selector UI |
| CLI `eshu report --scope workload:<name>` | label parser | unchanged: it takes the handle |
| Cassettes | 14 `"workload:` matches in 5 files under `testdata/cassettes/` (`awscloud`, `workloaddependency`, `kuberneteslive`, `repodependency`, `symbolruntime`), no instance literals — `rg -c` at `2ae147cf9`; 13 are `Workload` ids and move, and one (`"workload_object_id": "workload:claim-honesty-demo"` in `kuberneteslive/supply-chain-demo.json`) keys a `KubernetesWorkload` and stays; the key doc's §3.3 count predates four of those files | regenerate the 13 in the migration PR per `eshu-golden-corpus-rigor` |
| B-12 snapshot `testdata/golden/e2e-20repo-snapshot.json` | 32 `"workload:` occurrences over 5 names and 6 `"workload-instance:` over 2 ids, including asserted `workload.id` / `left.instance.id` values | regenerate; the corpus has no collisions, so node and edge counts must be unchanged (§4) |
| #5384 / #5167 query-time predicates | family 1 (direct `repo_id`) becomes exact; family 5 (`DEFINES`-collision inline-map, the family list in `go/internal/query/infra_scope.go`) becomes structurally unreachable | keep family 5 as a fail-closed guard until the collision counters have read zero for one release, then retire it with its own RED/GREEN |
| `get_service_changed_since` | `service_evidence_key` embeds the instance id | one generation of retired-plus-added rows, announced in the runbook; never collapsed into unchanged |
| Search index | persisted `GraphHandles{Kind:"workload", ID}` | reindex (step 8) |
| `reducer_workload_identity` Postgres `entity_keys` | legacy name keys | preserved (Q7); the graph handle is derived at repo-aware seams |

## 8. Collision telemetry an operator can use

| Signal | Type | Labels | 3 AM question it answers |
| --- | --- | --- | --- |
| `eshu_dp_workload_identity_collision_total` | counter | `mode` (`same_scope_drop`, `cross_scope_merge`) | "Did two repositories contend for one identity?" Before the flip it measures the defect; after it, any non-zero is a leaked key |
| `eshu_dp_workload_identity_rejected_total` | counter | `reason` (`blank_segment`, `colon_in_name`, `colon_in_environment`) | "Why is a workload missing?" A drop is counted, never silent |
| `eshu_dp_workload_multi_definer_nodes` | graph-backed gauge, served from the snapshot refresher in `docs/public/reference/telemetry/graph-gauge-snapshots.md` | — | "Is any node defined by more than one repository?" The only detector for a cross-scope merge; expected 0 after the flip |
| `eshu_dp_workload_same_name_edges` | graph-backed gauge | — | "How many cross-repo name coincidences exist?" Growth is a review prompt, not an alert |
| `eshu_dp_workload_handle_resolution_total` | counter | `surface` (`api`, `mcp`, `reducer`), `outcome` (`exact`, `unique_by_name`, `ambiguous`, `miss`) | "Are old collectors or bookmarks failing to resolve, and where?" Rising `ambiguous` on `reducer` means a collector needs `workload_repository_id`; rising after a new repository lands is the staleness case in the compatibility doc §2.3 |

Each lands with a `docs/public/observability/telemetry-coverage.md` row, per
the `telemetry-coverage` gate. Structured logs on `ambiguous` carry the handle
and candidate `repo_id`s; the reducer log also carries the distinct skip
reason so `workload_handle_ambiguous` and `ambiguous_anchor` are never read as
one number.
