# #7057: Neo4j entity-id anchor for the relationships and story reads

## Problem

`docs/internal/evidence/7057-divergence-uid-anchor.md` fixed the Neo4j code
reads whose anchor is always a call-graph node. It left the label-agnostic
entity lookups alone because they can resolve non-code nodes. Those reads
still found their entity with `codemodel.GraphEntityIDPredicate`,
`(e.id = $entity_id OR e.uid = $entity_id)`, on an unlabeled or loosely
labeled node. Neo4j planned that as an `AllNodesScan` (or a
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

Callers get entity ids from several places:

- content entity ids from the content store;
- graph `e.id` from the repo-scoped resolve query (`entity/handler.go`
  `BuildResolveGraphQuery`, which returns any node a File CONTAINS);
- Repository ids (the resolve ranking boosts `Repository`);
- Workload ids from workload resolution (`entity/resolve_workload.go`);
- the neighbour ids the relationships row itself returns. It projects every
  incoming and outgoing neighbour, over all relationship types, as
  `coalesce(x.id, x.uid)`.

The last source makes every uid-keyed label reachable, not only the code
labels. The old predicate matched any node whose `id` or `uid` equals the
input, so the question for each label is whether an index-backed seek finds
the same node.

| Label group | Neo4j schema | Writers and identity | id vs uid | Reachable as an entity id | Anchor branch |
| --- | --- | --- | --- | --- | --- |
| Parsed content entities: every label in `projector/canonical` `entityTypeLabelMap` except Module and Parameter (Function, Class, Variable, K8sResource, TerraformResource, SqlTable, Flux*, CloudFormation*, Package*, ...) | `<label>_uid_unique` | canonical entity writer (`canonicalNodeEntity*Template`: `MERGE (n:%s {uid: row.entity_id}) SET n += row.props`, props `id = entity.EntityID`); semantic entity writer (`MERGE ... {uid: row.entity_id}`, `SET n.id = row.entity_id` or properties `id = entity_id`) | equal | yes | uid constraint |
| Module (semantic entities) | `module_uid_unique` | semantic writer MERGEs on uid and sets `id` to the same value. The canonical import-graph Module MERGEs on `(name, lang)` with no id or uid. | equal, or neither | yes | uid constraint |
| File | `file_uid_unique` | canonical file writer MERGEs on `path` and sets `f.uid`; it never sets `id` | no `id` | yes (search and call-graph results) | uid constraint |
| Reducer or dedicated-writer uid labels: CloudResource, KubernetesWorkload, KubernetesNamespace, CidrBlock, PrefixList, SecurityGroupRule, IncidentRoutingEvidence, CodeTaintEvidence, ExternalPrincipal, SecretsIAM*, ShellCommand, TerraformStateResource, OCI and package-registry labels | `<label>_uid_unique` | `MERGE (x:Label {uid: row.uid})` then `SET x.id = row.uid`. ShellCommand's edge writer sets no id. The dependency-target upsert sets `id` ON CREATE to the same value it MERGEs uid on. | equal, or no `id` | yes (infra, cloud, supply-chain reads) | uid constraint |
| Rationale, DocumentationSection | `rationale_uid`, `documentation_section_uid` RANGE indexes (new here, Neo4j only) | `MERGE (rationale:Rationale {uid: row.rationale_uid})` (`canonical_rationale_edges.go`) and `MERGE (section:DocumentationSection {uid: row.section_uid})` (`canonical_documentation_edges.go`); no `id` | no `id` | yes: the relationships row returns them as `source_id` of EXPLAINS and DOCUMENTS neighbours | uid index |
| Repository, Workload, WorkloadInstance, Platform, Endpoint, EvidenceArtifact, CloudAction | `REQUIRE x.id IS UNIQUE` | MERGE on `id`; no writer sets `uid` | no `uid` | yes: Repository and Workload ids come straight from resolve | id constraint |
| Parameter, Directory, name-keyed import Module, Environment, Ecosystem, Tier, CodeownerTeam, SourceLocalRecord | other keys | MERGE on name, path or ref keys; no `id` or `uid` | neither | the old predicate never matched them | not needed |

Every `SET <alias>.id = ...` in `go/internal/storage/cypher` and
`go/internal/reducer` was checked against its MERGE key. On uid-keyed labels
it is always the MERGE value. So a `{uid: x}` seek on the uid-keyed labels,
and an `{id: x}` seek on the id-constrained labels, find exactly the nodes
`(e.id = x OR e.uid = x)` found.

## Fix

`codemodel.Neo4jEntityIDAnchor(alias, param)` renders:

```cypher
CALL () {
  MATCH (e:<117 uid-constrained labels> {uid: $entity_id}) RETURN e
  UNION
  MATCH (e:DocumentationSection|Rationale {uid: $entity_id}) RETURN e
  UNION
  MATCH (e:CloudAction|Endpoint|EvidenceArtifact|Platform|Repository|Workload|WorkloadInstance {id: $entity_id}) RETURN e
}
```

`UNION` returns a node once even if more than one branch reaches it. The
empty variable scope clause `CALL () {` needs Neo4j 5.23. That is the
documented floor (`graph-backend-installation.md`; the call-chain builder
already ships `CALL (start, end) {`). The Neo4j 5 Cypher manual says the
scope clause was introduced in 5.23 and that "As of Neo4j 5.23, it is
deprecated to use `CALL` subqueries without a variable scope clause." Other
code still uses the deprecated form: `rg 'CALL \{'` finds 44 matches in 27
non-test Go files, some on NornicDB-only paths or in comments. Moving them is
a separate follow-up.

Where each read uses it:

- `relationshipsGraphRow` entity-id branch:
  `codemodel.RelationshipGraphRowCypherFromAnchor(Neo4jEntityIDAnchor("e", "$entity_id"))`.
  The name and repo-anchored branches render the same Cypher as before, byte
  for byte.
- `story.GraphCypher`: the anchored endpoint (`source` outgoing, `target`
  incoming) binds through the CALL anchor. The repo and grant predicates
  (`RepoPredicates`) stay in the `WHERE` after the MATCH, unchanged.
- `story.ClassMethodsCypher`: `class` binds through the CALL anchor. The
  grant predicates on `class` and `method` stay in the `WHERE`. A single
  `:Class` anchor would drop rows: the class-hierarchy story resolves
  Interface, Trait, Struct, Enum and Protocol entities, a caller can pass any
  entity id, and File and Function nodes also CONTAIN Functions.
- `story.InheritanceDepthCypher`: the pattern already requires `:Class`, so
  the anchor is inline: `(target:Class {uid: $entity_id})` for incoming and
  `(source:Class {uid: $entity_id})` for outgoing. The canonical and semantic
  entity writers both set Class `id = uid`. The endpoint grants and the scoped
  interior `all(node IN nodes(path) ...)` predicate are unchanged.
- The builders' `predicate func(string, string) string` parameter is gone.
  Every caller passed `graphEntityIDPredicate`, and the Neo4j anchor has to
  change the MATCH, not the WHERE.
- The `source_sha256` pins in `queryplan/testdata/query-source-coverage.yaml`
  for `relationshipsGraphRow`, `relationshipStoryClassMethods`,
  `relationshipStoryInheritanceDepthRows`, and
  `relationshipStoryGraphRowsForDirection` were re-derived. Their classes
  (`keyed_support`, `single_key`) and bounds are unchanged: each read still
  binds one entity and keeps its RunSingle or `LIMIT`.

A label-qualified `WHERE` was tried first, since it would have kept the
predicate hook:
`WHERE (e:<uid labels> AND e.uid = $x) OR (e:<id labels> AND e.id = $x)`.
Neo4j planned it as a `UnionNodeByLabelsScan` at 384,875 db hits, no better
than the `AllNodesScan`, so it was dropped.

### Schema change

A new Neo4j-only list, `neo4jUIDLookupIndexes`, adds two statements. It is
gated by the schema dialect, the mirror of the NornicDB-only
`nornicDBMergeLookupIndexes`:

- `CREATE INDEX rationale_uid IF NOT EXISTS FOR (r:Rationale) ON (r.uid)`
- `CREATE INDEX documentation_section_uid IF NOT EXISTS FOR (s:DocumentationSection) ON (s.uid)`

These are RANGE indexes, not uniqueness constraints. A constraint would fail
to create on an existing graph that already holds duplicate uids, and the
anchor only needs a seek.

They stay off NornicDB on purpose. NornicDB readers resolve the label first
and never use `Neo4jEntityIDAnchor`. A NornicDB fingerprint bump would make
bootstrap re-apply the full schema on every existing NornicDB store, and a
re-issued `CREATE INDEX IF NOT EXISTS` re-backfills existing property indexes
there (`nornicdb-pitfalls.md`, section "Pitfall: `CREATE INDEX IF NOT
EXISTS` Rebackfills Existing Property Indexes"). The NornicDB statement list, fingerprint
(`f957752d...`) and compatible list are byte-identical to base `a95dd0d54d`.
`TestSchemaUnconstrainedUIDIndexesAreNeo4jOnly` pins the fingerprint, and a
statement dump from both trees compares equal.

Only the Neo4j fingerprint moves, from `9041fb74...` to `dc9d1cfb...` (254 to
256 statements). The bump is additive. The writers already MERGE both labels
on uid, no MERGE or MATCH identity changes, and a writer on the previous
schema writes the same graph. The previous Neo4j fingerprint is recorded as
`graphSchemaNeo4jPreUnconstrainedUIDIndexFingerprint` in
`schema_predecessors.go` and listed as compatible, ahead of the #6793 and
#6541 predecessors. `TestSchemaApplicationsDeclareCompatibilityDecision` pins
the decision. `TestEnsureSchemaAppliesUnconstrainedUIDIndexesOnNeo4jOnly`
drives the real execution path, which walks the DDL tables separately from
the statement listing. It checks that Neo4j executes both indexes, that
NornicDB executes neither, and that every listed statement is executed.

DDL cost on the fixture below: 66.6 ms and 109.3 ms to create the two
indexes over 2,000 nodes each, and 137 ms for `db.awaitIndexes`. The Neo4j
cutover applies the schema on a new, empty graph, so building the indexes
there costs nothing. Until an existing Neo4j graph has the indexes, the
middle branch plans as a `UnionNodeByLabelsScan` over just those two labels:
8,244 db hits on the fixture, still with the right rows.

### Label-list pins

`codemodel` may not import `internal/graph`, so the three label lists are
copied into it. Two tests keep the copies honest:

- `TestNeo4jEntityIDAnchorLabelsMatchSchema` parses
  `graph.SchemaStatementsForBackend(neo4j)`. It fails if the uid-constraint,
  uid-index (unconstrained) or id-constraint set drifts from its list, or if
  a label appears in the id list and a uid list at once.
- `TestNeo4jEntityIDAnchorCoversEveryUIDWriter` scans every non-test `.go`
  file under `storage/cypher` and `reducer` for a literal
  `MERGE (x:Label {uid:`. It also takes every canonical `entityTypeLabelMap`
  label, since the canonical writer MERGEs those on uid through a template.
  It fails when one of those labels is in none of the three lists. That is
  how F1 happened: a uid writer with no constraint and no index. It also
  fails on a literal id-keyed `MERGE (x:Label {id:` whose label is
  uid-anchored, since that could write a node whose id is not its uid. A
  planted `MERGE (f:Function {id: row.id})` failed the test and removing it
  passed.

One known gap: the semantic writer's string-concatenated MERGE
(`"MERGE (n:" + label + " {uid: ..."` in `semantic_entity_statements.go`)
is invisible to the literal scan. Today every label it receives is
uid-constrained, so no label is missed. Enumerating the semantic label set
would close the gap; that is left as a follow-up.

## Tests

- `codequery/relationship_uid_anchor_test.go`:
  `TestNeo4jRelationshipsGraphRowAnchorsOnIndexedEntityID` and
  `TestNeo4jRelationshipStoryReadsAnchorOnIndexedEntityID` (five subtests:
  class methods, inheritance outgoing and incoming, graph outgoing and
  incoming). They drive the unchanged reader methods through a capturing fake
  graph, so they compile on base `a95dd0d54d`. On that base all six fail with
  "Neo4j read still renders the id-OR-uid anchor", re-verified by the
  independent reviewer; the same result was first seen on the pre-rebase
  parent `6085458ac1`. On the first cut of this change, before the
  unconstrained-uid branch, they failed on the missing
  `DocumentationSection|Rationale` branch and the missing `CALL () {`.
- `codemodel/neo4j_entity_anchor_test.go`: the two pins above and the
  rendered clause shape. Seeded violation: removing `Rationale` from
  `neo4jEntityUIDIndexAnchorLabels` fails both pins, naming
  `canonical_rationale_edges.go`. Restoring it passes.
- `graph/schema_entity_uid_indexes_test.go`:
  `TestSchemaIndexesUnconstrainedUIDLabels` requires both index statements in
  the Neo4j and NornicDB schema. It failed before the DDL was added.
- Updated for the new signatures, with their assertions kept:
  - `auth_scoped_relationship_story_clause_test.go` (grant and repo predicate
    binding, scoped and unscoped)
  - `relationship_story_cross_repo_test.go`
  - `relationship_story_grant_clause_live_test.go`
  - `entity_resolution_test.go` (handler-level anchor)
  - `relationship_story_test.go`
  - `relationship_story_class_test.go`: its fake now routes on the anchor
    text.

## Performance Evidence

Performance Evidence: live `PROFILE` on Neo4j Community, image
`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`,
in an isolated container on ports 59474/59687. The container was removed
afterwards.

The schema was the full Neo4j DDL from `graph.SchemaStatementsForBackend`:
254 of 256 statements applied. The two failures are the procedure-syntax
full-text fallbacks.

The seed had 128,550 nodes and 348,499 relationships:

- Nodes: 50 Repository, 10,000 File, 60,000 Function, 10,000 Class, 20,000
  Variable, 2,000 each of Struct, Interface and TypeAlias, 3,000 Module, 5,000
  TerraformResource, 5,000 K8sResource, 5,000 CloudResource, 500 Workload,
  2,000 Rationale and 2,000 DocumentationSection.
- Edges: REPO_CONTAINS, CONTAINS (File to entity, Class to method), 180,000
  Function CALLS, INHERITS chains, REFERENCES, PROVISIONS, DEFINES, EXPLAINS
  and DOCUMENTS.

Every uid-keyed node has `id = uid` except File, Rationale and
DocumentationSection, which, like their writers, carry no `id`.

Each query is the production builder's rendered text. A scratch program
printed it from base `a95dd0d54d` (a throwaway detached worktree) and from
this branch. Each query ran once to warm the plan cache and once under
`PROFILE`. Timings are the median of five warm runs over HTTP. Result rows
were compared sorted, before and after: all 23 cases returned identical rows.

| Read | Input | Before db hits (operator) | After db hits | Before / after ms | Rows |
| --- | --- | --- | --- | --- | --- |
| relationships row | Function id | 389,567 (AllNodesScan) | 4,014 | 60.6 / 6.5 | 1 |
| relationships row | Class id | 386,497 | 970 | 61.3 / 5.2 | 1 |
| relationships row | File uid | 387,402 | 1,894 | 62.7 / 6.7 | 1 |
| relationships row | TerraformResource / K8sResource / Variable / Module id | 385,830 / 385,814 / 385,922 / 385,814 | 308 / 292 / 399 / 292 | 59.6-83.6 / 3.4-6.7 | 1 each |
| relationships row | CloudResource uid | 385,756 | 233 | 59.0 / 3.8 | 1 |
| relationships row | Rationale uid | 385,767 | 245 | 62.5 / 6.7 | 1 |
| relationships row | DocumentationSection uid | 385,765 | 243 | 72.7 / 6.6 | 1 |
| relationships row | Repository id | 402,950 | 17,636 | 99.5 / 29.7 | 1 |
| relationships row | Workload id | 385,728 | 205 | 81.7 / 5.7 | 1 |
| relationships row | missing id | 385,651 | 126 | 72.7 / 7.5 | 0 |
| story graph, outgoing CALLS | Function id | 1,689,613 (AllNodesScan) | 422 | 548.6 / 8.3 | 3 |
| story graph, incoming CALLS | Function id; Class id | 1,689,756; 1,689,349 | 572; 133 | 310.6 / 3.7; 289.0 / 4.7 | 5; 0 |
| story graph, outgoing | missing id | 1,689,349 | 126 | 227.7 / 2.8 | 0 |
| story class methods | Class id; File uid; missing | 894,003; 894,019; 894,001 (NodeByLabelScan) | 140; 195; 126 | 84.4-97.5 / 3.0-6.3 | 1; 6; 0 |
| story inheritance outgoing | Class id, depth chain 3 | 20,031 (NodeByLabelScan + seek) | 23 (one NodeUniqueIndexSeek) | 5.5 / 2.9 | 3 |
| story inheritance incoming | Class id; missing | 20,422; 20,002 | 345; 1 | 6.1 / 2.5; 5.0 / 2.2 | 26; 0 |

Every after plan has no scan operator. The CALL anchor plans as 126 index
seeks: 124 `NodeUniqueIndexSeek` (117 uid-constraint labels and 7
id-constraint labels) and 2 `NodeIndexSeek` (the new uid indexes). A missing
id costs about 126 db hits, paid once per request, not per row. The
Repository row's after cost is the relationship expansion of a repository
with 200 files (REPO_CONTAINS and DEFINES), not the anchor.

F1 regression, followed live. The Rationale and DocumentationSection ids
were taken from the `incoming[].source_id` of a relationships row
(`rationale:5` from Function `fn5`; `doc-section:0` and `rationale:100` from
`fn100`), then passed back as `entity_id`:

| Build | Rows for each followed id | Plan |
| --- | --- | --- |
| base `a95dd0d54d` (id-OR-uid) | 1 | AllNodesScan, about 385,765 db hits |
| first cut of this change (uid and id constraints only) | 0 | 124 NodeUniqueIndexSeek |
| this change | 1, identical to base | 124 NodeUniqueIndexSeek plus 2 NodeIndexSeek, 243-245 db hits |

The anchor text is long, so its cold planning cost was measured too. A
unique comment was appended to defeat the plan cache, and the median of five
cold runs (plan plus execute) was taken:

- relationships row: 45.5 ms new against 90.5 ms old
- incoming story read: 21.9 ms new against 225.1 ms old

These are query-level numbers on a 129k-node fixture. The old cost grows
with total node count, since `AllNodesScan` touches every node in the
database; the new cost does not. This makes no claim about end-to-end wall
time for `POST /api/v0/code/relationships` on the 984-repo corpus. The trial
report says the handler's non-graph work is a large part of its 1.27s p50,
so whether the endpoint gets under 1s has to be measured on that corpus.

## Live grant-leak probe moved to Neo4j

`TestLiveNornicDBRelationshipStoryCompatBuilderMustNotLeakUngrantedRows`
(scheduled class, tag `live_nornicdb_relationship_story`) fed the Neo4j-compat
story builder to NornicDB. That builder now carries the `CALL () { ... UNION
... }` anchor, which NornicDB rejects ("unsupported clause after CALL {}:
MATCH"); production never sends it there. The probe was moved, not dropped:
`TestLiveNeo4jRelationshipStoryCompatBuilderMustNotLeakUngrantedRows`
(`relationship_story_grant_neo4j_live_test.go`, tag
`live_neo4j_relationship_story`, ledger row `backends: neo4j`) runs the same
builder on Neo4j against the shared two-tenant fixture, seeded into the `neo4j`
database. It also asserts the granted callee is present, so an empty result
cannot pass. The historical name in the 5167 evidence notes is left as the
record of that earlier run.

Run on `neo4j:2026-community` (digest as above): PASS with one row, source
`LiveClauseAnchorFn`, target `LiveClauseGrantedCallee`. Seeded mutation, with
the grant predicate dropped from `story.RepoPredicates`: FAIL with three rows,
returning `LiveClauseUngrantedCallee` and `LiveClauseOrphanCallee`. The mutant
was reverted and the probe passed again.

## Observability Evidence

No-Observability-Change: these are query-shape changes inside existing
readers and builders, plus two additive schema indexes. They add no metric,
span, or log key. The existing graph-read telemetry on these handlers (query
duration, row counts, and the graph deadline error path) still covers the
same reads. The existing schema bootstrap logs report each new Neo4j index
statement with its backend, ordinal, duration and failure class.
