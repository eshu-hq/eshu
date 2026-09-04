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
`LIMIT`. That is a correctness and cost defect for every caller class, not a
tenancy one, and it does not depend on NornicDB — `OPTIONAL MATCH … WHERE` has
the same semantics on Neo4j, which is the lane that builder ships to.

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
| call-chain shortestPath, both builders | anchoring `WHERE`, on `start.repo_id` and `end.repo_id` | `buildCallChainCypher`, `buildNornicDBCallChainCypher` |
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

## The Path-Wide Bound That Cannot Be Written

Bounding only the two endpoints of the inheritance walk leaves a gap in
principle: an intermediate ancestor in another repository could still join two
in-grant classes. The obvious closure is
`all(node IN nodes(path) WHERE node.repo_id IN $ids)`, and it does not filter on
the pinned build. Neither does `none(...)/NOT IN`, nor a `size([...]) = 0`
comprehension, nor an inline literal list; and one scalar equality per allowed
value, OR-ed, fails the other way and drops rows that should be admitted. Only a
single scalar equality inside `all(...)` is evaluated. The full table is in
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md)
under the list-membership entry this batch added, pinned as measured values by
`TestLiveNornicDBPathListPredicateBehaviour`.

Call-chain does not need it: its NornicDB response path is a Go-side
breadth-first search over the one-hop read, so bounding each hop bounds the
whole chain, and `TestCallChainBoundsEveryFrontierHop` proves an out-of-grant
intermediate cannot join two granted endpoints. The inheritance walk keeps the
endpoint bound and says why at the call site.

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

Three cypher digests and eleven source digests move, in two commits separate
from the semantic ones.

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

- A story ambiguity list that used to name every tenant's match now names only
  readable ones, so a request that answered `ambiguous` may now resolve.
- A call chain that exists only by passing through an ungranted repository is
  absent, not returned with the hop hidden.
- An ungranted repository selector on either route returns `400`.
- A token with no repository grants gets `"status": "not_found"` (story) or
  `"chains": []` (call-chain), with no backend read — the same answer as a
  target that does not exist.
- `repo_id` on the story route now filters. It was inert in the same clause
  position for every caller class, scoped or not.

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
`IN`/`=` membership test against the caller's grant, on a node the query already
matched, and every one of them moves EARLIER in the statement — from a trailing
`WHERE` into the anchoring `MATCH`'s own — so it is applied before `SKIP`/`LIMIT`
rather than after the traversal. A scoped caller reads no more rows than before
and on these routes strictly fewer: both were corpus-wide for a caller who named
no repository. The one shape that gets faster rather than merely narrower is the
Neo4j-compat story builder, which was scanning every `CALLS` edge in the graph
because its anchor predicate was in the inert position; no number is claimed for
that, only the row-set difference measured live (5 rows to 1 on the seeded
graph). No statement gains a clause, a hop, or a second round trip, and no
builder's row count for an unscoped caller changes — pinned by
`TestRelationshipStorySharedKeyReadIsUnchanged`,
`TestCallChainSharedKeyReadIsUnchanged`,
`TestRelationshipStoryBuildersCarryNoGrantForAnUnscopedCaller` and
`TestLiveNornicDBRelationshipStoryFullProjectionUnscopedIsUnchanged`.

No-Observability-Change: no metric instrument, metric label, span, log event,
queue stage, worker knob, or schema phase changes. The existing query-route
spans and HTTP request metrics continue to expose both routes; the refusal paths
answer through the routes' own success shapes and are visible as ordinary 200s
with empty results, exactly as the batch-1 and batch-2a refusals are.
