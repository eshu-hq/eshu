# Workload Identity: Reconciled Model And Compatibility Contract

Status: proposal for owner decision on issue #5385. It reconciles the two
recorded decisions that pull against each other and states the identity,
compatibility, migration, and consumer contract an implementer would build to.
No implementation. Read with
[5385-workload-identity-key.md](5385-workload-identity-key.md) (the measured
mechanism and the site inventory) and
[5385-workload-identity-options.md](5385-workload-identity-options.md).

Owners: reducer, projector, query/MCP, SDK contract, and console maintainers.

## 0. The tension, and how this document resolves it

Two owner statements are on the record:

1. #6179 (2026-08): **Option C** — `Workload` keyed by repository
   (`WorkloadCandidate.RepoID`), `WorkloadInstance` derived from it.
2. Issue comment, 2026-09-08: **SAME_NAME correlation edges** — "keep per-repo
   Workload nodes, link same-named workloads across repos with explicit
   `SAME_NAME` edges … No identity migration: avoids churning every instance-id
   consumer, all cassettes, and the B-12 snapshot".

These cannot both hold literally. Both `MERGE` sites key the node on `id`
alone (`batchWorkloadNodeUpsertCypher` and
`batchWorkloadInstanceNodeUpsertCypher` in
`go/internal/reducer/workload_materializer.go`; `canonicalWorkloadUpsertCypher`
and `canonicalWorkloadInstanceUpsertCypher` in
`go/internal/storage/cypher/canonical.go`) and `id` is declared unique (the
`workload_id` and `workload_instance_id` constraints in
`go/internal/graph/schema_tables.go`). With a name-only `id`, two
repositories that share a name get **one** node by construction. "Per-repo
Workload nodes" therefore requires the node key to carry the repository. There
is no reading of the graph in which the key stays `workload:<name>` and the
nodes are per-repo.

The resolution this document proposes, and asks the owner to confirm in
section 5, question 1:

- The **graph node key changes** to the Option C shape. That is the only way
  to get per-repo nodes, and it is what #6179 already accepted.
- The **public handle does not change**. `workload:<name>` stays accepted on
  every surface that takes one today (API path and body parameters, MCP
  selectors, console URLs, CLI scopes, and the SDK's provider-asserted
  `workload_id` fields). It becomes a *handle* that the server resolves to a
  node, failing closed when the caller's grant admits more than one.
- "No identity migration" is honoured as **no contract break and no in-place
  rewrite**. It is not honoured as "no node key change": the stored `id`
  values move, the B-12 snapshot's asserted ids regenerate, and the old nodes
  are rebuilt rather than rewritten (section 3).
- `SAME_NAME` edges carry the cross-repo correlation that the collapse used to
  express physically. They are the queryable, refusable form of the same
  fact, and the owner's rationale points 1 and 3 hold exactly as written.

The alternative reading — keep `id = workload:<name>` in API responses while
the graph keys on something else — was considered and rejected. An `id` that
two nodes share is not an identifier, `/api/v0/workloads/{workload_id}/context`
would be ambiguous by design, and every writer that anchors on `{id: …}` would
fan out across tenants (the Option A trap from the options doc). Under the
fail-closed posture of #5384, a missed site must match nothing, not everything.

## 1. The reconciled identity model

### 1.1 Node keys

Built only by `go/internal/workloadid` (`NewWorkloadID`,
`NewWorkloadInstanceID`), whose constructors already accept the repository id
and ignore it (#6580). The re-key is an edit inside that package plus the
read-side conversions in 1.4.

| Node | Key | Example |
| --- | --- | --- |
| `Workload.id` | `workload:<repo_id>:<name>` | `workload:orders-api:checkout` |
| `WorkloadInstance.id` | `workload-instance:<repo_id>:<name>:<environment>` | `workload-instance:orders-api:checkout:prod` |
| Public handle (not a node key) | `workload:<name>` | `workload:checkout` |

`<repo_id>` is `WorkloadCandidate.RepoID` (`go/internal/reducer/projection.go`),
the defining repository, as #6179 settled — never `DeploymentRepoID(s)`, which
is plural, re-picked on confidence changes, and usually absent (key doc §5).
`<name>` is `WorkloadCandidate.WorkloadName` after the existing trim; a blank
segment still yields the empty id and the row is dropped, with the counter in
4.3 recording the drop rather than a silent skip.

Two rules that make the readable composite safe:

- **Nothing parses the key.** Consumers read `repo_id`, `name`, and
  `environment` from properties, which the four upsert templates above
  already stamp and which `go/internal/graph/schema_tables_indexes.go` already
  indexes (`workload_name`, `workload_repo_id`, `workload_instance_environment`,
  `workload_instance_workload_id`, `workload_instance_repo_id`). The one parser
  that cuts at the first colon, MCP's `normalizeQualifiedIdentifier`
  (`go/internal/mcp/service/context/routes.go`), is converted in 1.4.
  `repo_id` itself may contain colons (`repository:r_<hex>` from
  `go/internal/repositoryidentity`; the golden corpus uses bare names), which
  is why the prohibition is on parsing rather than on the delimiter.
- **A name containing `:` is rejected at the constructor**, counted under
  `reason="colon_in_name"` (4.3), and never written. Today's key would already
  be ambiguous for such a name; the re-key makes the rejection explicit.

The opaque-digest alternative (a hex of `(repo_id, name, environment)`, the
`stableAPIEndpointID` precedent) is offered to the owner in section 5,
question 2. This document recommends the readable composite because the id
appears in console URLs and API responses, and because no fixed-segment parser
exists to break (verified in the first design pass on the issue).

### 1.2 Properties every write stamps

Unchanged: `repo_id`, `name`, `kind`, `classification`, `environment`
(instance), `workload_id` (instance, the parent's new id),
`materialization_confidence`, `materialization_provenance`, `evidence_source`.

Added on both labels: `scope_id` and `generation_id`. `Workload` writes carry
no generation today, which is why the options doc warned that old hubs are
never reaped. Stamping them makes the rebuild in section 3 and any later
retract addressable by generation, the same way instance retraction already is.

After the re-key `Workload.repo_id` is a true single-owner property: it can no
longer be overwritten by a second repository, so the direct-ownership family
of the #5384 predicate becomes exact rather than "whoever wrote last".

### 1.3 SAME_NAME edges

**What they connect.** Two `Workload` nodes with equal `name` and different
`repo_id`. One edge per unordered pair, written in a deterministic direction
(lexically smaller `id` → larger) and always read undirected
(`-[:SAME_NAME]-`), so a pair is idempotent under retry and never doubled.

**Properties.**

| Property | Value | Why |
| --- | --- | --- |
| `name` | the shared bare name | lets a reader filter without touching endpoints |
| `relationship_basis` | `"name_equality"` | states what the edge proves: the names match, nothing more |
| `confidence` | `0.5` fixed | a name coincidence is not evidence of the same service; the value is deliberately below every materialized-truth edge (`DEFINES` 1.0, `RUNS_ON` 0.97) so nothing ranks it as truth |
| `evidence_source` | `"workload_materialization"` | the writer's existing source token |
| `scope_id`, `generation_id` | of the write that created or last confirmed it | retract and freshness |
| `identity_key` | `"canonical"` | matches the RUNS_ON convention so legacy cleanup templates have a discriminator |

**Provenance and write path.** Written by the workload materializer after the
node upsert of a scope-generation, as one batched statement anchored on the
`workload_name` index: `MATCH (w:Workload {id: row.workload_id})`,
`MATCH (o:Workload {name: w.name}) WHERE o.repo_id <> w.repo_id`, then
`MERGE` the ordered pair. This is a hot-path graph write and is a **theory
until measured** under the repo's prove-the-theory rule: the implementer runs
`PROFILE` on the pinned NornicDB with the `workload_name` index present before
building it, and records the row in the measurement ledger.

**Retract.** Node-level. A `DETACH DELETE` of either endpoint removes the edge
on both backends (`ledger:5385-retract-rebuild-node-detach-delete`), which is
the only retract this design relies on because every relationship retract
measured inert on the pinned build (`ledger:5385-retract-rebuild-relationship-inert`).

**Visibility under a scoped token.** The edge crosses tenants by definition.
The recommendation (section 5, question 3) is that a scoped read traverses
`SAME_NAME` only when the grant admits **both** endpoints; a sibling in an
ungranted repository is invisible, and no count of hidden siblings is
returned. The existence of a same-named workload elsewhere is itself
information about another tenant.

### 1.4 Cross-repo addressing keeps working

The handle `workload:<name>` resolves through one ladder, implemented once in
`go/internal/query` and reused by MCP and the reducer's provider-asserted
resolution (2.4):

1. Exact node id match (`{id: $target}`) — a caller holding the new id.
2. Handle match: strip the `workload:` prefix, match `{name: $bare}` within
   the caller's repository grant. Exactly one node → resolved. Zero → miss.
   More than one → **ambiguous**, fail closed, return the candidates'
   `repo_id`s so the caller can narrow with the `repo` argument the surfaces
   already accept (`entityMapWorkloadRepoScopedResolverQuery` and
   `changeSurfaceWorkloadRepoScopedResolverQuery` are the existing
   `{repo_id, name}` resolvers; `serviceWorkloadAmbiguousError` in
   `go/internal/query/entity/service_workload_resolution.go` is the existing
   ambiguity error).
3. Optional expansion: `include_same_name: true` returns the resolved node
   plus its admitted `SAME_NAME` siblings, explicitly labelled as such.

Sites the re-key must convert or prove already resolved (the #6580 tracking
comment on the issue; `go/internal/workloadid/README.md`):

| Site | Today | After |
| --- | --- | --- |
| `canonicalWorkloadIDCandidate` in `go/internal/query/impact/change_surface_resolvers.go` | builds `workload:<target>` and matches `{id}` | phase 2 becomes the handle ladder; phase-3 name fallback is subsumed |
| the response `id` build in `go/internal/query/entity/workload_context.go` | emits `"workload:" + name` as the response `id` | emits the node's `id` property; adds `handle: workload:<name>` beside it so existing clients keep a stable string to display |
| the catalog identity row in `go/internal/query/repository/catalog.go` | synthesizes the id from `reducer_workload_identity` names | synthesizes from `(repo_id, name)`; `catalogWorkloadKey` keys by repo and name so split siblings stop merging in `/catalog` |
| `BuildWorkloadDependencyRows` in `go/internal/reducer/dependency.go` | `NewWorkloadID(targetRepoIDs[depName], depName)` | **no change**: the name→repo map already supplies the repository, so the constructor yields the scoped id by construction. (`BuildWorkloadDependencyRows` has no production caller; the live DEPENDS_ON path carries ids already built by projection.) |
| `normalizeQualifiedIdentifier` in `go/internal/mcp/service/context/routes.go` | cuts at the first colon | treats a `workload:` prefix as a handle and forwards it whole; `canonicalWorkloadIdentifier` accepts both the handle and the full id |
| `apps/console/src/pages/ExplorerPage.tsx:384`, `apps/console/src/api/eshuGraphDeploymentModel.ts:49` | build `workload:${name}` | unchanged: they build handles, which the API resolves; bookmarked `/workspace/services/workload:<name>` URLs keep working and gain an ambiguity state |

## 2. The compatibility contract

Independently released collectors provide scoped source evidence and never
choose canonical ids. That means the graph key in 1.1 is **reducer-owned**;
no collector emits it, and no collector needs to know it.

### 2.1 Fact payload fields that carry workload identity

| Family / kind | Field | Carries repo identity today? | Consumer |
| --- | --- | --- | --- |
| `aws/v1` `Resource.Attributes` pass-through | `workload_id`, `workload_ids` (`ResourceAnchorAttributes.WorkloadIDs` in `attribute_shapes.go`) | No. The scope is an AWS account, not a repository. | `workload_cloud_relationship_materialization.go` → exact `MATCH` in `WorkloadCloudRelationshipUpsertCypherFormat` (`workload_cloud_relationship_writer.go`) |
| `service_catalog.repository_link` | `workload_id` (`repository_link.go`) | Partly: `repository_id`/URL fields name the linked repository | `servicecatalog` correlation index |
| `service_catalog.entity` | `workload_id` (`entity.go`) | No | same |
| `vulnerability.suppression` | `workload_id` (`suppression.go`) | No | suppression matching |
| `reducer_supply_chain_impact_finding` (reducer-owned) | `workload_ids`, `repository_id` (`findings.go`) | Yes | search, MCP, console |
| Envelope | `scope_id` (`envelope.go`) | Indirectly: a scope maps to one or more repositories via `ScopeRepositoryReader.ListScopeRepositoryIDs` (`go/internal/reducer/crossrepo/cross_repo_resolution_ownership.go`) | every handler |

Nothing in-tree writes the AWS or service-catalog `workload_id` today
(key doc §6a), so the real-world population of legacy handles is unknown from
inside this repository. The contract below is written so that population does
not matter.

### 2.2 Schema changes and their classification

| Change | Class under contract-system-v1 §5 | Gate |
| --- | --- | --- |
| Add optional `workload_repository_id` (string) to `service_catalog.repository_link`, `service_catalog.entity`, `vulnerability.suppression` | **Minor** — additive optional field | `factschema-diff` passes; module bumps to the next minor |
| Accept optional `workload_repo_id` in the AWS attribute pass-through (`ResourceAnchorAttributes.WorkloadRepoID`) | **Minor** — additive optional attribute; the pass-through is already tolerant of unknown keys | `payload-usage-manifest` gains the new read |
| `workload_id` accepts both the handle `workload:<name>` and the full id `workload:<repo_id>:<name>` | **Minor** — the accepted value set widens; the field's meaning ("a workload the provider is asserting") is unchanged; the doc comment states the ladder | none; docs lockstep |
| `reducer_supply_chain_impact_finding.workload_ids` values become full ids | **Minor** for the schema (unchanged shape); the emitted values move with the node key, and `repository_id` beside them already scopes the finding | B-12 and cassette regeneration (section 4) |
| Reject a `workload_id` handle that resolves ambiguously (2.4) | **Behaviour** change on a defect path: today the handle matched the merged node and attached cross-tenant | reducer regression test |

No change is major. Nothing is removed, renamed, narrowed, or re-derived. If
the owner instead required legacy handles to be **refused** outright, that
would change the field's meaning and be major with a decode shim; this
document does not recommend it (section 5, question 5).

### 2.3 What an old collector's facts do during cutover

An old collector emits `workload_id: "workload:checkout"` with no repository
field. The reducer resolves it with the ladder from 1.4, restricted by what the
fact itself can prove:

| Fact scope | Candidate set | Outcome |
| --- | --- | --- |
| Repository-bound scope (`scope_id` → repositories via `ListScopeRepositoryIDs`) | `Workload {name}` whose `repo_id` is in that set | one → resolved; many → `ambiguous_anchor`; none → miss |
| Non-repository scope (AWS account, catalog provider) with a `repository_id`/`workload_repository_id` field | `Workload {repo_id, name}` | one → resolved; none → miss |
| Non-repository scope, handle only | `Workload {name}` across the graph | one → resolved; many → `ambiguous_anchor`, skipped, counted; none → miss |

The rule that never bends: **a handle is never attached to more than one
node, and never to the "last writer".** `ambiguous_anchor` is an existing
skip reason with an existing counter
(`workloadCloudRelationshipSkipAmbiguousAnchor` in
`workload_cloud_relationship_materialization.go`), so the reducer
path needs a resolution step, not a new failure vocabulary. An ambiguous skip
is a visible, classified outcome; the collector's fix is to emit the new
optional repository field, and until it does the affected edge is absent
rather than wrong.

A new collector emitting the full id resolves at rung 1 and is unaffected by
any name coincidence.

### 2.4 Conformance fixture and the isolation proof

Two proofs, in two places, because the fixture pack is dependency-free and
cannot run the reducer.

**SDK side — the schema is compatible.** The three existing valid fixtures
(`sdk/go/factschema/fixturepack/payloads/service_catalog.repository_link.valid.json`,
`service_catalog.entity.valid.json`, `vulnerability.suppression.valid.json`)
stay **byte-identical**: an old collector's payload, with no
`workload_repository_id`, must still validate against the regenerated JSON
Schema. `payload_schema_test.go` in `sdk/go/collector/conformance` already
fails closed on a missing required field, so keeping the new field optional is
what the fixtures prove. Add one fixture per kind carrying the new field
(`*.repo-scoped.valid.json`, extending `fixturepack.go`'s
one-valid-one-invalid enumeration to accept a suffixed variant) so the
scorecard example's pin test (`examples/collector-extensions/scorecard/fixturepack_pin_test.go`)
exercises both shapes from outside the tree.

**Reducer side — tenants never merge.** A workload-materialization Odù in the
Ifá family catalog (`go/internal/ifa/workload_dependency_family_catalog.go`)
with two repositories named `alpha` and `beta`, each defining `checkout` in
different environments, plus one `service_catalog.repository_link` fact
carrying the legacy handle `workload:checkout` and no repository field. It
asserts, by direct graph read: two `Workload` nodes, two `WorkloadInstance`
nodes, one `SAME_NAME` edge, `DEFINES` from each repository to its own node
only, `RUNS_ON` from each instance to its own platform only, and **zero**
edges written from the legacy handle, with the `ambiguous_anchor` counter at
one. The same Odù, with the handle replaced by `workload:alpha:checkout`,
asserts one edge on alpha's node and none on beta's. That pair is the
RED/GREEN evidence the contract change ships with; the existing materializer
test that pins one shared node for two repositories becomes its RED.

## 3. Migration and rebuild ordering

### 3.1 What #6197 measured, and what it did not

- Every relationship retract on the pinned backend is a silent no-op
  (`ledger:5385-retract-rebuild-relationship-inert`); node `DETACH DELETE`
  clears every family on both backends
  (`ledger:5385-retract-rebuild-node-detach-delete`). The migration therefore
  deletes nodes and re-projects; it never retracts edges.
- Both retract statements anchor on the id that changes, so the old nodes must
  go **before** the new ids exist, or they orphan with their edges attached.
- On the 908-repository graph the population was 40 `Workload` and 33
  `WorkloadInstance` nodes (key doc §3.1). Rebuild wall time was **not**
  measured. The implementer states a time bound before the first rebuild and
  records it in the ledger.

### 3.2 Ordered steps

0. Step zero landed (#6580): typed constructors, blank-segment drop, routing
   guard tests.
1. **Land the code with the old key still emitted.** Handle ladder in query
   and MCP, `SAME_NAME` writer, `scope_id`/`generation_id` stamps, the SDK
   minor bump with fixtures, the reducer resolution step for provider-asserted
   handles, telemetry from 4.3, and the read-side conversions from 1.4. All of
   it is exercised by the two-repository Odù against the *old* key, where the
   collision counters fire; this proves the detectors before the fix removes
   the thing they detect.
2. **Retire durable rows keyed on the old ids, using the old ids.**
   `service_evidence_key` rows (`ServiceRuntimeEvidenceKey` embeds the
   instance id; key doc §6 item 8) and the persisted search handles
   (`go/internal/searchdocs/project.go`). Postgres first, because it is
   what the drained queue reads back.
3. **Drain the reducer queue** for the workload materialization and
   repo-dependency domains; hold new claims.
4. **`DETACH DELETE` every `Workload` and `WorkloadInstance` node** by label.
   Not by old id: the relationship-inert finding means a stray edge that
   escaped an id-anchored delete would stay forever. A label-wide delete of a
   40-node population is the cheapest correct shape. Verify with a settled
   count read (the ledger rows record the stale-read trap).
5. **Flip the constructors** in `go/internal/workloadid` to the 1.1 formats;
   update the pinned-format tests deliberately.
6. **Re-enqueue workload materialization for every scope**, then
   repo-dependency (which writes `RUNS_ON`) and cloud relationship domains, in
   that order, so every edge writer finds its new anchor. Old-shape ids never
   coexist with new-shape ids in the graph.
7. **Regenerate the golden artifacts** in the same PR (section 4), re-prove
   the `fetchWorkloadRuntimeTopology` query-plan pin (its `source_sha256`,
   `WorkloadInstance.workload_id` anchor, and the retained #5272 caveat in
   `go/internal/queryplan/testdata/hot-cypher.yaml:236-258`), and re-index
   search.
8. Run the two-repository Odù against the new key: counters at zero, two
   nodes, one `SAME_NAME` edge.

Steps 1 and 2 are separate PRs; 3–8 are one PR with one operator runbook. The
rebuild is a one-shot maintenance step with a stated bound, not a rolling
coexistence window: because relationship retracts are inert, a window in
which both shapes exist is a window in which nothing can clean up.

### 3.3 The graph retract, RUNS_ON, and cloud USES

**`go/internal/graph/mutations.go` no longer exists on `main`.** The brief
names its `repo_id`-keyed clause; that file (`ResetRepositorySubtreeInGraph`
and siblings) had no callers, was retracted as dead code on the issue, and has
since been removed (`rg ResetRepositorySubtreeInGraph go/` is empty at
`2ae147cf9`). The live repository cleanup is
`canonicalNodeRepositoryIDCleanupCypher`
(`go/internal/storage/cypher/canonical_node_cypher.go`), which deletes the
`Repository` node and its incident edges only. Under per-repo keys it needs
one addition: `DETACH DELETE` the `Workload` and `WorkloadInstance` nodes
whose `repo_id` matches, so a removed repository does not leave orphaned
per-repo workload nodes. Today's shared node survives that cleanup on purpose;
tomorrow's owned node must not.

**`RUNS_ON` scopes by construction.** The upsert
(`canonicalRunsOnUpsertCypher` in `canonical_relationships.go`) walks
`(repo {id})-[:DEFINES]->(w)` then `(i)-[:INSTANCE_OF]->(w)`. With a per-repo
key the first hop reaches exactly one `w`, so the second reaches only that
repository's instances; no `i.repo_id` predicate is required, and none is
added, so the query plan does not move. The retract pair
(`RetractRepoRunsOnEdgesCypher`, `RetractSingleRepoRunsOnEdgesCypher`) is the
same traversal and inherits the same scoping; it stays
inert on the pinned build regardless (ledger above) and is not relied on. The
two-repository Odù asserts one `RUNS_ON` per instance to its own platform.

**Cloud `USES` scopes by resolution.** The writer's
`MATCH (workload:Workload {id: row.workload_id})<-[:INSTANCE_OF]-(instance) WHERE instance.environment = row.environment`
(`WorkloadCloudRelationshipUpsertCypherFormat`) stays as written. What
changes is upstream: `row.workload_id` is the output of the 2.3 ladder, so it
is either one full id or the row was skipped as `ambiguous_anchor`. The writer
can no longer fan one resource onto every tenant's instance of a name.

## 4. Consumer impact

| Consumer | Impact | Disposition |
| --- | --- | --- |
| `GET /api/v0/workloads/{workload_id}/context`, `/story` | exact `w.id` match, 404 on miss | accept the handle via the ladder; add `409`-class ambiguous response listing candidate `repo_id`s |
| `POST /api/v0/compare/environments` (`workload_id` body) | exact match, no name fallback | same ladder; `repo` optional narrowing |
| `fetchWorkloadRuntimeTopology` | pinned plan, `workload_id` anchor | anchor value moves; pin re-proved (3.2 step 7) |
| `/catalog` read model | merges rows by bare name | key by `(repo_id, name)`; siblings appear separately with `same_name_siblings` count |
| MCP `service/context` selectors | first-colon cut | handle forwarded whole; ambiguity surfaces in the `truth` envelope |
| Console (three sites, 1.4) | builds and bookmarks handles | unchanged code; new ambiguity state in the selector UI |
| CLI `eshu report --scope workload:<name>` | label parser | unchanged: it takes the handle |
| Cassettes | 14 `"workload:` literals in 5 files under `testdata/cassettes/` (`awscloud`, `workloaddependency`, `kuberneteslive`, `repodependency`, `symbolruntime`), no instance literals — counted with `rg -c` at `2ae147cf9` | regenerate in the migration PR per `eshu-golden-corpus-rigor` |
| B-12 snapshot `testdata/golden/e2e-20repo-snapshot.json` | 32 `"workload:` occurrences over 5 names and 6 `"workload-instance:` over 2 ids, including asserted `workload.id` / `left.instance.id` values | regenerate; the corpus has no collisions, so counts of nodes and edges must be unchanged — that equality is the proof the re-key did not over-reach |
| #5384 / #5167 query-time predicates | family 1 (direct `repo_id`) becomes exact; family 5 (`DEFINES`-collision inline-map, the family list in `go/internal/query/infra_scope.go`) becomes structurally unreachable | keep family 5 as a fail-closed guard until the collision counters have read zero for one release, then retire it with its own RED/GREEN |
| `get_service_changed_since` | `service_evidence_key` embeds the instance id | one generation of retired-plus-added rows, announced in the runbook; never collapsed into unchanged |
| Search index | persisted `GraphHandles{Kind:"workload", ID}` | re-index (3.2 step 7) |
| `reducer_workload_identity` Postgres `entity_keys` | legacy name keys | preserved as recorded on the issue; the graph handle is derived at repo-aware seams, and identity-only fallback plus supply-chain normalization change in the same stack |

### 4.3 Collision telemetry an operator can use

| Signal | Type | Labels | 3 AM question it answers |
| --- | --- | --- | --- |
| `eshu_dp_workload_identity_collision_total` | counter | `mode` (`same_scope_drop`, `cross_scope_merge`) | "Did two repositories contend for one identity?" Before the flip it measures the defect; after it, any non-zero is a leaked key |
| `eshu_dp_workload_identity_rejected_total` | counter | `reason` (`blank_segment`, `colon_in_name`) | "Why is a workload missing?" A drop is counted, never silent |
| `eshu_dp_workload_multi_definer_nodes` | graph-backed gauge, served from the snapshot refresher in `docs/public/reference/telemetry/graph-gauge-snapshots.md` | — | "Is any node defined by more than one repository?" The only detector for a cross-scope merge; expected 0 after the flip |
| `eshu_dp_workload_same_name_edges` | graph-backed gauge | — | "How many cross-repo name coincidences exist?" Growth is a review prompt, not an alert |
| `eshu_dp_workload_handle_resolution_total` | counter | `surface` (`api`, `mcp`, `reducer`), `outcome` (`exact`, `unique_by_name`, `ambiguous`, `miss`) | "Are old collectors or bookmarks failing to resolve, and where?" Rising `ambiguous` on `reducer` means a collector needs the new repository field |

Each lands with a `docs/public/observability/telemetry-coverage.md` row, per
the `telemetry-coverage` gate. Structured logs on `ambiguous` carry the handle
and candidate `repo_id`s.

## 5. Questions the owner must answer

1. **Confirm the reconciliation in section 0.** The node key changes (Option C
   shape); `workload:<name>` stays the public handle; "no identity migration"
   means no contract break and no in-place rewrite, not no key change.
   *Recommendation: confirm.* There is no per-repo node without a per-repo key,
   and the handle layer is what preserves every consumer the 2026-09-08 note
   wanted to protect.
2. **Readable composite or opaque digest for the node key?**
   *Recommendation: readable composite* (`workload:<repo_id>:<name>`), with
   parsing prohibited and a colon in a name rejected at the constructor. Ids
   appear in URLs and responses, no fixed-segment parser exists, and the
   properties consumers need are already stamped and indexed.
3. **Is a `SAME_NAME` sibling in an ungranted repository visible to a scoped
   token, even as a count?** *Recommendation: no.* Traverse only when both
   endpoints are admitted. Cross-tenant existence is itself a leak.
4. **Ambiguity policy for a legacy handle that matches more than one node
   within the caller's grant.** *Recommendation: fail closed everywhere* — an
   ambiguous API response with candidates, `ambiguous_anchor` in the reducer —
   and never pick the last writer or the highest confidence.
5. **SDK change: additive optional `workload_repository_id` fields and the
   widened `workload_id` value set as a minor bump, with legacy handles still
   accepted?** *Recommendation: yes.* Refusing legacy handles would be a major
   change with a shim and would break collectors that are correct today.
6. **One-shot drained rebuild (3.2) rather than a coexistence window?**
   *Recommendation: one-shot.* Relationship retracts are inert on the pinned
   backend, so a coexistence window cannot be cleaned up. The population is
   40 nodes and 33 instances; the implementer states the bound before running.
7. **`reducer_workload_identity` `entity_keys` stay legacy-keyed in Postgres,
   with the graph handle derived at repo-aware seams?** *Recommendation: yes*,
   as already recorded on the issue; a Postgres rewrite buys nothing the seam
   derivation does not.
8. **Does `Workload.repo_id` stay authoritative for ownership?**
   *Recommendation: yes.* After the flip it is single-owner by construction,
   which is what makes the #5384 direct-ownership family exact and lets the
   `DEFINES`-collision family retire later.

## 6. What was checked for this document

Read-only at `origin/main` `2ae147cf9`: the issue thread with every comment,
both merged design documents, `go/internal/workloadid`, the two `MERGE` sites,
the RUNS_ON and cloud USES writers, the repository cleanup Cypher, the query
and MCP resolver sites, the SDK schema fields listed in 2.1, the fixture-pack
enumeration, the #5384 predicate families, the query-plan pin, and the three
5385 measurement-ledger rows. Counts of snapshot and cassette literals come
from `rg -c` on that commit. No code was run against a backend; the
`SAME_NAME` write cost is explicitly a theory awaiting `PROFILE`.
