# #7057: Neo4j entity-id anchor for the relationships and story reads

## Problem

`docs/internal/evidence/7057-divergence-uid-anchor.md` fixed the Neo4j code
reads whose anchor is always a call-graph node. It left the label-agnostic
entity lookups alone because they can resolve non-code nodes. Those reads
still found their entity with `codemodel.GraphEntityIDPredicate`,
`(e.id = $entity_id OR e.uid = $entity_id)`, on an unlabeled or loosely
labeled node, and Neo4j planned that as an `AllNodesScan` (or a
`NodeByLabelScan` for the labeled class reads) on every request:

- `codequery/relationship_handlers.go` `relationshipsGraphRow`, the first read
  of `POST /api/v0/code/relationships` and the MCP relationship tools when an
  `entity_id` is given. On the 984-repo Neo4j trial this endpoint took
  1.27-1.50s (mandate: under 1s).
- `codequery/story_reads.go` `relationshipStoryClassMethods`,
  `relationshipStoryInheritanceDepthRows`, and
  `relationshipStoryGraphRowsForDirection`, which build their Cypher in
  `codequery/relationships/story` (`ClassMethodsCypher`,
  `InheritanceDepthCypher`, `GraphCypher`).

After this change no production Neo4j read calls `GraphEntityIDPredicate`.
The NornicDB branch of `BuildTransitiveRelationshipRowsCypher` still uses it,
and the NornicDB readers are unchanged.

## Graph truth: which nodes an entity id can name

Callers get entity ids from resolve and search results: content entity ids
from the content store, graph `e.id` from the repo-scoped resolve query
(`entity/handler.go` `BuildResolveGraphQuery`, any node a File CONTAINS),
Repository ids (the resolve ranking boosts `Repository`), and Workload ids
from workload resolution (`entity/resolve_workload.go`). The old predicate
matched any node whose `id` or `uid` equals the input, so the question for
each label is whether a constraint-backed seek finds the same node.

| Label group | Neo4j constraint | Writers and identity | id vs uid | Reachable as an entity id | New anchor branch |
| --- | --- | --- | --- | --- | --- |
| Parsed content entities: every label in `projector/canonical` `entityTypeLabelMap` except Parameter (Function, Class, Variable, Module, K8sResource, TerraformResource, SqlTable, Flux*, CloudFormation*, Package*, ...) | `<label>_uid_unique` | canonical entity writer (`canonicalNodeEntity*Template`: `MERGE (n:%s {uid: row.entity_id}) SET n += row.props`, props `id = entity.EntityID`); semantic entity writer (`MERGE ... {uid: row.entity_id}`, `SET n.id = row.entity_id` or properties `id = entity_id`) | equal | yes | uid |
| File | `file_uid_unique` | canonical file writer MERGEs on `path` and sets `f.uid`; never sets `id` | no `id` | yes (search and call-graph results) | uid |
| Reducer or dedicated-writer uid labels: CloudResource, KubernetesWorkload, KubernetesNamespace, CidrBlock, PrefixList, SecurityGroupRule, IncidentRoutingEvidence, CodeTaintEvidence, ExternalPrincipal, SecretsIAM*, ShellCommand, TerraformStateResource, OCI and package-registry labels | `<label>_uid_unique` | `MERGE (x:Label {uid: row.uid})` then `SET x.id = row.uid` (ShellCommand's edge writer sets no id; the dependency-target upsert sets `id` ON CREATE to the same value it MERGEs uid on) | equal or no `id` | yes (infra, cloud, supply-chain reads) | uid |
| Repository, Workload, WorkloadInstance, Platform, Endpoint, EvidenceArtifact, CloudAction | `REQUIRE x.id IS UNIQUE` | MERGE on `id`; no writer sets `uid` | no `uid` | yes: Repository and Workload ids come straight from resolve | id |
| Rationale, DocumentationSection | none | MERGE on `uid`, no `id` | no `id` | no: no query package reads either label, so no API returns these uids | not covered |
| Parameter, Directory, name-keyed import Module, Environment, Ecosystem, Tier, CodeownerTeam, SourceLocalRecord | other keys | MERGE on name/path/ref keys; no `id` or `uid` | neither | the old predicate never matched them | not needed |

Every `SET <alias>.id = ...` in `go/internal/storage/cypher` and
`go/internal/reducer` was checked against its MERGE key: on uid-keyed labels
it is always the MERGE value. So on the uid-constrained labels a `{uid: x}`
seek finds exactly the nodes `(e.id = x OR e.uid = x)` found, and on the
id-constrained labels an `{id: x}` seek does. No label needed a truth change.
The only nodes the old predicate could match and the new anchor cannot are
Rationale and DocumentationSection, and no read path surfaces their uids.

## Fix

`codemodel.Neo4jEntityIDAnchor(alias, param)` renders:

```cypher
CALL {
  MATCH (e:<117 uid-constrained labels> {uid: $entity_id}) RETURN e
  UNION
  MATCH (e:CloudAction|Endpoint|EvidenceArtifact|Platform|Repository|Workload|WorkloadInstance {id: $entity_id}) RETURN e
}
```

The two label lists are copied from the graph schema, because `codemodel`
may not import `internal/graph`. `TestNeo4jEntityIDAnchorLabelsMatchSchema`
parses `graph.SchemaStatementsForBackend(neo4j)` and fails if either list
drifts from the uid-unique or id-unique constraint set. `UNION` returns a
node once even if both branches reach it.

- `relationshipsGraphRow` entity-id branch:
  `codemodel.RelationshipGraphRowCypherFromAnchor(Neo4jEntityIDAnchor("e", "$entity_id"))`.
  The name and repo-anchored branches render byte-identical Cypher to before.
- `story.GraphCypher`: the anchored endpoint (`source` outgoing, `target`
  incoming) binds through the CALL anchor. The repo and grant predicates
  (`RepoPredicates`) stay in the `WHERE` after the MATCH, unchanged.
- `story.ClassMethodsCypher`: `class` binds through the CALL anchor. The
  grant predicates on `class` and `method` stay in the `WHERE`.
- `story.InheritanceDepthCypher`: the pattern already requires `:Class`, so
  the anchor is inline, `(target:Class {uid: $entity_id})` incoming and
  `(source:Class {uid: $entity_id})` outgoing. Both Class writers set
  `id = uid`. The endpoint grants and the scoped interior
  `all(node IN nodes(path) ...)` predicate are unchanged.
- The builders' `predicate func(string, string) string` parameter is gone.
  Every caller passed `graphEntityIDPredicate`, and the Neo4j anchor has to
  change the MATCH, not the WHERE.
- The `source_sha256` pins in `queryplan/testdata/query-source-coverage.yaml`
  for `relationshipsGraphRow`, `relationshipStoryClassMethods`,
  `relationshipStoryInheritanceDepthRows`, and
  `relationshipStoryGraphRowsForDirection` were re-derived. Their classes
  (`keyed_support`, `single_key`) and bounds are unchanged: each read still
  binds one entity and keeps its RunSingle or `LIMIT`.

A label-qualified WHERE was tried first, as a cheaper change that would have
kept the predicate hook:
`WHERE (e:<uid labels> AND e.uid = $x) OR (e:<id labels> AND e.id = $x)`.
Neo4j planned it as a `UnionNodeByLabelsScan` at 384,875 db hits, no better
than the `AllNodesScan`, so it was dropped.

## Tests

- `codequery/relationship_uid_anchor_test.go`:
  `TestNeo4jRelationshipsGraphRowAnchorsOnIndexedEntityID` and
  `TestNeo4jRelationshipStoryReadsAnchorOnIndexedEntityID` (five subtests:
  class methods, inheritance outgoing and incoming, graph outgoing and
  incoming). They drive the unchanged reader methods through a capturing fake
  graph, so they compile on base `6085458ac1`. On base all six fail with
  "Neo4j read still renders the id-OR-uid anchor"; on this branch all pass.
- `codemodel/neo4j_entity_anchor_test.go`: the schema pin above and the
  rendered clause shape.
- Updated for the new signatures, with their assertions kept:
  `auth_scoped_relationship_story_clause_test.go` (grant and repo predicate
  binding, scoped and unscoped), `relationship_story_cross_repo_test.go`,
  `relationship_story_grant_clause_live_test.go`,
  `entity_resolution_test.go` (handler-level anchor), and
  `relationship_story_test.go`.

## Performance Evidence

Performance Evidence: live `PROFILE` on Neo4j Community, image
`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`,
in an isolated container on ports 59474/59687 that was removed afterwards.
The schema was the full Neo4j DDL from `graph.SchemaStatementsForBackend`
(252 of 254 statements applied; the two failures are the procedure-syntax
full-text fallbacks). The seed had 126,550 nodes and 346,499 relationships:
50 Repository, 10,000 File, 60,000 Function, 10,000 Class, 20,000 Variable,
2,000 each of Struct, Interface and TypeAlias, 3,000 Module, 5,000
TerraformResource, 5,000 K8sResource, 5,000 CloudResource, 500 Workload and
2,000 Rationale. Edges: REPO_CONTAINS, CONTAINS (File to entity, Class to
method), 180,000 Function CALLS, INHERITS chains, REFERENCES, PROVISIONS,
DEFINES and EXPLAINS. Every uid-keyed node has `id = uid`, as the writers do.

Each query is the production builder's rendered text, printed by a scratch
program from base `6085458ac1` (throwaway detached worktree) and from this
branch. Each ran once to warm the plan cache and once under `PROFILE`.
Timings are the median of five warm runs over HTTP. Result rows were compared
sorted, before and after: all 21 cases returned identical rows.

| Read | Input | Before db hits (operator) | After db hits | Before / after ms | Rows |
| --- | --- | --- | --- | --- | --- |
| relationships row | Function id | 383,567 (AllNodesScan) | 4,012 | 69.8 / 9.1 | 1 |
| relationships row | Class id | 380,497 | 968 | 61.3 / 5.6 | 1 |
| relationships row | File uid | 381,402 | 1,892 | 64.2 / 13.7 | 1 |
| relationships row | TerraformResource / K8sResource / Variable / Module id | 379,830 / 379,814 / 379,922 / 379,814 | 306 / 290 / 397 / 290 | 80.8-113.8 / 7.8-12.1 | 1 each |
| relationships row | CloudResource uid | 379,756 | 231 | 79.5 / 8.2 | 1 |
| relationships row | Repository id | 396,950 | 17,634 | 81.2 / 19.5 | 1 |
| relationships row | Workload id | 379,728 | 203 | 77.4 / 7.0 | 1 |
| relationships row | missing id | 379,651 | 124 | 71.5 / 12.6 | 0 |
| story graph, outgoing CALLS | Function id | 1,675,613 (AllNodesScan) | 420 | 333.8 / 5.0 | 3 |
| story graph, incoming CALLS | Function id; Class id | 1,675,756; 1,675,349 | 570; 131 | 855.9 / 10.7; 689.1 / 9.2 | 5; 0 |
| story graph, outgoing | missing id | 1,675,349 | 124 | 304.7 / 3.3 | 0 |
| story class methods | Class id; File uid; missing | 892,003; 892,019; 892,001 (NodeByLabelScan) | 138; 193; 124 | 91.3-102.6 / 3.2-4.6 | 1; 6; 0 |
| story inheritance outgoing | Class id, depth chain 3 | 20,031 (NodeByLabelScan + seek) | 23 (one NodeUniqueIndexSeek) | 11.9 / 11.0 | 3 |
| story inheritance incoming | Class id; missing | 20,422; 20,002 | 345; 1 | 9.5 / 5.5; 11.0 / 7.4 | 26; 0 |

Every after plan has no scan operator. The CALL anchor plans as 124
`NodeUniqueIndexSeek` operators (117 uid labels, 7 id labels), about 124 db
hits for a missing id. That floor is paid once per request, not per row.
The Repository row's after cost is the relationship expansion of a
repository with 200 files (REPO_CONTAINS and DEFINES), not the anchor.

The anchor text is long (117 labels), so its cold planning cost was
measured too. With a unique comment appended to defeat the plan cache, the
median of five cold runs (plan plus execute) was 81.9 ms for the new
relationships row against 183.9 ms for the old one, and 50.6 ms against
466.3 ms for the incoming story read. The long disjunction does not make
the first execution slower.

These are query-level numbers on a 127k-node fixture. The old cost grows
with total node count, since `AllNodesScan` touches every node in the
database. The new cost does not. This makes no claim about end-to-end wall
time for `POST /api/v0/code/relationships` on the 984-repo corpus. The trial
report says the handler's non-graph work is a large part of its 1.27s p50,
so whether the endpoint gets under 1s has to be measured on that corpus.

## Observability Evidence

No-Observability-Change: these are query-shape changes inside existing
readers and builders. They add no metric, span, or log key. The existing
graph-read telemetry on these handlers (query duration, row counts, and the
graph deadline error path) still covers the same reads.
