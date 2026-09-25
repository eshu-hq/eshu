# #5167: POST /api/v0/code/relationships leaves the pending row-filtering ledger

`POST /api/v0/code/relationships` is where the `analyze_code_relationships`
MCP tool sends its `who_modifies`, `module_deps`, `variable_scope`,
`find_complexity`, `find_functions_by_argument` and
`find_functions_by_decorator` query types. It was the last code-family route on
the #5167 pending ledger. Scoped tokens got a 403 from the policy layer. The
handler bound no grant on most of its read paths, so allowlisting it as-is
would have become a cross-tenant read.

This change binds the grant on every read path the handler has, then makes the
five ledger promotion moves: the matcher, the advertised-route entry, the
OpenAPI marker (the `403` was already declared), and the removal from
`pendingRowFilteringRoutes`.

## Read paths and where the grant binds

| Path | Backend | Before | Now |
| --- | --- | --- | --- |
| Anchor metadata (`relationships.MetadataCypher`) | NornicDB | bound on the Repository alias (batch 2b) | unchanged |
| One-hop outgoing/incoming (`relationships.OneHopRelationshipsCypher`) | NornicDB | no grant | neighbour `repo_id` in the anchoring MATCH's WHERE, before ORDER BY/LIMIT |
| Far-file / far-repo enrichment (`relationships/enrich.go`) | NornicDB | no grant | same WHERE on `enrichNode.repo_id` |
| Transitive walk (`nornicDBTransitiveOneHopRows`) | NornicDB | no grant | neighbour `repo_id` in each hop's WHERE, so the BFS never steps onto an ungranted node |
| One-statement read (`codemodel.RelationshipGraphRowCypherFromAnchor`) | Neo4j | no grant, anchor included | anchor in a required `WITH e WHERE`; each neighbour in its own OPTIONAL MATCH WHERE |
| Transitive traversal (`codemodel.BuildTransitiveRelationshipRowsCypher`) | Neo4j | no grant | `all(node IN nodes(path) WHERE <grant on node.repo_id>)` |
| Content fallback entity (`relationshipsFromContent`) | Postgres | no check | the resolved entity's `repo_id` must be granted, otherwise the unknown-entity 404 |
| Content fallback name without `repo_id` (`resolveRelationshipEntity`) | Postgres | corpus-wide `SearchEntitiesByNameAnyRepo` | `relationshipNameMatchesInGrant`: each granted repository in turn |
| Content fallback neighbours (`BuildContentRelationships`) | Postgres | `repo_id = $1` on the anchor's repository | unchanged; the anchor check above closes it |
| Empty grant | both | reads ran | unknown-entity 404, no backend read |

The content-store neighbour SQL did not change. Every neighbour read in
`content_relationships*.go` (`SearchEntitiesByName`,
`SearchEntitiesReferencingComponent`, `ListRepoEntitiesByType`, `ListRepoFiles`)
is `WHERE repo_id = $1` bound to the anchor entity's own repository. Refusing an
ungranted anchor therefore closes the fallback's neighbour set. No new SQL was
written, so no `EXPLAIN ANALYZE` applies.

The ambiguity listing (`codemodel.ResolveRelationshipsNameTarget`) could not
leak. `resolveExactGraphEntityCandidates` returns nothing without a `repo_id`,
and `applyRepositorySelectorForCapability` has already grant-checked the
`repo_id`. The name leak was the content fallback's corpus-wide read, and
`TestCodeRelationshipsUngrantedNameIsIndistinguishableFromUnknown` pins it.

## Proof

Two-tenant fixture: `grant_clause_attachment_live_seed_test.go` plus
`relationships_grant_live_seed_test.go`. It adds a granted and an ungranted
neighbour in each direction for REFERENCES, IMPORTS, INHERITS, OVERRIDES and
USES_METACLASS. It also adds a hub with 1200 ungranted neighbours whose uids sort
ahead of its 40 granted neighbours. The caller is granted `repo://tenant-a/granted-service`.

Backends:

- NornicDB: `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`, compose env, container `nornic-5167rel`, bolt 17995.
- Neo4j: `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`, container `neo4j-5167rel`, bolt 17996.

No schema was applied, so no indexes or constraints were present. Both backends
were fresh containers.

```bash
cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:17995 ESHU_LIVE_GRAPH_BACKEND=nornicdb \
  go test ./internal/query/codequery -tags live_code_relationships_grant \
  -run TestLiveCodeRelationshipsGrant -count=1 -v
```

### RED (route through `CodeHandler.Mount`, scoped caller, origin/main 2ae147cf9)

| Case | NornicDB | Neo4j |
| --- | --- | --- |
| outgoing CALLS | returned `LiveClauseUngrantedCallee`, `LiveClauseOrphanCallee` | same |
| incoming CALLS | returned `LiveClauseUngrantedCaller` | same |
| all types, both directions | same leaks | same |
| REFERENCES/IMPORTS/INHERITS/OVERRIDES/USES_METACLASS × out/in | every ungranted neighbour returned (10/10) | 10/10 |
| hub, outgoing CALLS | 500 rows, 500 ungranted, 0 of 40 granted | 1240 rows, 1200 ungranted |
| transitive out from chain start | `LiveClauseChainBridge` (ungranted), `LiveClauseChainEnd` (reached through it) | same |
| transitive in from chain end | bridge and `LiveClauseChainStart` | same |
| ungranted entity_id | 200 from the content fallback | 200 from the graph itself: the anchor was unbound |

Hermetic RED (`auth_scoped_code_relationships_grant_test.go`, both backends):

- The ungranted `entity_id` and the ungranted repo-less `name` both returned 200
  with the other tenant's entity.
- The empty grant returned 200 and issued 1–2 graph reads.
- A name held by both tenants returned 404 instead of the granted copy.

### GREEN

On both backends every leak case returns only the granted neighbour, and the
granted neighbour is always present. An empty result cannot pass: the pinned
NornicDB silently ignores a predicate it cannot parse.

- The hub returns exactly the 40 granted rows, each with
  `target_repo_id = repo://tenant-a/granted-service`.
- Both transitive walks return nothing.
- The clean all-granted chain still returns its mid and end nodes.
- An ungranted anchor returns a 404 byte-identical to an unknown one.
- The shared-key subtest still returns every tenant's neighbours and the full
  transitive chain.

Mutation check: with the enrichment bind removed, the NornicDB hub case fails
with 40 errors of the form `target_repo_id = "", want the granted repository`.
The shared LIMIT is spent on ungranted neighbours and the granted rows lose
their metadata. That is why the enrichment reads bind the grant even though the
Go merge would never attach an ungranted row.

The real-middleware round trip is
`TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware`, which
iterates `scopedTokenAdvertisedRoutes` and now includes this route.

Performance Evidence: route-level medians of 25 warm runs through
`CodeHandler.Mount` (`TestLiveCodeRelationshipsGrantTiming`) on the fixture
above, on a shared host with load average 67–89. "Before" is the unscoped
request, whose statements are byte-identical to the ones every caller ran before
this change, because grant text renders only for a scoped caller. "After" is the
scoped request.

| Backend | Anchor | Before median | After median |
| --- | --- | --- | --- |
| NornicDB | function, all types, both directions | 23.6 / 23.1 / 39.7 ms | 26.7 / 23.6 / 38.9 ms |
| NornicDB | hub, outgoing CALLS | 34.8 / 41.3 / 45.0 ms | 24.5 / 29.4 / 23.8 ms |
| Neo4j | function, all types, both directions | 13.0 ms | 12.2 ms |
| Neo4j | hub, outgoing CALLS | 68.2 ms | 13.8 ms |

Three NornicDB runs show no measurable change on the ordinary anchor. The deltas
are +3.1, +0.5 and −0.8 ms, inside run-to-run noise. The hub gets faster because
the bound read returns 40 rows instead of 500 (NornicDB) or 1240 (Neo4j). For a
scoped caller the row ceiling (`RowLimit` 500, fetch 501) now counts granted rows
only. The anchor keeps its `{uid: $entity_id}` label-property seek on both
backends, and the added predicate evaluates only on the anchor's
already-expanded neighbours.

No-Observability-Change: no metric instrument, metric label, span, log event,
queue stage, worker knob or schema phase changes. The route's existing HTTP
request metrics and query spans still cover it. A refused anchor or an empty
grant is visible as the route's ordinary 404, deliberately indistinguishable
from an unknown entity so the index cannot be probed. An ungranted `repo_id`
selector is the existing 400.

## Out of scope, noted

- The MCP fallback sends only `{entity_id, query_type}`, and
  `RelationshipsRequest` has no `query_type`. All six fallback query types
  therefore return every relationship of the entity. That semantics is unchanged
  here.
- The handler's response map is built field by field and drops the
  `outgoing_truncated`/`incoming_truncated` flags that `relationships.GraphRow`
  sets. The OpenAPI schema advertises them. The live hub run (500 rows before
  the fix) returned no flag. This predates the change and is not a grant defect.
