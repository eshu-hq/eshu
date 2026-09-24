# #7057: Neo4j code reads anchor on uid, not an unindexed id-OR-uid scan

## Problem

Several Neo4j-dialect code reads found their anchor node with
`codemodel.GraphEntityIDPredicate`, which renders `(alias.id = x OR alias.uid = x)`
in `WHERE`. On Neo4j, `uid` has a uniqueness constraint on every code label
these reads touch (`uidConstraintLabels` in
`go/internal/graph/schema_tables.go`). `id` has no index or constraint on any
of those labels in the Neo4j schema. NornicDB has a `Function.id` lookup index
(`nornicdb_function_legacy_id_lookup`), but none of the NornicDB branches
change here. The OR cannot use the uid constraint, so Neo4j plans it as a
label scan (or, for an unlabeled `MATCH (e)`, an `AllNodesScan`) on every
request, once per anchored id.

The live report came from a 984-repo Neo4j corpus
(`n4jtrial-neo4j-984-8d5949f90-20260924T101450Z`):
`code/divergence/findings` returned HTTP 500 ("graph query exceeded its
deadline") on 3 of 11 repos, and `code/divergence/report` and its MCP tools
took 10-13s. PROFILE there showed 42.4M db hits in 16.9s for 40 ids over
530,540 Functions.

## Why uid alone matches the same nodes

- The canonical entity writer MERGEs on `uid` and sets `id` from the same
  `EntityID`: `canonicalEntityProperties`
  (`go/internal/storage/cypher/canonical_node_writer_entities.go`) sets
  `properties["id"] = entity.EntityID`, and `canonicalNodeEntityUpsertTemplate`
  (`canonical_node_cypher.go`) runs `MERGE (n:%s {uid: row.entity_id}) SET n += row.props`.
  `id` and `uid` are both reserved metadata keys, so metadata cannot overwrite
  either one.
- `File` nodes never get an `id` property. Only `f.uid` is set.
- Every live CALLS writer MATCHes both endpoints by `{uid:}` on
  `Function|Class|Struct|Interface|TypeAlias|File`
  (`edge/writer/code_call_labels.go` `codeCallEndpointLabels`, and
  `BatchCanonicalCodeCallUpsertCypher` for the `Function|Class|File`
  fallback). The only `{id:}` CALLS template, `BuildCanonicalCodeCallUpsert`,
  has no production caller. So any node that can start or end a CALLS walk
  carries one of those six labels and a uid equal to its id.

So for these reads, a `{uid: x}` anchor on the right label set returns the
same rows as the OR predicate. The live row comparison below confirms it.

## Fix

Eleven Neo4j builders and readers change, twelve anchors in all (call-chain
anchors both ends). The NornicDB branch of every builder is byte-unchanged.

Divergence, wrapper-bypass, and compare-paths:

- `codequery/outlier.go` `BuildOutlierCalleeEdgesCypher`:
  `MATCH (member:Function {uid: mid})`.
- `codequery/wrapper_bypass_cypher.go`, six builders:
  `BuildWrapperCallersCypher`, `BuildWrapperFanInCypher`,
  `BuildWrapperCalleesCypher`, `BuildWrapperFamilyCallersCypher`,
  `BuildWrapperFamilyFanInCypher`, `BuildWrapperFamilyCalleesCypher`. Each
  uses `(alias:Function|Class|File {uid: ...})`, and the repo and grant
  predicates move into one `WHERE` after the hop. Production calls the
  Family variants and `BuildWrapperCalleesCypher`; `BuildWrapperCallersCypher`
  and `BuildWrapperFanInCypher` have no production caller (only tests), and
  were changed so all six stay consistent.
- `codequery/compare_paths_cypher.go` `BuildComparePathsHopCypher`: the same
  anchor.

Call-chain and transitive relationships:

- `codequery/chain/cypher.go` `BuildCallChainCypher`. This serves
  `POST /api/v0/code/call-chain` (`handleCallChain`) and the MCP call-chain
  tool. An endpoint given by entity id now renders
  `(start:Function|Class|Struct|Interface|TypeAlias|File {uid: $start_entity_id})`
  (and the same for `end`). Name lookups are unchanged.
- `codequery/callers.go` `callChainCandidateOneHopRows`: the one-hop read
  that the call-chain name resolver walks. The source is anchored the same
  way. Before, it was an unlabeled `MATCH (source)-[:CALLS]->(target)` with
  an id-OR-uid `WHERE`, which planned as an `AllNodesScan`. Its typed
  `non_hot` (`degree_bounded`, `single_key`) `source_sha256` in
  `go/internal/queryplan/testdata/query-source-coverage.yaml` was
  re-derived. The class still holds: one uid-anchored source and one CALLS
  hop.
- `codemodel/code_relationships_graph_response.go`
  `BuildTransitiveRelationshipRowsCypher`. This is the transitive walk behind
  `/api/v0/code/relationships`. The Neo4j branch anchors on
  `(e:Function|Class|Struct|Interface|TypeAlias|File {uid: $entity_id})`
  instead of an unlabeled `MATCH (e) WHERE (e.id = ... OR e.uid = ...)`. A
  node outside those labels has no CALLS edge, so it never produced a row. The
  new `codemodel.CallGraphEndpointLabels` constant holds that label set, and
  `chain.AnchorLabelDisjunction` now aliases it.

## Tests (each RED on base 669c2e6d5b, GREEN on this branch)

RED was run by copying the branch's test files onto a throwaway detached
worktree at 669c2e6d5b and running the same tests there.

- `codequery/uid_anchor_test.go` `TestNeo4jWrapperAndCompareAnchorsSeekUID`
  has one subtest per builder (the six wrapper builders and
  `BuildComparePathsHopCypher`), each run scoped and unscoped. It asserts the
  exact `(alias:Function|Class|File {uid: <param>})` anchor and rejects any
  `.id =`. All seven subtests fail on base and pass on the branch.
- `codequery/outlier_cypher_test.go`: the outlier anchor.
- `codequery/chain/call_chain_anchor_test.go`: the entity-id anchors, plus a
  mixed id/name case with a repo scope and a grant.
- `codequery/call_chain_candidate_test.go`: the one-hop anchor with no
  bound, with a repo bound, and with a grant scope. It also checks that no
  empty `WHERE` is rendered.
- `codemodel/code_relationships_graph_response_test.go`: the transitive
  anchor in both directions, and a check that the NornicDB branch keeps its
  own shape.
- Handler-level pins updated to the new anchor: `metrics/call_graph_contract_test.go`
  and `entity_resolution_test.go`.

## Performance Evidence

Performance Evidence: live `PROFILE` on Neo4j 2026.08.1 Community, image
`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
(the pin in `docker-compose.live-backend-neo4j.yml`), in an isolated
container that was removed afterwards. The seed had 50,000 Function, 10,000
Class, 10,000 File, 2,000 each of Struct, Interface, and TypeAlias, and
20,000 Variable nodes (which carry no CALLS edges): 96,000 nodes and 205,997
CALLS edges. Each label has a `<label>_uid_unique` constraint, created with
the same statement `uidConstraintLabels` produces. `id = uid` on every node
except File, which has no `id`.

Each query is the production builder's rendered text, printed by a scratch
program from base 669c2e6d5b and from this branch (the one-hop reader is an
inline string, so its two shapes are copied from the diff). Each ran once to
warm the plan cache, then once more under `PROFILE` for the db hits shown.
Result rows were compared sorted, before and after: every case below returned
identical rows.

These are query-level measurements on a 96k-node fixture. They make no claim
about end-to-end endpoint wall time on the 984-repo corpus. That still has to
be measured on the Neo4j cutover corpus.

| Read | Input | Before db hits (scan operator) | After db hits (operator) | Rows |
| --- | --- | --- | --- | --- |
| wrapper family-callers | 40 ids (Function/Class/File/missing), repo bound | 5,714,300 (NodeByLabelScan x3) | 2,414 (NodeUniqueIndexSeek x3) | 104 |
| wrapper family-callers, grant scoped | same | 5,714,300 | 2,414 | 104 |
| wrapper family-fan-in | same 40 ids | 5,712,466 | 580 | 28 |
| wrapper family-callees | same 40 ids | 5,712,534 | 638 | 54 |
| wrapper callers / fan-in / callees | 1 id | 142,865 / 142,819 / 142,820 | 68 / 22 / 23 | 3 / 1 / 2 |
| compare-paths hop | Function id / File id | 142,824 / 142,810 | 27 / 12 | 2 / 1 |
| outlier callee-edges | 40 Function ids | 4,001,372 (NodeByLabelScan) | 1,292 (NodeUniqueIndexSeek) | 80 |
| call-chain, entity ids | fn to fn, depth 5 | 456,030 (UnionNodeByLabelsScan x2) | 34 (NodeUniqueIndexSeek x12) | 1 |
| call-chain, entity ids | Struct to Function; missing start id | 456,025; 228,006 | 29; 6 | 1; 0 |
| call-chain, entity ids | grant scoped, repo bound | 310,091 (NodeByLabelScan x12) | 16 | 0 |
| call-chain, id start and name end | start by id, end by name | 380,031 | 152,033 (see below) | 1 |
| transitive relationships | Function out / in, depth 4 | 289,183 / 288,484 (AllNodesScan) | 1,190 / 491 (NodeUniqueIndexSeek x6) | 202 / 88 |
| transitive relationships | Struct out; File out; missing id | 288,354; 288,713; 288,001 | 361; 720; 6 | 61; 121; 0 |
| call-chain name-resolver one-hop | Function id; File id | 288,025; 288,010 (AllNodesScan) | 32; 16 (NodeUniqueIndexSeek x6) | 3; 1 |

The grant-scoped call-chain case returned 0 rows both before and after because
the fixture nodes carry no grant scope properties. It shows the predicate still
binds and the plan changes, not that grants filter correctly. Grant and repo
predicate preservation is proven by the unit tests above, not by this fixture.

In the mixed id/name call-chain case only the id endpoint gets the uid seek.
The name endpoint still plans as a `UnionNodeByLabelsScan` because `name` has
no index; that read is unchanged and out of scope here. It still drops from
380,031 to 152,033 db hits.

Before-side cost grows linearly with node count: every changed read went from
a full label or all-node scan to a fixed number of index seeks. The issue's
984-repo corpus had 80 db hits per Function on the outlier anchor
(42,443,629 / 530,540). The outlier fixture here shows the same per-node cost
(4,001,372 hits over 50,000 Functions).

### Live tests

`TestLiveOutlierBackendParity` (tag `live_nornicdb_convention_outlier`) ran
against the same pinned Neo4j container with `ESHU_LIVE_GRAPH_BACKEND=neo4j`
and passed. It drives `assembleOutlierTrack`, so it exercises the changed
outlier anchor end to end.

The wrapper-bypass and compare-paths live tests
(`live_nornicdb_wrapper_bypass`) are NornicDB-only: they hard-code the
`nornic` database and `GraphBackendNornicDB`. The NornicDB branch of every
changed builder is byte-unchanged, so those tests do not exercise any changed
text. They show only that the unchanged dialect still works, and they are not
a regression proof for the Neo4j change. The Neo4j proof for the wrapper and
compare-paths anchors is the PROFILE table above plus the unit tests.

## Remaining scans (stacked follow-up, not changed here)

Four Neo4j reads still anchor with an id-OR-uid predicate:

- `relationshipsGraphRow` (`codequery/relationship_handlers.go`) runs first on
  every `/api/v0/code/relationships` request that carries an `entity_id`. It
  renders `MATCH (e) WHERE (e.id = $entity_id OR e.uid = $entity_id)`. The
  lookup is label-agnostic: it can return Repository and other non-code nodes,
  and some of those are keyed only by `id` (`Repository {id: ...}` is used
  elsewhere in the same handler). Anchoring it on the code labels would drop
  those rows, so it needs a label-resolution step first (the NornicDB route
  already does this through `nornicDBRelationshipEntityLabel`).
- `relationshipStoryGraphCypher` (`codequery/relationships/story/graph.go`)
  renders an unlabeled relationship pattern,
  `MATCH (source)-[rel:<TYPE>]->(target)`, with the id-OR-uid predicate on the
  anchor end. The same label-resolution question plausibly applies.
- `relationshipStoryClassMethodsCypher` (`story/class.go`) renders
  `MATCH (class)-[:CONTAINS]->(method:Function)` with the predicate on the
  unlabeled `class` end. It can likely take a direct label plus `{uid:}` anchor.
- `relationshipStoryInheritanceDepthCypher` (`story/class.go`) already labels
  both ends `:Class` and applies the predicate in `WHERE`. It can likely take
  the `{uid:}` anchor directly, with no label-resolution step.

The ~288k-db-hit `AllNodesScan` in the table above was measured on the
unlabeled one-hop and transitive shapes only. The story reads were not
profiled here. All four land in a stacked follow-up PR under #7057, before
that issue closes.

## Observability Evidence

No-Observability-Change: these are query-shape changes inside existing
builders. They add no metric, span, or log key. The existing graph-read
telemetry on these handlers (query duration, row counts, and the graph
deadline error path) still covers the same reads.
