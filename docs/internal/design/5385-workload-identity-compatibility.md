# Workload Identity: Reconciled Model And Compatibility Contract

Status: decided 2026-09-25 (section 3) for issue #5385. It reconciles the two
recorded decisions that pulled against each other and states the identity and
compatibility contract an implementer builds to. Migration, rebuild ordering,
required probes, consumer impact, and telemetry are in the companion
[5385-workload-identity-migration.md](5385-workload-identity-migration.md).
The measured mechanism and site inventory remain in
[5385-workload-identity-key.md](5385-workload-identity-key.md); the option
survey is [5385-workload-identity-options.md](5385-workload-identity-options.md).

Owners: reducer, projector, query/MCP, SDK contract, and console maintainers.

## 0. The tension, and the decision that resolves it

Two owner statements were on the record before 2026-09-25:

1. #6179 (2026-08): **Option C** — `Workload` keyed by repository
   (`WorkloadCandidate.RepoID`), `WorkloadInstance` derived from it.
2. Issue comment, 2026-09-08: **SAME_NAME correlation edges** — "keep per-repo
   Workload nodes, link same-named workloads across repos with explicit
   `SAME_NAME` edges … No identity migration: avoids churning every instance-id
   consumer, all cassettes, and the B-12 snapshot".

They cannot both hold literally. Both `MERGE` sites key the node on `id` alone
(`batchWorkloadNodeUpsertCypher` and `batchWorkloadInstanceNodeUpsertCypher` in
`go/internal/reducer/workload_materializer.go`; `canonicalWorkloadUpsertCypher`
and `canonicalWorkloadInstanceUpsertCypher` in
`go/internal/storage/cypher/canonical.go`) and `id` is declared unique (the
`workload_id` and `workload_instance_id` constraints in
`go/internal/graph/schema_tables.go`). With a name-only `id`, two repositories
that share a name get **one** node by construction. Per-repo nodes require the
node key to carry the repository.

**Decision D1 (2026-09-25): re-key with a readable composite, and keep
`workload:<name>` as a public handle.** The graph node key changes to the
Option C shape; the handle stays accepted on every surface that takes one
today and is resolved server-side, failing closed when the caller's grant
admits more than one node. "No identity migration" is honoured as no contract
break and no in-place rewrite. It is **not** honoured as no key change.

**Options offered to the owner, as the arbiter framed them.**

(a) **Re-key both nodes and keep `workload:<name>` as a resolved handle.**
52 golden literals move (32 `"workload:` and 6 `"workload-instance:`
occurrences in `testdata/golden/e2e-20repo-snapshot.json`, plus 14
`"workload:` occurrences across 5 cassette files under `testdata/cassettes/`,
counted with `rg -c` at `2ae147cf9`). This adds the handle ladder (1.4), an
SDK minor bump (2.2), and a search reindex.

(b) **Option D: the hub stays name-keyed and the instance becomes
repo-owned.** About 6 literals move and there is no ladder, but the hub
remains a permanent cross-tenant edge surface for `RUNS_IN`
(`canonical_runs_in_edges.go`), `DEPENDS_ON` and workload-sourced
`EXPOSES_ENDPOINT` (the edge templates in `workload_materializer.go`), and
`DOCUMENTS` (`canonical_documentation_edges.go`, which anchors on
`{id: row.target_entity_id}`). Its properties stay last-writer-wins
(`batchWorkloadNodeUpsertCypher` `SET`s `repo_id` unconditionally), the
family-5 under-authorization becomes permanent for any colliding tenant (the
`DEFINES`-collision exclusion documented in
`go/internal/query/relationships_catalog_cypher.go`), and `SAME_NAME` has
nothing to connect. A third shape, name-only plus the query-time guards
alone, moves 0 literals and repairs nothing at write time.

**Recommendation and recorded decision: (a).** It deviates literally from
rationale point 2 of the 2026-09-08 note (the 52-literal count), because (b)
deviates from point 3 and from the "per-repo Workload nodes" clause, and
accuracy outranks compatibility. **Re-confirmed by the owner on 2026-09-25
with Option D in view**, after the (b) column above was presented as written.

## 1. The reconciled identity model

### 1.1 Node keys

Built only by `go/internal/workloadid` (`NewWorkloadID`,
`NewWorkloadInstanceID`), whose constructors already accept the repository id
and ignore it (#6580). The re-key is an edit inside that package plus the
read-side conversions in 1.4.

| Node | Key | Example (production `repo_id`) |
| --- | --- | --- |
| `Workload.id` | `workload:<repo_id>:<name>` | `workload:repository:r_8477a002:checkout` |
| `WorkloadInstance.id` | `workload-instance:<repo_id>:<name>:<environment>` | `workload-instance:repository:r_8477a002:checkout:prod` |
| Public handle (not a node key) | `workload:<name>` | `workload:checkout` |

`<repo_id>` is `WorkloadCandidate.RepoID` (`go/internal/reducer/projection.go`),
the defining repository, as #6179 settled — never `DeploymentRepoID(s)`, which
is plural, re-picked on confidence changes, and usually absent (key doc §5).
In production `repo_id` is `repository:r_<8 hex>` (`CanonicalRepositoryID` in
`go/internal/repositoryidentity/identity.go`), so the composite contains
colons; the golden corpus uses bare repository names, which is why its
literals read as `workload:api-svc` rather than the production shape.

Rules that make the readable composite safe:

- **Nothing parses the key.** Consumers read `repo_id`, `name`, and
  `environment` from properties, which the four upsert templates above already
  stamp and which `go/internal/graph/schema_tables_indexes.go` already indexes
  (`workload_name`, `workload_repo_id`, `workload_instance_environment`,
  `workload_instance_workload_id`, `workload_instance_repo_id`). The one
  parser that cuts at the first colon, MCP's `normalizeQualifiedIdentifier`
  (`go/internal/mcp/service/context/routes.go`), is converted in 1.4.
- **A name containing `:` is rejected at the constructor.** This is an
  explicit decision, not a side effect: the row is dropped and counted under
  `reason="colon_in_name"` (migration doc, telemetry). Affected population:
  of the 12 distinct workload names in the B-12 snapshot and cassettes, 0
  contain a colon; of the 22 fixture directories under `tests/fixtures`, 0 do
  (counted at `2ae147cf9`). Production names come from repository names and
  directory basenames, so a colon is possible but unobserved.
- **Environment must be colon-free.** It already is: every producer funnels
  through the allowlisted, canonicalized tokens recorded in
  `go/internal/workloadid/evidence-notes.md`. The constructor rejects a colon
  there too, under the same counter, so the last segment of an instance id is
  unambiguous even though the key is never parsed.

Decision D1 chose the readable composite over an opaque digest because the id
appears in console URLs and API responses, no fixed-segment parser exists to
break (verified in the first design pass on the issue), and the properties a
consumer needs are already stamped and indexed.

### 1.2 Properties every write stamps

Unchanged: `repo_id`, `name`, `kind`, `classification`, `environment`
(instance), `workload_id` (instance, the parent's new id),
`materialization_confidence`, `materialization_provenance`, and
`evidence_source`, whose value on this path is `finalization/workloads`
(`EvidenceSourceWorkloads` in `go/internal/reducer/projection.go`).

Added on both labels: `scope_id` and `generation_id`. `Workload` writes carry
no generation today, which is why the options doc warned that old hubs are
never reaped. Stamping them makes the rebuild and any later retract
addressable by generation, the same way instance retraction already is.

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
| `confidence` | `0.5` fixed | a name coincidence is not evidence of the same service; deliberately below every materialized-truth edge (`DEFINES` carries `row.materialization_confidence`, at or above the 0.82 admission floor; `RUNS_ON` is 0.97) so nothing ranks it as truth |
| `evidence_source` | `finalization/workloads` | the materializer's existing source token |
| `scope_id`, `generation_id` | of the write that created or last confirmed it | retract and freshness |
| `identity_key` | `"canonical"` | matches the `RUNS_ON` convention so legacy cleanup templates have a discriminator |

**Writer shape.** Two statements, not one. First read the siblings:
`MATCH (o:Workload) WHERE o.name IN $names RETURN o.id, o.name, o.repo_id`.
Pair and order the results in Go, dropping same-`repo_id` pairs. Then write:
`UNWIND $pairs AS row MATCH (a:Workload {id: row.a}) MATCH (b:Workload {id: row.b}) MERGE (a)-[:SAME_NAME]->(b) SET …`.
The tempting single statement — an inline `{name: w.name}` match followed by
`WHERE o.repo_id <> w.repo_id` — is the shape documented as "Inline `MATCH`
Property Pattern Silently Dropped By A Trailing `WHERE`" in
`docs/public/reference/nornicdb-pitfalls.md`, and is not used. Cost is a
theory until measured: probe P2 in the migration doc runs `PROFILE` on the
paired shape against the rejected one before the writer is built.

**Concurrency.** Reducer claims are partitioned by scope (`scope_id` and
`conflict_domain` on `fact_work_items`), so the workload materializer's only
cross-scope `MERGE` is this one: two scopes materializing same-named
workloads concurrently can both attempt the same ordered pair. That is a
commit-time UNIQUE conflict on a `MERGE`-shaped group, which
`graphWriteRetryReasonUniqueConflict` in
`go/internal/storage/cypher/retrying_executor.go` already classifies and
retries. The classifier is not a proof: probe P6 in the migration doc (two
workers on the same pair, at least 20 trials on the current pin) must show
exactly one edge and idempotency on retry. **The edge-versus-query-time choice
is gated on P6.** If P6 shows duplicates, `SAME_NAME` becomes gauge-only and
siblings are computed at query time from the `workload_name` index with the
grant filter applied; every consumer in this document that reads the edge
then reads that computed set instead.

**Retract.** Node-level only. A `DETACH DELETE` of either endpoint removes the
edge on both backends (`ledger:5385-retract-rebuild-node-detach-delete`).

**Visibility under a scoped token — Decision D2.** The edge crosses tenants by
definition. A scoped read traverses `SAME_NAME` only when the grant admits
**both** endpoints; a sibling in an ungranted repository is invisible, and no
count of hidden siblings is returned anywhere, including `/catalog`'s
`same_name_siblings`, which counts admitted siblings only. The existence of a
same-named workload elsewhere is itself information about another tenant.

### 1.4 Cross-repo addressing keeps working

The handle `workload:<name>` resolves through one ladder, implemented once in
`go/internal/query` and reused by MCP and the reducer's provider-asserted
resolution (2.3):

1. Exact node id match (`{id: $target}`) — a caller holding the new id.
2. Handle match: strip the `workload:` prefix, match `{name: $bare}` within
   the caller's repository grant. Exactly one node → resolved. Zero → miss.
   More than one → **ambiguous, fail closed (Decision D3)**, returning the
   candidates' `repo_id`s so the caller can narrow with the `repo` argument
   the surfaces already accept (`entityMapWorkloadRepoScopedResolverQuery`
   and `changeSurfaceWorkloadRepoScopedResolverQuery` are the existing
   `{repo_id, name}` resolvers; `serviceWorkloadAmbiguousError` in
   `go/internal/query/entity/service_workload_resolution.go` is the existing
   ambiguity error).
3. Optional expansion: `include_same_name: true` returns the resolved node
   plus its admitted `SAME_NAME` siblings, explicitly labelled as such.

**The coupling to state plainly:** a legacy handle keeps working only while
the bare name is unique within its candidate set. For a repository-bound
scope the candidate set is that scope's repositories
(`ListScopeRepositoryIDs`), so another tenant's same-named workload never
enters it: the ambiguity couples tenants **only for non-repository scopes**
(an AWS account, a catalog provider) carrying handle-only facts, and for
user-typed handles under a grant that spans both repositories. In those
cases, the moment a second repository with the name is ingested, every stored
handle for it — bookmarks, saved selectors, provider-asserted fields — fails
closed until narrowed or replaced by a full id. That is the price of D3 and
the intended behaviour; the alternative is picking a tenant.

Cost of a handle lookup: one `workload_name` index seek per handle, batched
per intent on the reducer side and per request on the query side. Probe P2
covers it in the same `PROFILE` session as the writer.

Sites the re-key must convert or prove already resolved (the #6580 tracking
comment on the issue; `go/internal/workloadid/README.md`):

| Site | Today | After |
| --- | --- | --- |
| `canonicalWorkloadIDCandidate` in `go/internal/query/impact/change_surface_resolvers.go` | builds `workload:<target>` and matches `{id}` | phase 2 becomes the handle ladder; the phase-3 name fallback is subsumed |
| the response `id` build in `go/internal/query/entity/workload_context.go` | emits `"workload:" + name` as the response `id` | emits the node's `id` property; adds `handle: workload:<name>` beside it so existing clients keep a stable string to display |
| the catalog identity row in `go/internal/query/repository/catalog.go` | synthesizes the id from `reducer_workload_identity` names | synthesizes from `(repo_id, name)`; `catalogWorkloadKey` keys by repo and name so split siblings stop merging in `/catalog` |
| `BuildWorkloadDependencyRows` in `go/internal/reducer/dependency.go` | `NewWorkloadID(targetRepoIDs[depName], depName)` | **no change**: the name→repo map already supplies the repository, so the constructor yields the scoped id by construction. (No production caller; the live DEPENDS_ON path carries ids already built by projection.) |
| `normalizeQualifiedIdentifier` in `go/internal/mcp/service/context/routes.go` | cuts at the first colon | treats a `workload:` prefix as a handle and forwards it whole; `canonicalWorkloadIdentifier` accepts both the handle and the full id |
| `apps/console/src/pages/ExplorerPage.tsx`, `apps/console/src/api/eshuGraphDeploymentModel.ts`, `apps/console/src/pages/VulnerabilitiesReachable.tsx` | build or display `workload:${name}` | unchanged code: they build handles, which the API resolves; bookmarked `/workspace/services/workload:<name>` URLs keep working and gain an ambiguity state |

## 2. The compatibility contract

Independently released collectors provide scoped source evidence and never
choose canonical ids. The graph key in 1.1 is **reducer-owned**; no collector
emits it, and no collector needs to know it.

### 2.1 Fact payload fields that carry workload identity

| Family / kind | Field | Carries repo identity today? | Consumer |
| --- | --- | --- | --- |
| `aws/v1` `Resource.Attributes` pass-through | `workload_id`, `workload_ids` (`ResourceAnchorAttributes.WorkloadIDs` in `attribute_shapes.go`) | No. The scope is an AWS account, not a repository. | `workload_cloud_relationship_materialization.go` → exact `MATCH` in `WorkloadCloudRelationshipUpsertCypherFormat` |
| `service_catalog.repository_link` | `workload_id` (`repository_link.go`) | Partly: `repository_id` and the URL fields name the linked repository | `service_catalog_correlation_index.go` |
| `service_catalog.entity` | `workload_id` (`entity.go`) | No | same |
| `vulnerability.suppression` | `workload_id`, nested in `Scope` beside `repository_id` (`suppression.go`) | Only when the scope also sets `repository_id` | `go/internal/reducer/supplychain/core/decode.go`; stored by `vulnerability_suppression_store.go` |
| `reducer_supply_chain_impact_finding` (reducer-owned) | `workload_ids`, `repository_id` (`findings.go`) | Yes | search, MCP, console; the two GIN-indexed columns (migration doc §7) |
| Envelope | `scope_id` (`envelope.go`) | Indirectly: a scope maps to one or more repositories via `ScopeRepositoryReader.ListScopeRepositoryIDs` (`go/internal/reducer/crossrepo/cross_repo_resolution_ownership.go`) | every handler |
| `k8s_workload_identity_use` | `workload_object_id` | n/a | **not affected**: keys `KubernetesWorkload` by object uid, never `Workload.id` |

Nothing in-tree writes the AWS or service-catalog `workload_id` today (key doc
§6a), so the real-world population of legacy handles is unknown from inside
this repository. The contract below is written so that population does not
matter.

### 2.2 Schema changes and their classification

One new field name is used everywhere: **`workload_repository_id`**.

| Change | Class under contract-system-v1 §5 | Gate |
| --- | --- | --- |
| Add optional `workload_repository_id` (string) to `service_catalog.repository_link`, `service_catalog.entity`, and `vulnerability.suppression.Scope` | **Minor** — additive optional field | `factschema-diff` passes; module bumps to the next minor |
| Accept optional `workload_repository_id` in the AWS attribute pass-through (`ResourceAnchorAttributes.WorkloadRepositoryID`) | **Minor** — additive optional attribute; the pass-through already tolerates unknown keys | `payload-usage-manifest` gains the new read |
| `workload_id` accepts both the handle `workload:<name>` and the full id `workload:<repo_id>:<name>` | **Minor** — the accepted value set widens; the field's meaning ("a workload the provider is asserting") is unchanged; the doc comment states the ladder | docs lockstep |
| `reducer_supply_chain_impact_finding.workload_ids` values move from handles to full ids | **Minor, with the justification below** | B-12 and cassette regeneration |
| A `workload_id` handle that resolves ambiguously is skipped (2.3) | Behaviour change on a defect path: today the handle matched the merged node and attached cross-tenant | reducer regression test |

**Why `workload_ids` is Minor and not Major.** The policy makes "changing the
meaning of a field, including how a stable key is derived" major. The field's
meaning is "the canonical ids of the workloads this finding touches, as minted
by the reducer"; that meaning is unchanged, and the finding's own stable key
is not derived from it. What changes is the reducer's id format, which is
reducer-owned output regenerated every generation, not a collector-facing
input. The emitter and every in-tree reader move in one release; old rows are
superseded on the next generation. A Major bump with a shim would shim
nothing: no shim can map a name-only id to a repository without the graph,
which is the defect being fixed. The disclosed residual cost: an external
client filtering by a **stored** old handle through the `?` containment
readers (migration doc §7) gets no rows until it passes through the
ladder-aware endpoints. That is listed as consumer impact, not hidden behind
the classification.

Nothing is removed, renamed, narrowed, or re-derived. If the owner instead
required legacy handles to be **refused**, that would change the field's
meaning and be Major with a shim; Decision Q5 does not.

### 2.3 What an old collector's facts do during cutover

An old collector emits `workload_id: "workload:checkout"` with no repository
field. The reducer resolves it with the ladder from 1.4, restricted by what the
fact itself can prove:

| Fact scope | Candidate set | Outcome |
| --- | --- | --- |
| Repository-bound scope (`scope_id` → repositories via `ListScopeRepositoryIDs`) | `Workload {name}` whose `repo_id` is in that set | one → resolved; many → skipped as `workload_handle_ambiguous`; none → miss |
| Non-repository scope (AWS account, catalog provider) with `repository_id` or `workload_repository_id` | `Workload {repo_id, name}` | one → resolved; none → miss |
| Non-repository scope, handle only | `Workload {name}` across the whole graph | one → resolved; many → `workload_handle_ambiguous`, skipped, counted; none → miss |

Two disclosures on that table. The graph-wide row is **existing behaviour**,
not a new grant: today's exact `MATCH` on `workload:<name>` already resolves
across the whole graph, because the name-only key made every name global. And
the skip reason is **new and distinct**: `ambiguous_anchor`
(`workloadCloudRelationshipSkipAmbiguousAnchor` in
`workload_cloud_relationship_materialization.go`) means the cloud anchor
evidence itself was ambiguous; `workload_handle_ambiguous` means the evidence
was clear and the graph holds more than one workload with that name. An
operator needs to tell those apart, so the reason is not overloaded.

The rule that never bends: **a handle is never attached to more than one
node, and never to the "last writer".** An ambiguous skip is a visible,
classified, **alertable** outcome: the reducer emits a structured log with the
handle and the candidate `repo_id`s, and
`eshu_dp_workload_handle_resolution_total{surface="reducer",outcome="ambiguous"}`
carries an alert rule, not only a count. The collector's fix is to emit
`workload_repository_id`; until it does, the affected edge is absent rather
than wrong.

**Staleness.** A handle that resolved uniquely can become ambiguous later,
when a second repository with that name is ingested. The `USES` edge written
earlier stays on the first node until the cloud-relationship domain
re-materializes for the emitting scope; on that re-materialization the row
resolves ambiguous, is skipped, and the earlier edge is retracted by scope and
evidence source (`RetractWorkloadCloudRelationshipEdgesCypher`, the
scope-wide retract `commitEdges` dispatches in
`workload_cloud_relationship_materialization.go`). That retract is a
relationship delete, **unverified on the current pin**: probe P1 covers it
with the other retract shapes under a Neo4j control. If P1 shows it inert,
either the stale-`USES` hazard is recorded as an open item on the issue or
the writer retracts by instance node, which is proven on both backends. Until
P1 runs, the stale edge is visible only as the `ambiguous` counter rising for
that surface.

**Matching semantics per consumer** (each needs a RED/GREEN test, migration
doc T3):

| Consumer | Full id | Unique handle | Handle spanning two repositories |
| --- | --- | --- | --- |
| `vulnerability.suppression.Scope.workload_id` | applies to that node's findings | applies | **does not apply** to either; a suppression must never silence findings in another tenant; counted |
| `service_catalog.repository_link` / `entity` `workload_id` | links that node | links | no link; `workload_handle_ambiguous` |
| AWS `workload_id` / `workload_ids` | `USES` edge to that node's instances | edge | no edge; `workload_handle_ambiguous` |

A new collector emitting the full id resolves at rung 1 and is unaffected by
any name coincidence.

### 2.4 Conformance fixture and the isolation proof

Two proofs, in two places, because the fixture pack is dependency-free and
cannot run the reducer.

**SDK side — the schema is compatible, and the new shape is primary.** For
`service_catalog.repository_link`, `service_catalog.entity`, and
`vulnerability.suppression`, the **primary** valid fixture
(`sdk/go/factschema/fixturepack/payloads/<kind>.valid.json`) carries
`workload_repository_id`; today's handle-only bytes are kept verbatim under a
`<kind>.legacy-handle.valid.json` sibling (extending `fixturepack.go`'s
one-valid-one-invalid enumeration to accept a suffixed variant) so an old
collector's payload, with no `workload_repository_id`, still validates
against the regenerated JSON Schema. The SDK doc comment on `workload_id`
marks the handle-only form as **legacy**, and states the ladder.
`payload_schema_test.go` in `sdk/go/collector/conformance` already fails
closed on a missing required field, so keeping the new field optional is what
the legacy fixture proves; the scorecard example's pin test
(`examples/collector-extensions/scorecard/fixturepack_pin_test.go`) exercises
both shapes from outside the tree.

**Reducer side — tenants never merge.** A workload-materialization Odù in the
Ifá family catalog (`go/internal/ifa/workload_dependency_family_catalog.go`)
with two repositories named `alpha` and `beta`, each defining `checkout` in
different environments, plus one `service_catalog.repository_link` fact
carrying the legacy handle `workload:checkout` and no repository field. It
asserts, by direct graph read: two `Workload` nodes, two `WorkloadInstance`
nodes, one `SAME_NAME` edge, `DEFINES` from each repository to its own node
only, `RUNS_ON` from each instance to its own platform only, zero duplicate
`Endpoint` nodes, and **zero** edges written from the legacy handle, with
`workload_handle_ambiguous` counted once. The same Odù, with the handle
replaced by `workload:alpha:checkout`, asserts one edge on alpha's node and
none on beta's. The existing materializer test that pins one shared node for
two repositories becomes the RED. The migration doc lists it as probe P5.

## 3. Decisions (2026-09-25)

Recorded by the owner in the orchestrator session, with an independent
arbiter check that confirmed D1–D3, kept D4 with a corrected justification
(migration doc §3), and confirmed the three recorded recommendations.

| # | Decision | Rationale retained |
| --- | --- | --- |
| D1 | **Option (a): re-key both nodes with a readable composite; keep `workload:<name>` as a resolved handle.** Rejected: option (b) Option D / `WorkloadGroup`; name-only plus query guards. Re-confirmed 2026-09-25 with Option D in view. | Section 0. There is no per-repo node without a per-repo key; (b) leaves the hub a permanent cross-tenant edge surface with last-writer properties and permanent family-5 under-authorization; accuracy outranks compatibility, so the 52-literal deviation from rationale point 2 is accepted. |
| D1a | Readable composite, not an opaque digest. | Section 1.1: ids appear in URLs and responses; no fixed-segment parser exists; parsing is prohibited and a colon in a name or environment is rejected and counted. |
| D2 | **Hide an ungranted `SAME_NAME` sibling, not even a count.** | Section 1.3: cross-tenant existence is itself a leak; `/catalog` counts admitted siblings only. |
| D3 | **Fail closed on an ambiguous legacy handle**, everywhere: an ambiguous API response with candidate `repo_id`s; `workload_handle_ambiguous` in the reducer. Never last-writer, never highest-confidence. | Sections 1.4 and 2.3. The coupling — handles work only while the name is unique — is the intended price. |
| D4 | **One-shot drained rebuild, no coexistence window.** | Migration doc §3: during coexistence exact-id lookups resolve stale old nodes, node counts double, and `DEPENDS_ON` straddles the two shapes. Nodes are deleted because node `DETACH DELETE` is proven on both backends; the decision does not rest on relationship-retract behaviour. |
| Q5 (recorded, owner may revise on the PR) | SDK change is a **Minor** bump: additive optional `workload_repository_id`, widened `workload_id` value set, legacy handles still accepted; `workload_ids` Minor with the 2.2 justification. Two requirements ride with it: the repo-scoped fixture is the **primary** valid fixture and the SDK doc comment marks handle-only as legacy (2.4); the reducer's `ambiguous` outcome is **alertable**, with the handle and candidate `repo_id`s logged, not only counted (2.3). | Refusing legacy handles would be Major with a shim and would break collectors that are correct today. Ambiguity couples tenants only for non-repository scopes with handle-only facts (1.4). |
| Q7 (recorded, owner may revise on the PR) | `reducer_workload_identity` `entity_keys` stay legacy-keyed in Postgres; the graph handle is derived at repo-aware read/output seams, with identity-only fallback and supply-chain normalization changing in the same stack. | Already recorded on the issue; a Postgres rewrite buys nothing the seam derivation does not. |
| Q8 (recorded, owner may revise on the PR) | `Workload.repo_id` stays authoritative for ownership. | Single-owner by construction after the flip; makes the #5384 direct-ownership family exact and lets the `DEFINES`-collision family retire later. |

## 4. What was checked for this document

Read-only at `origin/main` `2ae147cf9`: the issue thread with every comment,
both merged design documents, `go/internal/workloadid`, the two `MERGE` sites,
the RUNS_ON and cloud USES writers, both repository cleanup Cypher templates,
the query and MCP resolver sites, the SDK schema fields listed in 2.1, the
fixture-pack enumeration, the #5384 predicate families, the query-plan pin,
the retry classifier, both `workload_ids` GIN indexes, and the three 5385
measurement-ledger rows. Counts of golden literals, workload names, and fixture
directories come from `rg` and a short Python pass on that commit. No code
was run against a backend; every cost in this document is a theory until the
probes in the migration doc land.
