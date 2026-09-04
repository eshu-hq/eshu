# #5167 Code Family, Batch 2b — Two Routes Whose Grant Text Did Not Filter

`POST /api/v0/code/call-chain` and `POST /api/v0/code/relationships/story` leave
`pendingRowFilteringRoutes`
(`go/internal/query/auth_scoped_routes_pending_row_filtering.go`) and join the
scoped-token allowlist. The ledger goes from 10 pending routes to 8. It was 22
before batch 1, 12 after it, and 10 after batch 2a.

These two are not like the eight before them. Both already carried repository
predicates before this batch, and neither predicate dropped a row. That was a
theory until it was measured, and measuring it first was the point: promoting a
route on a filter that does not filter would have moved a documented gap into an
undocumented one.

## The Headline: The Predicates Were Inert, And Measured So

`relationshipStoryRepoPredicates` (`code_relationship_story_graph.go`) rendered,
for a scoped caller:

```text
sourceRepo.id IN $relationship_repo_ids AND targetRepo.id IN $relationship_repo_ids
```

Both consumers — `relationshipStoryGraphCypher` and
`nornicDBRelationshipStoryGraphCypher` — attached it to a `WHERE` that follows
their `OPTIONAL MATCH` repository chains. `nornicDBCallChainOneHopRows`
(`code_call_chain_nornicdb.go`) did the same with its traversal bound. A `WHERE`
in that position constrains the optional pattern, not the driving row set, so
the out-of-grant row survives with the last optional pattern's variables nulled.

Measured against the pinned replay-tier image
(`timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d…`, which self-reports
`1.2.2`), on a seeded two-tenant graph, with the caller granted one of the two
repositories:

| Statement | Seeded | Returned | Verdict |
| --- | --- | --- | --- |
| `nornicDBRelationshipStoryGraphCypher`, outgoing | 1 granted + 1 out-of-grant + 1 unattributed callee | **3 rows** | inert |
| `nornicDBRelationshipStoryGraphCypher`, incoming | 1 granted + 1 out-of-grant caller | **2 rows** | inert |
| `relationshipStoryGraphCypher` (Neo4j compat), outgoing | 5 `CALLS` edges in the graph | **5 rows** | inert, and the anchor was inert too |
| `nornicDBRelationshipStoryClassMethodsCypher`, out-of-grant class | 1 out-of-grant method | **1 row** | no binding at all |
| `nornicDBRelationshipStoryInheritanceDepthCypher`, granted class | 1 out-of-grant ancestor | **1 row** | no binding at all |
| `nornicDBCallChainOneHopRows`, bound to one repository | 3 callees | **3 rows** | inert |
| `nornicDBRelationshipMetadataCypher`, wrong repository | 1 entity | **0 rows** | filters, fail-closed |

Two details make the story leak worse than "rows come back with null repository
columns". First, `normalizeNornicDBRelationshipStoryRows` collapses
`target_repo_id` by preferring the node property `target.repo_id` — which is
never nulled — over the `targetRepo.id` fallback the predicate nulled, so the
out-of-grant repository id reached the response body in full, alongside the
entity id, name, file path and language. Second, the compat builder put its
ANCHOR predicate in the same inert position, so that statement had no working
anchor at all: it returned every `CALLS` edge in the graph, bounded only by
`LIMIT`. That is a correctness and cost defect for every caller class, and
for a scoped caller a tenancy leak as well: with the anchor predicate and the
grant predicate both inert in the same clause, a scoped caller of that builder
received every `CALLS` edge in the graph. It does not depend on NornicDB —
`OPTIONAL MATCH … WHERE` has the same semantics on Neo4j, which is the lane that
builder ships to.

Root-Cause Evidence: the cause is clause position, and the observation that
establishes it is the row-level difference above — the same predicate text
returns 3 rows after an `OPTIONAL MATCH` and 1 row in the anchoring `MATCH`,
against the same seeded graph in the same session, with the granted callee kept
and both the out-of-grant and unattributed callees dropped. The reduced- and
full-projection candidates were measured separately so the fix was not confused
with the finding. The measurement predates any production edit and is reproduced
by the live tests named under Verification.

## What Moved

Each route makes the ledger header's five moves: a matcher, an advertised entry
classed `scopedRouteGrantBound`, the `x-scoped-token-support` marker and the
policy `403` on its OpenAPI operation, and removal from the pending map. Both
join `scopedCodeGraphGrantRoute`
(`go/internal/query/auth_scoped_routes_code_flow.go`).

| Read | Where the grant binds now | Symbol |
| --- | --- | --- |
| story direct rows, both backends | anchoring `MATCH`'s `WHERE`, on each endpoint's `repo_id` | `relationshipStoryRepoPredicates` (`code_relationship_story_graph.go`) |
| story class methods, both backends | anchoring `MATCH`, on `class.repo_id` and `method.repo_id` | `relationshipStoryGrantPredicates` |
| story inheritance walk, both backends | anchoring `MATCH`, on both path endpoints | `relationshipStoryGrantPredicates` |
| story repo-scoped overrides | anchoring `MATCH`, on `source.repo_id` and `target.repo_id` | `relationshipStoryOverrideRowsCypher` (`code_relationship_story_class.go`) |
| story target resolution | granted repositories queried one at a time instead of the corpus-wide search | `relationshipStoryGrantedCandidates` (`code_relationship_story_resolution.go`) |
| call-chain one hop, NornicDB | anchoring `MATCH`, on `target.repo_id` | `nornicDBCallChainOneHopRows` (`code_call_chain_nornicdb.go`) |
| call-chain candidate probe, compat | the `MATCH`-attached `WHERE` it already had | `callChainCandidateOneHopRows` (`code_call_chain_resolution.go`) |
| call-chain shortestPath endpoints, both builders | anchoring `WHERE`, on `start.repo_id` and `end.repo_id` | `buildCallChainCypher`, `buildNornicDBCallChainCypher` |
| call-chain shortestPath interior hops, Neo4j compat | inside the `all(node IN nodes(path) …)` predicate | `callChainPathHopPredicates` (`code_call_chain.go`) |
| shared metadata anchor (routes 2, 4, 5) | the Repository alias, in its required `MATCH` pair | `nornicDBRelationshipMetadataPredicate` (`code_relationships_nornicdb_identity.go`) |
| call-chain name resolution (SQL) | defense-in-depth grant check before the read | `resolveExactGraphEntityCandidates` (`entity_resolution.go`) |

## Why The Alias Differs Per Statement

`repo_id` is a property the canonical node writer sets on every entity it
projects (`canonicalEntityProperties`, `internal/storage/cypher`), so the
driving row already carries the grant key. That is what lets the predicate sit
in the anchoring `MATCH` while the `OPTIONAL MATCH` clauses stay optional and
stay projection-only: the traversal shape does not change, and the repository
fallback columns still populate.

The metadata anchor is the exception, and deliberately so. It reaches the
repository through two REQUIRED `MATCH` clauses, so binding on the Repository
alias there decides row membership — measured, along with the fail-closed half:
an entity with no File/Repository chain returns nothing. Using the node property
there would have been equally correct and strictly less informative about why.

An entity the graph cannot attribute to any repository now fails the predicate
and is dropped for a scoped caller. That is the fail-closed half batch 1 landed
for `complexityListAnchor`, and it is why the seeded fixture carries an
unattributed callee: it is exactly the row an `OPTIONAL MATCH`-attached
predicate keeps.

## Interior Hops, And The Two Different Shapes That Bound Them

Binding the two endpoints of a variable-length read is not enough when the
projection returns the nodes in between. Call-chain returns every node on the
path, with id, name, labels, language, docstring and method kind, so a chain
whose endpoints are both in grant can still carry an interior hop from a
repository the caller was never granted. Each backend needs a different shape,
and the difference is measured, not stylistic.

**NornicDB.** The response path is a Go-side breadth-first search over
`nornicDBCallChainOneHopRows`, so bounding each hop as the traversal expands
bounds the whole chain. `TestCallChainBoundsEveryFrontierHop` proves an
out-of-grant intermediate cannot join two granted endpoints. This lane cannot
use a path predicate: `all(node IN nodes(path) WHERE node.repo_id IN $ids)` does
not filter on the pinned build, and neither does `none(...)/NOT IN`, a
`size([...]) = 0` comprehension, or an inline literal list; one scalar equality
per allowed value, OR-ed, fails the other way and drops rows that should be
admitted. Only a single scalar equality inside `all(...)` is evaluated. The full
table is in
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md)
under the list-membership entry this batch added, pinned as measured values by
`TestLiveNornicDBPathListPredicateBehaviour`.

**Neo4j compat.** `buildCallChainCypher` issues one `shortestPath` read, so
there is no per-hop moment to bound; the grant goes into the
`all(node IN nodes(path) …)` predicate, which is the only clause that reaches
the interior. That form is a defect on the pinned NornicDB build and works
correctly on Neo4j, which is the lane this builder ships to, and the repo
already emitted exactly that shape there for `$repo_id` and
`$traversal_repo_ids`. `callChainPathHopPredicates` composes the grant as a
conjunct beside the request's own bound rather than replacing it.
`TestCallChainNeo4jLaneBoundsInteriorHops` proves it on both anchoring shapes (a
request with `repo_id` and one without), and
`TestCallChainNeo4jLaneKeepsAnInGrantChain` proves it narrows rather than
empties.

Round-1 review caught that this lane had been left unbounded and untested while
the route was promoted and documented as bounded. The NornicDB measurement is
what led there: "a list predicate does not filter" is a fact about one build,
and it was carried into a lane where the opposite is true. That is recorded
because the reasoning error is more reusable than the fix.

`buildNornicDBCallChainCypher` deliberately gets the endpoint grant and no path
conjunct, since the list form would grant nothing there. It is unreachable from
`handleCallChain` and does not parse on the pin; the comment at that builder
says what would have to happen if it ever became reachable.

The inheritance walk keeps its endpoint bound for the same NornicDB reason and
says so at the call site.

## Two Pitfalls-Page Corrections

Both are in
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md):

1. The pre-bound-endpoint `shortestPath` shape the page records as safe was
   measured on v1.1.11 and does not parse on the current pin —
   `buildNornicDBCallChainCypher`'s exact statement returns
   `shortestPath: could not resolve start variable "start"`. Nothing in
   production reaches that builder (`handleCallChain` sends a NornicDB backend
   to `nornicDBCallChainRows`), so this is dead code that would hard-error, not
   a live outage. It is left in place, bound like every other builder in the
   family, with the parse failure pinned by a test rather than deleted.
2. The list-membership entry above is new.

## Query-Plan Manifests

Three `cypher_sha256` values and fourteen `source_sha256` lines move, across
`hot-cypher.yaml`, `query-source-coverage.yaml` and the
`grandfathered_non_hot.go` baselines, in two commits separate from the semantic
ones. Fourteen lines but thirteen distinct symbols: `QP-CODE-REL-STORY` and
`QP-CODE-REL-STORY-INCOMING` share one builder, so its digest appears twice.

Counted from the diff rather than from memory. The first of those two commits
says "seven source digests" in its message, which undercounts by three: it
missed the `source_sha256` lines in `hot-cypher.yaml` itself, which the same
commit updated. The message cannot be corrected without rewriting a pushed-ready
history, so the number is corrected here, where a reviewer checks it.

No `plan` block changes. The statement each manifest entry actually pins is the
all-scopes, no-`repo_id` shape, which renders no predicate at all — so the
`WHERE` slot the fix relocated is empty there and the statement is identical up
to one blank line, verified by rendering both templates with the same
substitutions and comparing them with whitespace collapsed. For a scoped caller
the anchoring `MATCH` gains a Filter, which adds neither `AllNodesScan` nor
`CartesianProduct` (the forbidden set) and removes none of `NodeIndexSeek`,
`Expand` or `Sort` (the required set), so both entries' plan claims stay
accurate for both caller classes.

## What A Client Can Observe

Scoped callers:

- A story ambiguity list that used to name every tenant's match now names only
  readable ones, so a request that answered `ambiguous` may now resolve.
- A call chain that exists only by passing through an ungranted repository is
  absent, not returned with the hop hidden. That holds on both lanes: NornicDB
  bounds each hop as its Go-side traversal expands, and the Neo4j-compat
  `shortestPath` read carries the grant inside its `all(node IN nodes(path) …)`
  predicate, which is the only clause that reaches the hops between the two
  endpoints.
- An ungranted repository selector on either route returns `400`.
- A token with no repository grants gets `"status": "not_found"` (story) or
  `"chains": []` (call-chain), with no backend read — the same answer as a
  target that does not exist.

Every caller class, scoped or not:

- `repo_id` on the story route now filters. It was inert in the same clause
  position for every caller class.
- The call-chain traversal bound now filters. It was inert too — 3 rows with
  `$traversal_repo_ids` naming one repository, and the identical 3 rows with it
  nil — so a shared-key caller that passes `repo_id` or `cross_repo` gets a
  correctly narrower hop set than before.
- `coalesce(target.repo_id, targetRepo.id, '')` became
  `coalesce(target.repo_id, '')` in the call-chain one-hop read. `targetRepo` is
  not bound at the anchoring `MATCH`, so the fallback could not move with the
  predicate; a target the graph can attribute only through its `REPO_CONTAINS`
  edge, with no `repo_id` of its own, is now dropped.
- On a Neo4j deployment, `POST /api/v0/code/relationships/story` returns a
  different result set. Its anchor predicate sat in the same inert clause, so
  the route returned every `CALLS` edge in the graph up to `limit`; it returns
  the anchor's edges now. Measured 5 rows to 1 on the seeded graph. This is the
  largest observable change in the batch for a shared-key or admin caller.

## Capability Matrix

`specs/capability-matrix.v1.yaml`'s `symbol_graph.inheritance` row is
`production: supported` on `remote_validation: prod-symbol-graph-inheritance`,
whose committed artifact is
`docs/internal/remote-validation/prod-symbol-graph-inheritance.md`. That proof
went through `POST /api/v0/code/relationships`, which this batch does not
promote and does not change the response shape of. Nothing here raises or lowers
a capability row, and the file is not in the diff: this is a tenancy fix with
unit-level, statement-level and live-backend proof, not deployed validation.

## Proof Ledger

The red/green runs and the BITES mutation ledger live in
[#5167 code family batch 2b proofs](5167-code-family-batch-2b-proofs.md), split
out because the two together outgrow the repository's 500-line Markdown cap.

## Verification

Run after the last edit, exit codes captured directly (`cmd; echo $?`, never
after a pipe):

```text
cd go && go test ./internal/query ./internal/mcp/... ./internal/queryplan -count=1   # 0
cd go && go vet ./internal/query ./internal/mcp ./internal/queryplan                 # 0
cd go && go test ./internal/query -tags live_nornicdb_relationship_story \
  -run TestLiveNornicDB -count=1                                                     # 0
cd go && go test ./internal/query -tags live_nornicdb_call_chain \
  -run TestLiveNornicDB -count=1                                                     # 0
mkdocs build --strict --clean --config-file docs/mkdocs.yml                           # 0
git diff --check                                                                      # 0
```

The two live runs need a standalone pinned NornicDB on a non-default Bolt port:

```bash
docker run -d --name nornic-5167-e2 -e NORNICDB_EMBEDDING_ENABLED=false \
  -e NORNICDB_NO_AUTH=true -p 17987:7687 \
  timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74
```

No-Regression Evidence: a correctness change with no latency claim attached; no
benchmark was run and no speedup is asserted. Every predicate added is an
`IN`/`=` membership test on a node the query already matched, and every one of
them moves EARLIER in its statement — from a trailing `WHERE` into the anchoring
`MATCH`'s own, or into the `all(node IN nodes(path) …)` clause that runs with the
traversal — so filtering happens before `SKIP`/`LIMIT` rather than after. No
statement gains a clause, a hop, or a second round trip.

Row counts do change, and not only for scoped callers. Three shapes are
affected, each measured rather than reasoned about:

1. `relationshipStoryGraphCypher` (Neo4j-compat story) returned every `CALLS`
   edge in the graph, because its ANCHOR predicate sat in the inert clause
   alongside the grant. It returns the anchor's edges now: 5 rows to 1 on the
   seeded graph, pinned by
   `TestLiveNornicDBRelationshipStoryCompatBuilderMustNotLeakUngrantedRows`.
   That applies to every caller class, and makes the route strictly cheaper as
   well as correct.
2. `nornicDBCallChainOneHopRows`' traversal bound was inert — 3 rows with
   `$traversal_repo_ids` naming one repository, 3 rows with it nil — and now
   filters, so an unscoped caller passing `repo_id` or `cross_repo` gets a
   narrower hop set. Its `coalesce` fallback narrowed with it, dropping a target
   attributable only through `REPO_CONTAINS`. Pinned by
   `TestLiveNornicDBCallChainOneHopMustNotLeakUngrantedTargets` and
   `TestLiveNornicDBCallChainOneHopUnscopedIsUnchanged`, which together show the
   bound applying and an unbounded call still returning all three rows.
3. `buildCallChainCypher` gains a grant conjunct inside its path predicate, for
   a scoped caller only. An unscoped caller's statement is unchanged, pinned by
   `TestShortestPathCallChainBuildersBindTheGrant/unscoped_carries_no_grant` and
   `TestCallChainNeo4jLaneSharedKeyReadIsUnchanged`.

What does not change for an unscoped caller is the statement text of every
NornicDB story builder and of both call-chain builders — no grant array renders
— pinned by `TestRelationshipStoryBuildersCarryNoGrantForAnUnscopedCaller`,
`TestShortestPathCallChainBuildersBindTheGrant/unscoped_carries_no_grant`, and
the live `TestLiveNornicDBRelationshipStoryFullProjectionUnscopedIsUnchanged`,
which runs the shipped unscoped statement against the backend and gets all three
seeded rows back.

An earlier draft of this paragraph said no builder's row count for an unscoped
caller changes. That was false in two places — items 1 and 2 above — and none of
the tests it cited could have detected either. It is recorded here rather than
quietly corrected, because the reason it was wrong is the reason this batch
exists: a predicate whose text is present can still decide nothing, and a test
that never exercises the predicate cannot tell you which.

No-Observability-Change: no metric instrument, metric label, span, log event,
queue stage, worker knob, or schema phase changes. The existing query-route
spans and HTTP request metrics continue to expose both routes; the refusal paths
answer through the routes' own success shapes and are visible as ordinary 200s
with empty results, exactly as the batch-1 and batch-2a refusals are.
