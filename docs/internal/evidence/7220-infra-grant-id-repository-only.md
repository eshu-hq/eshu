# #7220 Scoped Infra Grant: Id Equality Admits Repository Nodes Only

The scoped infra grant predicate admitted a node when `n.id IN
$allowed_repository_ids` or `n.id IN $allowed_scope_ids`, for every label. Any
node whose `id` spelled a granted repository or scope id was returned, whoever
owned it. #7220 named the repository-id term; the scope-id term had the same
defect, in both dialects: `infraResourceScopeListPredicate` (Neo4j,
`infra_scope.go`) and `infraResourceScopeCoreDisjuncts` (SHAPE-A,
`infra_scope_grant.go`, also used by the relationships catalog endpoints).

## Fix

| Dialect and position | Before | After |
| --- | --- | --- |
| Neo4j list, any position | `n.id IN $allowed_repository_ids`, `n.id IN $allowed_scope_ids` | `(n:Repository AND (n.id IN $allowed_repository_ids OR n.id IN $allowed_scope_ids))` |
| SHAPE-A, single-node `MATCH (n:Label)` (search branches, per-label aggregates, the relationships anchor): `infraResourceScopeNodePredicate` | the same two bare terms | the same label term as Neo4j |
| SHAPE-A, alias bound by a relationship pattern (neighbors, relationships catalog endpoints, ecosystem `platform_count`): `infraResourceScopePredicate` | the same two bare terms | `(CASE WHEN 'Repository' IN labels(n) THEN n.id END) IN $allowed_repository_ids`, and the same for `$allowed_scope_ids` |

Every other label is admitted through `repo_id` or the ownership disjuncts the
predicate already walks (USES, MATCHES_STATE, DEPLOYMENT_SOURCE, DEFINES).

SHAPE-A needs both spellings because each one is wrong in the other's position
on NornicDB v1.3.3. Probes on a NornicDB v1.3.3 container, seeded with
`Repository r0`, `TerraformModule {id:'r0', repo_id:'r9'}`,
`TerraformModule {id:'m1', repo_id:'r0'}` and an anchor with `DEPENDS_ON` edges
to both modules (`nornicdb-query-pitfalls.md`, "A Label Predicate's Clause
Position Decides Whether It Is Evaluated"):

| Shape | Position | Rows | Verdict |
| --- | --- | --- | --- |
| bare `t.id IN ['r0']` (before) | relationship MATCH WHERE | `collide`, `owned` | the leak |
| `(t.repo_id IN [...] OR (t:Repository AND t.id IN [...]))` | relationship MATCH WHERE | none | drops the owned row |
| `(t.repo_id IN [...] OR ('Repository' IN labels(t) AND t.id IN [...]))` | relationship MATCH WHERE | none | drops the owned row |
| `(CASE WHEN 'Repository' IN labels(t) THEN t.id END) IN [...]` | relationship MATCH WHERE | `owned` | correct |
| `(CASE WHEN 'Repository' IN labels(n) THEN n.id END) IN ['r0']` | `MATCH (n:Repository)` | none | drops the granted Repository |
| `(n:Repository AND (n.id IN ['r0'] OR n.id IN ['z']))` | `MATCH (n:Repository)` and `MATCH (n)` | `repo` | correct |
| the same label term | `MATCH (n:TerraformModule)` | none | the collision is excluded |

The CASE form's single-node miss was found by the review of the first version of
this change, which used CASE everywhere and so broke a scoped relationships
lookup of a granted Repository on NornicDB. Single-node reads now use the label
term.

## Audit of every id-equality grant site

Each site was read in the statement it renders. "Bound" means the alias is
matched `:Repository` in the same statement.

| Site | Alias | Verdict |
| --- | --- | --- |
| `infra_scope.go` list predicate | any infra node | unsafe before, fixed |
| `infra_scope_grant.go` `infraResourceScopeCoreDisjuncts` (also `infraResourceScopePredicate`, `relationshipEndpointScopePredicate`) | any infra node | unsafe before, fixed |
| `infra_ecosystem_overview.go` durable entries | `i` (WorkloadInstance) | goes through `infraResourceScopePredicate` (CASE form); fixed with it. `platform_count` is a relationship read; `instance_count` is a single-node `MATCH (i:WorkloadInstance)`, where the CASE form is safe only because the pattern pins `i` to a label that is never a Repository |
| `infra_ecosystem_overview.go` other entries | `r`, `repo` | bound |
| `impact/deployment/impact_anchor_resolve.go:155` | `repo` | bound (`(repo:Repository)`, line 154) |
| `impact/change_surface_traversal.go:37` | `impacted` | tests `repo_id`, not `id` |
| `impact/change_surface_traversal.go:45` | `impacted` | bound (`(impacted:Repository)`) |
| `codequery/routes/impact.go:132` `RequiredRepositoryAccessClause` | caller's alias | Repository-only by contract; the only caller, `route_handlers.go:372`, binds `(repo:Repository)` |
| `querycontract.WorkloadScopePredicate` | Workload | `repo_id` only, no id term |
| `impact/blast_radius.go:200` (`GraphConditionOnProperty("repo", "id")`) and its `s.id` / `a.id IN $repo_ids` anchors at lines 80 and 303 | `repo`, `s`, `a` | bound |
| `impact/change_surface_resolvers.go:154`, `repository/handler.go:91,180`, `impact/deployment/cloud_resource_dependencies.go:61,83` | `repo`, `r` | bound |
| `GraphCondition` / `GraphPredicate` / `GraphWhereClause` callers (`trace_deployment_*`, `flux_bindings`, `dependency_cluster`, `workload_*`, `resolve_workload_query`, `deadcode`, `complexity_queries`, `quality/inspect`, `code_graph_search_query`, `code_import_dependencies_queries`, `language/cypher`, `selector`, `status_scoped`, `relationships/identity`, `entity/context_handler`) | `r`, `repo`, `s`, `t`, `target`, `targetRepo`, `repoViaInstance`, `source_repo`, `target_repo` | every alias bound `:Repository` in its MATCH |

`codequery/chain/grantfake.go` judged a hop predicate `node.id IN
$allowed_repository_ids` against the node's `repo_id` (substring match on `IN
$allowed_repository_ids`), so a fake route test would have passed on the leak.
It now judges a `.id` term against the node's own id, and records a statement
that tests `repo.id` without binding `(repo:Repository` as a parse failure.

## Proof

Live, `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`,
native arm64: `go test -tags live_infra_scope_neo4j ./internal/query -run
TestLiveInfraScopeNeo4jIDCollision`. Fixture: a granted anchor; a
`TerraformModule {id: <granted repo id>, repo_id: <ungranted>}`; a
`TerraformModule {id: <granted scope id>, repo_id: <ungranted>}`; a
`K8sResource {id: <granted repo id>}` with no `repo_id`; and a
`TerraformModule` owned by the granted repository as the control.

| Route | Dialect | Before | After |
| --- | --- | --- | --- |
| search | Neo4j list | 3 collision nodes returned | none; control returned |
| search | SHAPE-A, run on Neo4j | 3 collision nodes returned | none; control returned |
| relationships | Neo4j list | 3 collision neighbours returned | none; control returned |
| relationships | SHAPE-A, run on Neo4j | 3 collision neighbours returned | none; control returned |

`TestLiveInfraScopeNeo4jIDCollisionAnchors` adds the relationships anchor: a
granted Repository resolves as an anchor and a node whose id equals a granted
scope id (no Repository carries it) answers the nonexistent-id 404. It runs both
dialects on Neo4j; on NornicDB only the SHAPE-A subtest applies, and it passes.

The existing `TestLiveInfraScopeNeo4j*` equivalence, aggregate and ArgoCD
suites still pass: search, relationships and aggregates stay row-for-row equal
to the oracle and to SHAPE-A, so a granted Repository is still admitted.

Hermetic: `TestInfraScopePredicatesAdmitIDEqualityOnlyForRepository` (with a
seeded-violation RED/GREEN pair), the pinned Neo4j predicate text, and three
`GrantGraph` tests. The three NornicDB byte-identical statement digest pins
changed on purpose; substituting the old bare `<alias>.id` terms back for both
new spellings (the label term and the CASE operand) reproduces the original
digests exactly, so the id operand is the only change. Only those digest pins
guard the NornicDB statement text in CI: the live tests are `scheduled` on
Neo4j, so the NornicDB anchor and search proofs are run by hand.

## Performance

No-Regression Evidence (#7220): Neo4j list predicate on the search and count
routes. Backend `neo4j:2026-community@sha256:eabfbb04...`, native arm64, g5
grant, search query matching every fixture node so all rows reach the grant
filter, plain execution (no PROFILE), before and after alternated within one
process. Graph: 283,000 nodes (3,000 Repository, 150,000 TerraformResource,
60,000 K8sResource, 40,000 TerraformModule, 30,000 CloudResource). Db hits were
identical under PROFILE: 2,240,081 (search) and 1,957,927 (count).

First form, each id term guarded separately (`(n:Repository AND n.id IN ...)`
twice), 11 runs, medians:

| Route | Before | After | Change |
| --- | --- | --- | --- |
| search | 1.085 s | 1.249 s | +15% |
| count | 0.992 s | 1.144 s | +15% |

Reordering the terms (id test first) did not help. Isolating the full
predicate, including the four `EXISTS` families, on a hoisted `CALL { UNION }`
count over the same graph (medians, two separate runs):

| Predicate variant | Run A (7 runs) | Run B (9 runs) |
| --- | --- | --- |
| bare id terms (before) | 0.538 s | 0.543 s |
| two terms, label first | 0.627 s | |
| two terms, id first | 0.670 s | |
| one grouped label term (shipped) | 0.537 s | 0.555 s |
| grouped id terms, then label | | 0.584 s |
| grouped ids over a concatenated list | | 0.588 s |
| `'Repository' IN labels(n)` guard, id first | 0.665 s | |
| CASE, two terms (the SHAPE-A form) | 0.568 s | 0.600 s |
| no id term (control) | 0.485 s | 0.502 s |

The single grouped label term stays at the unguarded cost, so it is the shipped
form.

Shipped form, two runs of 11 alternated runs each. A second 283,000-node graph
was present in the database during these runs, so the count scanned 566,000
nodes and search still matched only its own 283,000. Medians before then after:

| Route | Run 1 | Run 2 |
| --- | --- | --- |
| search | 1.443 s then 1.434 s | 1.659 s then 1.679 s |
| count | 2.517 s then 2.632 s | 1.922 s then 1.880 s |

The before and after ranges overlap in all four cells, so no regression is
measurable at this scale. Absolute seconds differ between the two tables
because the database size differs; only the before/after ratio within a run
is comparable.

SHAPE-A also runs on Neo4j, in the relationships catalog scoped edges and the
ecosystem overview counts (`platform_count` is a relationship read; `instance_count`
is a single-node read of a label that is never a Repository), all of which carry
the CASE operand. Route-level Neo4j timing of those statements, old text against new,
9 alternated runs each, on 20,000 Repository, 60,000 Workload, 80,000
WorkloadInstance and 5,000 Platform nodes with 100,000 `USES_MODULE`, 100,000
`DEPENDS_ON` and 80,000 `RUNS_ON` edges, g5 grant (medians):

| Read | Before | After |
| --- | --- | --- |
| catalog edges, `USES_MODULE` (Repository source) | 0.278 s | 0.287 s (+3%) |
| catalog edges, `DEPENDS_ON` (Workload source) | 0.946 s | 0.969 s (+2%) |
| ecosystem `platform_count` | 0.800 s | 0.800 s |
| ecosystem `instance_count` | 0.785 s | 0.762 s |

The before and after ranges overlap in every row. SHAPE-A timing was not
measured on NornicDB: NornicDB is secondary for #7220.

Observability Evidence: the request spans already carry
`eshu.infra_scope_dialect` (`shape_a` or `neo4j_list_exists`). The predicate
change adds no metric, span or log key; a leak here is a wrong-row result, and
the operator-facing proof is the live collision test.

## NornicDB, noted and not blocking

On the local NornicDB image (`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c`,
reports `v1.3.3`; the image has no `RepoDigest`, so it is not proven to be the
pinned digest) the SHAPE-A relationships neighbor filter returned a foreign,
non-colliding neighbor. Re-running the same statement with the pre-#7220 text
returned the same rows, so this predates the change. With the anchor granted
`repo_id: r0`, an `OPTIONAL MATCH ... WHERE <predicate>` neighbor with
`repo_id: r9` and an unrelated id came back on NornicDB and not on Neo4j. It
needs its own proof against the pinned digest before it is filed. NornicDB
SHAPE-A search and the anchor lookups pass the collision test; only the
relationship-neighbor read fails it, through this defect.

`TestLiveInfraScopeShapeShapeADiscriminates` fails on this image at its
hand-written "naive backward-EXISTS" RED expectation (counted 1, wants 2). That
assertion does not use this predicate and does not depend on this change.
