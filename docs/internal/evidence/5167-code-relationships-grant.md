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
| Content fallback entity by id (`resolveRelationshipEntity`) | Postgres | `GetEntityContent` (`WHERE entity_id = $1`) | `GetEntityContentInRepositories` (`AND repo_id = ANY(...)`, PK seek) through `relationshipEntityContentForAccess`, plus a repository recheck in `relationshipsFromContent` |
| NornicDB label lookup (`nornicDBRelationshipEntityLabel`) | Postgres | unbound `GetEntityContent` (leaked only a label) | same grant-bound read |
| Content fallback name without `repo_id` (`resolveRelationshipEntity`) | Postgres | corpus-wide `SearchEntitiesByNameAnyRepo` | new `ContentReader.SearchEntitiesByNameInRepositories`: one statement, `repo_id = ANY($2)` in the same WHERE, same LIMIT 2 |
| Name-target ambiguity list (`codemodel.ResolveRelationshipsNameTarget`) | Postgres | only reachable with a grant-checked `repo_id` | unchanged, plus a defence-in-depth candidate filter on the grant |
| Content fallback neighbours (`BuildContentRelationships`) | Postgres | `repo_id = $1` on the anchor's repository | unchanged; the anchor check above closes it |
| Empty grant | both | reads ran | unknown-entity 404, no backend read |

The content-store neighbour SQL did not change, and nothing loops per granted
repository: the name search is one statement for the whole grant, so the
fallback's "exactly one match" rule holds across the grant (two granted copies
stay ambiguous; one granted plus one ungranted resolves to the granted copy). Every neighbour read in
`content_relationships*.go` (`SearchEntitiesByName`,
`SearchEntitiesReferencingComponent`, `ListRepoEntitiesByType`, `ListRepoFiles`)
is `WHERE repo_id = $1` bound to the anchor entity's own repository. Refusing an
ungranted anchor therefore closes the fallback's neighbour set; the hermetic
`relGrantContentBuilder` double records that it never runs for one.

### The new name search, measured

Postgres 18 (`postgres:18-alpine`, throwaway container `pg-5167rel`), a
`content_entities` table with the production indexes that matter here
(`repo_idx`, `(repo_id, entity_id)`, `name_trgm` GIN, `path_idx`, `type_idx`),
200,002 rows over 1000 repositories; `HandleRequest` is a common name (4000
rows), `RareUniqueSymbolXyz` a rare one (2 rows, repos `r_1` and `r_777`).
`EXPLAIN (ANALYZE)`, three executions each, median shown.

The shape is `repo_id = ANY($2)` with a `text[]` parameter (the
`appendRepositoryGrantFilter` form), not the `string_to_array` form
`GetEntityContentInRepositories` uses. The two plan identically under a custom
plan, but pgx caches named statements, and PostgreSQL's `plan_cache_mode = auto`
DID switch this statement to a generic plan after five executions of a mixed
workload on this data. Under the generic plan `string_to_array` is re-evaluated
on every heap recheck:

| Name, grant | before: `...AnyRepo`, custom | after, custom | after, generic: `ANY($2)` | generic: `string_to_array` | generic: `IN (SELECT unnest($2))` |
| --- | ---: | ---: | ---: | ---: | ---: |
| rare, 2 repos | 0.29 ms | 0.19 ms | 0.14 ms | 0.20 ms | 0.22 ms |
| rare, 128 | — | 0.13 ms | 0.54 ms | 0.53 ms | 11.1 ms |
| rare, 1000 (whole corpus) | — | 0.21 ms | 3.54 ms | 3.53 ms | 87.0 ms |
| common, 2 | 1.03 ms | 0.64 ms | 0.76 ms | 0.82 ms | 1.21 ms |
| common, 128 | — | 9.50 ms | 1.74 ms | 3.25 ms | 74.5 ms |
| common, 1000 | — | 0.67 ms | 30.95 ms | 169.05 ms | 603.9 ms |

The old corpus-wide read has a generic-plan worst case of its own: 128–131 ms
for the rare name (it walks `repo_idx` in `ORDER BY` order and filters). So the
grant-bound read's worst case, ~31 ms for a common name under a
whole-corpus grant with a generic plan, is below the read it replaces. The
custom-plan outlier (common name, 128-repo grant, ~9.5 ms) is the planner
walking `repo_idx` in `ORDER BY repo_id` order across 128 repositories; the
path only runs when the graph returned no row for a name. Row membership,
checked directly: grant `{r_1, r_2}` returns only `entity:rare-a`; `{r_1, r_777}`
returns both (ambiguous, so the fallback resolves nothing); `{r_5}` returns 0.
`TestContentReaderSearchEntitiesByNameInRepositoriesBindsTheGrant` pins one
statement with the grant ahead of `LIMIT`, and the empty-grant test pins that
an empty set reads nothing.

### Backend split for transitive CALLS

- NornicDB answers transitive CALLS only through the per-hop Go breadth-first
  walk (`nornicDBTransitiveOneHopRows`), with the grant in each hop's WHERE. The
  variable-length statement is not safe there: the probe measured that an
  endpoint-only bind leaks interior hops and that
  `all(node IN nodes(path) WHERE ...)` filters nothing on the pinned build.
- Neo4j answers it with `codemodel.BuildTransitiveRelationshipRowsCypher`,
  bound with `all(node IN nodes(path) WHERE <grant on node.repo_id>)`, which
  the live Neo4j run shows excluding both the bridged chain and its far end
  while admitting the all-granted chain.
- `TestCodeRelationshipsTransitiveDispatchIsPinnedPerBackend` fails if NornicDB
  is ever routed through the variable-length statement (mutation-checked:
  forcing it reds the test), or if the Neo4j statement loses its path-wide
  grant.

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
| name, no repo_id, only in repo-b | 404 (metadata read already bound) | 200 with repo-b's entity (the `MATCH (e) WHERE e.name` scan was unbound) |
| name held once per tenant, no repo_id | resolves the granted copy (already bound) | 404: both copies counted, so ambiguous |

Hermetic RED (`auth_scoped_code_relationships_grant_test.go`, both backends):

- The ungranted `entity_id` and the ungranted repo-less `name` both returned 200
  with the other tenant's entity.
- The empty grant returned 200 and issued 1–2 graph reads.
- A name held by both tenants returned 404 instead of the granted copy.
- Added after review: without a relationship builder an ungranted id answered
  503 where an unknown id answered 404, an existence oracle. The grant-bound
  entity read now returns nothing for it, so both answer 404
  (`TestCodeRelationshipsScopedEntityReadIsGrantBound`, which also pins that a
  scoped caller never issues the unbound `WHERE entity_id = $1` read;
  mutation-checked by reverting the label lookup).

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
