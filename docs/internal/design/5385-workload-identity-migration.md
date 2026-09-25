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

**Cascade on repository cleanup.** Two Cypher templates remove a repository
node, both dispatched from `canonical_node_writer_phases.go`:
`canonicalNodeRepositoryIDCleanupCypher` (lookup by id) and
`canonicalNodeRepositoryPathCleanupCypher` (lookup by path where the id
differs — the shape a rename or re-onboarding produces, because
`CanonicalRepositoryID` mints a **new** `repo_id` for the new identity). Under
per-repo keys, both must also `DETACH DELETE` the `Workload`,
`WorkloadInstance`, and `Endpoint` nodes whose `repo_id` matches the removed
repository; otherwise a rename leaves a full set of orphaned per-repo nodes
under the retired id, with their `SAME_NAME` edges still pointing at live
nodes. Today's shared node survives repository cleanup on purpose; tomorrow's
owned nodes must not.

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
   `workload_handle_ambiguous` skip, telemetry (§8), the read-side
   conversions, and the repository-cleanup cascade. What this step can prove:
   the collision detectors, because a two-repository Odù under the old key
   still produces one node and the counters fire. What it **cannot** prove:
   the `SAME_NAME` writer and the handle ladder, because under the old key
   there is only one node with a name. Those are proved in step 9.
2. **Hold claims** for every domain in §4 (the reducer's claim SQL is
   partitioned by domain, so this is a per-domain pause, not a global stop).
3. **Drain** in-flight work for those domains, including the service-catalog
   runtime domain, whose `service_evidence_key` embeds the instance id.
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
6. **Flip the constructors** in `go/internal/workloadid` and roll the binary;
   update the pinned-format tests deliberately. This is the only irreversible
   edit in the sequence, and it is a code roll.
7. **Re-enqueue every domain in §4** for every scope, in the order given
   there, so each edge writer finds its new anchor. Old-shape ids never
   coexist with new-shape ids in the graph.
8. **Regenerate the golden artifacts** (§7), re-prove the
   `FetchWorkloadRuntimeTopology` plan pin (probe P4), and reindex search.
9. **Run the two-repository Odù against the new key** (probe P5): counters at
   zero, two nodes, one `SAME_NAME` edge, per-family edge counts equal to the
   pre-cutover baseline on the collision-free corpus.

**Rollback.** Roll the old binary and re-enqueue the same domains: the old key
regenerates the old nodes and edges from facts, and the Postgres rows retired
in step 4 regenerate from the same facts. The delete in step 5 is not the
point of no return; facts are. Rollback after step 8 additionally reverts the
golden regeneration and the search reindex.

Steps 1 is its own PR; 2–9 are one PR with one operator runbook and one stated
time bound.

## 4. Writers to re-enqueue, and the per-family count assertion

The delete set (§2) and the re-enqueue set are derived from the 14 production
files that anchor a `MATCH` on `:Workload` or `:WorkloadInstance` at
`2ae147cf9`, grouped by the reducer domain that dispatches them, in
re-enqueue order:

| Order | Domain | Anchoring files | Families |
| --- | --- | --- | --- |
| 1 | workload materialization | `go/internal/reducer/workload_materializer.go`, `workload_materialization_repo_phase.go`, `workload_materializer_retract_instances.go`, `go/internal/storage/cypher/canonical.go` | `DEFINES`, `INSTANCE_OF`, `EXPOSES_ENDPOINT`, `DEPLOYMENT_SOURCE`, `DEPENDS_ON`, `SAME_NAME` |
| 2 | repo dependency / `RUNS_ON` | `go/internal/storage/cypher/canonical_relationships.go`, `go/internal/storage/cypher/edge/writer/unroutable.go` | `RUNS_ON` |
| 3 | `DomainRunsIn` | `go/internal/storage/cypher/canonical_runs_in_edges.go` | `RUNS_IN` |
| 4 | `DomainDocumentationMaterialization` | `go/internal/storage/cypher/canonical_documentation_edges.go` | `DOCUMENTS` |
| 5 | cloud `USES` | `go/internal/storage/cypher/workload_cloud_relationship_writer.go`, `go/internal/reducer/workloadinstance/lookup.go`, `go/internal/ifa/materializededges/workload_cloud_relationship.go` (gate) | `USES` |
| 6 | service-catalog runtime | `go/internal/reducer/service_runtime_instance_lookup.go` | `service_evidence_key` rows |
| 7 | value-flow chain | `go/internal/reducer/code/value/cloud_sink_loader.go`, `go/internal/reducer/code/value/affected/gate.go` | `INVOKES_CLOUD_ACTION` / `RUNS_IN` consumers |

The list is derived from the tree, not from memory, and the re-key PR re-runs
the search. It is still not self-proving: **the only proof the list is
complete is the per-family count assertion below.**

**Per-family count assertion** (in the Odù and the runbook): before step 5 and
after step 9, on the collision-free golden corpus, the counts of `RUNS_IN`,
`DOCUMENTS`, `INSTANCE_OF`, `RUNS_ON`, `USES`, `DEPLOYMENT_SOURCE`,
`DEPENDS_ON`, and `EXPOSES_ENDPOINT` edges must be equal, and `Workload`,
`WorkloadInstance`, and `Endpoint` node counts must be equal. Any drift means
a writer was missed or over-reached; the equality is the proof the re-key
changed identity and nothing else.

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
| P3 | `DETACH DELETE` of the three labels plus full re-materialization wall time on the 908-repo store at the current pin | exact seconds and a human duration, with the stated bound |
| P4 | Re-prove the `FetchWorkloadRuntimeTopology` pin (`go/internal/query/entity/workload_runtime_topology.go`; `go/internal/queryplan/testdata/hot-cypher.yaml`, whose caveats retain the #5272 "about 75 times slower" repository-first finding and the Neo4j-only `NodeIndexSeek` proof) under the new `workload_id` values | pin updated; the `WorkloadInstance.workload_id` anchor still seeks; the 75x finding re-measured, not assumed |
| P5 | The two-repository Odù from the compatibility doc §2.4 on the new key | one `RUNS_ON` per instance, zero duplicate `Endpoint` nodes, one `SAME_NAME` edge, zero handle edges, per-family counts equal |
| P6 | `SAME_NAME` concurrent-`MERGE` contention: two workers writing the same ordered pair, at least 20 trials on the current pin, settled reads, retry observed | exactly one edge after every trial and an idempotent retry. **Gates the edge-versus-query-time choice:** duplicates in any trial make `SAME_NAME` gauge-only, with siblings computed at query time from the `workload_name` index under the grant filter |

Required tests, each RED before its change and GREEN after:

| Test | Contract |
| --- | --- |
| T1 scoped-token isolation | A token granted only `alpha` reading `/catalog`, the entity map, and the change surface for `checkout` sees `same_name_siblings` = 0 and no `beta` `repo_id` anywhere in the response body. Requires an audit of every untyped or variable-length traversal that starts at `Workload` (`-[*]-`, `-[]-`, `-[:A|B]-` lists) so none can reach a sibling without the grant predicate. |
| T2 concurrent writers | The P6 shape as a permanent RED/GREEN: two scopes materializing same-named workloads concurrently on a live backend end with exactly one `SAME_NAME` edge for the pair, asserted by a settled read; the UNIQUE-conflict retry is observed, not assumed. |
| T3 matching semantics | For suppression, service-catalog links, and AWS attributes: full id resolves; unique handle resolves; a handle spanning two repositories applies to neither, links nothing, writes no edge, and is counted. |
| T4 repository cleanup cascade | Removing or renaming a repository deletes its owned `Workload`, `WorkloadInstance`, and `Endpoint` nodes through both cleanup templates; the sibling's nodes and its other `SAME_NAME` edges survive. |
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
| Ifá materialized-edge gate (`go/internal/ifa/materializededges/workload_cloud_relationship.go`) | asserts `USES` endpoints by workload id | regenerate with the Odù |
| MCP `service/context` selectors | first-colon cut | handle forwarded whole; ambiguity surfaces in the `truth` envelope |
| Console (`ExplorerPage.tsx`, `eshuGraphDeploymentModel.ts`, `VulnerabilitiesReachable.tsx`, `ImpactDeploymentSummary.tsx`, `ExposureServiceSelector.tsx`) | build, display, bookmark, or prompt for handles | unchanged code; new ambiguity state in the selector UI |
| CLI `eshu report --scope workload:<name>` | label parser | unchanged: it takes the handle |
| Cassettes | 14 `"workload:` literals in 5 files under `testdata/cassettes/` (`awscloud`, `workloaddependency`, `kuberneteslive`, `repodependency`, `symbolruntime`), no instance literals — `rg -c` at `2ae147cf9`; the key doc's §3.3 count predates four of those files | regenerate in the migration PR per `eshu-golden-corpus-rigor` |
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
