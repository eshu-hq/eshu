# #6782 slice 1: B-7 golden corpus on Neo4j, two divergences

The first B-7 golden-corpus run on Neo4j (origin/main, `ESHU_GRAPH_BACKEND=neo4j`)
finished with 558 passing shapes and 2 required failures in 213s. The NornicDB
run takes about 180s. This note records the diagnosis of each failure, which
backend matched the documented contract, the fix, and the proof.

Source: base `3b331d24a` (origin/main) and the #6782 slice-1 branch working
tree. Backends used for every probe and live test below:
`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`
(async writes, embeddings, search, and Heimdall disabled) and
`neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
(`NEO4J_AUTH=none`). Eshu's schema was applied with
`graph.EnsureSchemaWithBackend` before each live test and before the
benchmark. The same local host was used throughout, with fresh containers.

## 1. `mcp:find_function_call_chain` returned HTTP 500 on Neo4j

Root-Cause Evidence: the gate log carried
`Neo.DatabaseError.Statement.ExecutionFailed (The shortest path algorithm does
not work when the start and end nodes are the same ...)`. The snapshot shape
asks for `start = end = recursionFib`, a self-recursive call. A probe on a
self-loop reproduced it with the same error from `shortestPath((s)-[:CALLS*1..2]->(e))`
when `s = e`.

Contract: the NornicDB route (`nornicDBCallChainRows`, a Go-side breadth-first
walk) returns the cycle `[recursionFib, recursionFib]` at depth 1. That is the
answer the snapshot asserts. Neo4j was the backend at fault.

Rejected designs, all measured on the Neo4j image:

- **Guard `shortestPath()` with `start <> end`, and send `start = end` rows to
  `SHORTEST 1` in a `UNION` branch.** Neo4j rejects the statement: it cannot mix
  `shortestPath` with path selectors (`Neo.ClientError.Statement.SyntaxError`).
- **`SHORTEST 1` with the existing trailing `WHERE all(node IN nodes(path) ...)`.**
  The predicate filters the path already chosen instead of steering the search.
  On a graph where the shortest cycle crosses another repository and a longer
  in-repository cycle exists, it returned 0 rows. Legacy `shortestPath()` does
  apply the trailing predicate during the search. This bound is the grant and
  repository hop bound, so post-filtering would drop valid in-grant answers.
- **`SHORTEST 1` inline, after the anchoring `MATCH` clauses.** Both anchors'
  `NodeIndexSeek(2)` became `NodeIndexScan(20001)`. Adding a `WITH start, end`
  barrier, inline property maps, or `ANY SHORTEST` did not restore the seek.

Fix: `chain.BuildCallChainCypher` (the Neo4j route) runs
`CALL (start, end) { MATCH path = SHORTEST 1 (start)(()-[:CALLS]->(node) WHERE <hop bounds>){1,N}(end) RETURN path }`.
The hop bounds are the same `PathHopPredicates` conjuncts as before: the request
repository bound plus the caller grant. They now sit inside the quantified
pattern. The start node is held by the anchoring `WHERE`. The NornicDB route is
unchanged. The statement needs Neo4j 5.23 or later.

Performance Evidence: exact old vs new builder text (dumped from
`BuildCallChainCypher` at both trees) on neo4j:2026-community with Eshu's Neo4j
schema. The graph was 20,000 `:Function` nodes (`name`, `id`, `uid`,
`repo_id`; 10% in repo:b) with 4 deterministic `CALLS` per node (80,000 edges).
There were 8 endpoint pairs per variant. The timings are the median of 7 runs
after one warm run, and db hits come from `PROFILE`. The anchor operators are
identical in old and new (index seeks on `function_name`); the search operator
is `ShortestPath` in both. Every pair returned the same path length old vs new.

| Variant | Old db hits (range) | New db hits (range) | Old / new median ms |
| --- | --- | --- | --- |
| repo-scoped, depth 5 | 252-343 | 276-341 | 0.98-1.32 / 1.01-1.37 |
| repo-scoped, depth 10 | 644-6031 | 642-6568 (+0% to +12.3%) | 1.19-3.00 / 1.48-4.56 |
| unscoped, depth 5 | 248 | 248 | 0.69-1.41 / 0.66-1.03 |
| unscoped, depth 10 | 471-6152 | 471-6152 (identical) | 1.02-2.59 / 1.06-1.94 |
| self pair `f7->f7` | error (HTTP 500 path) | 248-967 | error / 0.92-1.72 |

The repo-scoped depth-10 cost rises by up to 12.3% in db hits (5213 to 5853 on
the worst pair). That is the price of the search applying the hop bound itself,
and latency stays in the 1-5 ms band. The request is bounded by `max_depth`
(clamped to 10 by the route) and `LIMIT 5`. This is a correctness fix for a
request that currently fails outright, and it is accepted with that measured
cost. A warm single-request measurement cannot show cold-client latency or
behavior under a full production graph; neither is claimed here.

Proof, RED/GREEN:

- Unit: `go test ./internal/query/codequery/chain -run TestBuildCallChainCypherNeo4jHandlesSelfRecursion`
  failed on the old builder and passes now.
- Live: `TestLiveCallChainSelfRecursion` (tag `live_nornicdb_answer_truth`) drives
  `POST /api/v0/code/call-chain` through the real route. On the old builder,
  Neo4j failed all 3 self cases with HTTP 500 and passed the plain chain;
  NornicDB passed all 4. On the new builder, both backends pass all 4 cases:
  the self call, the in-repository self cycle (length 3, not the shorter
  cross-repository cycle), the unbounded self cycle (length 2), and a plain
  chain.

## 2. `mcp:get_repo_context` lacked `source_tool_breakdown` on Neo4j

Root-Cause Evidence: a probe of
`UNWIND $rows AS row ... MERGE (a)-[rel:X]->(b) SET rel.source_tool = row.source_tool`
with the `source_tool` key absent from the row map stored the literal string
`"row.source_tool"` on NornicDB (`IS NOT NULL` true). Neo4j left no property.
With an explicit `nil` value, `IS NOT NULL` is false on both backends.
`EdgeWriter.buildRowMap` added `source_tool` and `evidence_type` only when the
intent payload had a value. The package-consumption and code-import writers of
orders-api's only verb-typed outgoing edge (`DEPENDS_ON` to lib-common) set no
`source_tool`. Edge counts matched between the two backend runs (`DEPENDS_ON`
5 and 5), so the edge existed on both; only its property differed.

Contract: `docs/public/reference/http-api/context-and-stories.md` says
`source_tool_breakdown` is "Omitted when no edges carry `source_tool`", and
`edge-source-tool-provenance.md` leaves an edge with no evidence kind
unstamped. Neo4j matched the contract. NornicDB's breakdown was reporting the
bogus token.

Fix: `setOptionalRowString` always writes the key, with `nil` when absent, for
`evidence_type` and `source_tool` on all three repo-dependency routes
(DEPENDS_ON, the typed verbs, and RUNS_ON).

Proof, RED/GREEN:

- Unit: `TestEdgeWriterRepoDependencyRowsCarryEveryReferencedKey` checks that
  every `row.<key>` the routed statement reads is present in each row.
  `TestEdgeWriterRepoDependencyAbsentSourceToolIsNil` pins `nil`, not `""`.
  Both failed before the fix.
- Live: `TestLiveRepoDependencyWithoutSourceToolStaysUnstamped` drives the
  production `EdgeWriter` and `repository.QueryRepoSourceToolBreakdown`. Before
  the fix it failed on NornicDB (`source_tool="row.source_tool" stamped=true`)
  and passed on Neo4j. After the fix it passes on both, with an unstamped edge
  and a breakdown of exactly `[{helm 1}]`.

Consequence for the snapshot: with the fix, orders-api has no stamped outgoing
edge on either backend, so the documented response omits
`source_tool_breakdown`. The snapshot's required field for that shape was
calibrated against the NornicDB junk token. The owner approved retargeting the
assertion to a repository where the field is true.

Snapshot change: the orders-api `get_repo_context` shape keeps every other
field, including its repository id and name pins, its `relationships` bounds,
`language_breakdown` and `relationship_overview`. It stops requiring
`source_tool_breakdown`. A second shape,
`get_repo_context?assert=source_tool_breakdown`, calls the same tool for
`helm-umbrella-chart`. Its Chart.yaml has one subchart dependency sourced from
`github.com/acme/deployable-source`, which is the HELM_CHART_REFERENCE
DEPLOYS_FROM edge that `rc-34` already requires with `source_tool = helm`.
That is the repository's only outgoing dependency, so the shape requires
`source_tool_breakdown` and pins `source_tool_breakdown.helm = 1`. The gate has
routed `?assert=<slug>` MCP keys to the bare tool name since `mcpToolName`
added them.

Adding a shape keeps more coverage than retargeting the existing one. Moving
the orders-api selector would have dropped its id pin and the
`language_breakdown` check, and it would not have pinned the breakdown's value.
The new shape proves the field is present and correct, not merely present. A
junk `row.source_tool` key or a wrong count now fails it.
`TestGoldenSnapshotAssertsSourceToolBreakdownOnTruth` (in
`go/cmd/golden-corpus-gate`) pins the change. It failed against the previous
snapshot and passes now, and it checks the shape against a stamped payload
(pass), an omitted breakdown, a junk-token breakdown, and a wrong count (each
fails). The live B-7 run is the end-to-end proof.

No-Regression Evidence: the writer change adds one map key per row (a `nil`
value) on the repo-dependency routes and changes no Cypher text or batch shape.
The call-chain measurement is above.

No-Observability-Change: neither fix adds or changes a metric, span, or log
field. The call-chain route keeps its existing graph-read error path
(`WriteGraphReadError`), and the edge writer keeps its existing
`logSharedEdgeWrite` and grouped-write telemetry.

## 3. Differences that pass on both backends (review finding F-7)

After the fixes above, both B-7 cells pass, but the element total that
`unresolved_row_tokens` reports differs: 2770 on NornicDB and 2772 on Neo4j
(`b7-batch-6782-nornic.log` and `b7-batch-6782-neo4j.log`, both at
`f9b6b189d`). Diffing every finding line of the two logs, and leaving out
timings, finds exactly three differences. All 66 asserted node and edge counts
are identical on both backends, so the element gap comes from labels and types
the snapshot does not count. At the r4 runs (`b7-6782-s1-r4-nornic.log` and
`b7-6782-s1-r4-neo4j.log`, both at `8068a0a27`) the totals are 2795 on NornicDB
and 2798 on Neo4j: the 3-element Neo4j surplus reproduces.

| Finding line | NornicDB | Neo4j |
| --- | --- | --- |
| `POST /api/v0/code/relationships?assert=transitive-callees` (`outgoing`) | 1 result | 2 results |
| `POST /api/v0/code/relationships?assert=transitive-callers` (`incoming`) | 1 result | 2 results |
| `rc-12` `(Class)-[:INHERITS]->(Class)` | 23 | 22 |
| `unresolved_row_tokens` element total | 2770 | 2772 |

### 3a. Transitive `CALLS` relationships: a route divergence, filed here

Reproducible: both earlier run pairs at this branch (`b7-6782-*.log` and
`b7-batch-6782-*.log`) show 1 against 2 for both shapes.

Root-Cause Evidence: `transitiveRelationshipsGraphRow`
(`go/internal/query/codequery/relationship_handlers.go`) runs two different
algorithms. On NornicDB, `relationships.TransitiveRows` walks the graph
breadth-first. It seeds `seen` with the start entity, so each node appears
once, at its shortest depth, and the start node never appears. On Neo4j,
`codemodel.BuildTransitiveRelationshipRowsCypher` returns every
`(e)-[:CALLS*1..N]->(x)` path. `BuildTransitiveRelationshipGraphResponse`
dedupes on `(id, name, depth)`, so the same node can appear at more than one
depth, and on a cycle the start node appears as its own callee.

Live probe: the pinned images above, with fresh containers on ports 28100
(NornicDB) and 28110 (Neo4j) and Eshu's schema applied. The seed was the
fixture's shape, `mutualPing -[:CALLS]-> mutualPong -[:CALLS]-> mutualPing`,
queried from `mutualPing` with `max_depth` 4 through the production functions.

| Route, run on | outgoing | incoming |
| --- | --- | --- |
| NornicDB BFS, on Neo4j | `mutualPong`@1 | `mutualPong`@1 |
| Neo4j Cypher, on Neo4j | `mutualPong`@1, `mutualPing`@2 | `mutualPong`@1, `mutualPing`@2 |
| NornicDB BFS, on NornicDB | `mutualPong`@1 | `mutualPong`@1 |
| Neo4j Cypher, on NornicDB | none | none |

On one Neo4j graph the two routes disagree, so the backend's storage is not
the cause; the routes implement different semantics. (The last row shows why
NornicDB has a route of its own: the variable-length pattern returns no rows
there.) The documented contract in `docs/public/reference/http-api/code.md`
says only that `max_depth` caps the traversal. It does not say whether the
start node or a repeat visit belongs in the result. So the contract does not
decide which route is wrong, and changing either one changes a hot-path
read.

Disposition: this is a real divergence and is left open. The snapshot shape
allows 1 to 4 results and requires `mutualPong` at depth 1, which both routes
return, so B-7 is green on both. The fix needs the owner to choose the
semantics (the recommendation is the BFS rule: each reachable node once, at
its shortest depth, excluding the start). Then the non-selected route should
change, with a Cypher measurement, and the shape should pin an exact count.
Tracked in #6849, which belongs to the #6782 Tier-B response diff.

### 3b. `rc-12` INHERITS changes run to run on both backends

The Class `INHERITS` count is not deterministic on either backend, so this
is not a backend divergence. Every B-7 log at this branch and its siblings
reports `rc-12` as 22 or 23:

| Backend | 22 | 23 |
| --- | --- | --- |
| Neo4j | `b7-6782-neo4j`, `b7-batch-6782-neo4j` | `b7-batch2-6782-neo4j` |
| NornicDB | `b7-6782-nornic`, `b7-6785-ledger`, `b7-batch-6785`, `b7-batch-6786pub`, `b7-labels-rebased`, `b7-batch2-6782-nornic`, `b7-batch2-labels` | `b7-batch-6782-nornic`, `b7-6785a-rebased`, `b7-batch2-6785`, `b7-batch2-6786pub` |

The same-branch pairs show the flip directly. At `f9b6b189d`
(`b7-batch-6782-*`) NornicDB reported 23 and Neo4j 22. At `be5cc1d5b`
(`b7-batch2-6782-*`) NornicDB reported 22 and Neo4j 23.

Disproven theory: that NornicDB's relationship `MERGE` in
`batchCanonicalInheritanceEdgeUpsertCypher` creates a second edge. On both
pinned images with the schema applied, the production template left exactly
one `INHERITS` edge per pair in each of three probes: two identical rows in one
`UNWIND` batch; three sequential runs; and 8 concurrent writers on each of 18
pairs. The Neo4j 23 in batch2 is consistent with that: the variance does not
depend on the backend.

Disposition: open, tracked in #6850. The extra edge most likely comes from
Eshu's pipeline upstream of the graph write, not from either backend. The
next theories are a different resolved parent (parent-resolution order) or an
ordering race between the inheritance retract and upsert. Telling them apart
needs a projected corpus graph dumped from a 23-edge run on either backend and
diffed against a 22-edge run. `rc-12` is a floor (`>= 1`), so the flip does not
fail B-7. It must be root-caused before `rc-12` or an INHERITS count gets an
exact bound.

### 3c. The element total

In both same-branch run pairs, Neo4j carries 3 more elements than NornicDB
once the INHERITS edge from 3b is netted out. At `f9b6b189d` the totals were
2770 (NornicDB, 23 INHERITS) and 2772 (Neo4j, 22). At `be5cc1d5b` they were
2770 (NornicDB, 22) and 2774 (Neo4j, 23). The logs do not break the total down
by label, and the snapshot asserts no per-label total for these elements, so
the 3 elements are not yet identified. They are left for the slice-2
graph-state tier, which diffs per-label and per-type totals between backends.
No assertion pins the element total: it is a corpus-size readout, not a
contract.

## Open follow-ups

- The missing-row-key shape is recorded in
  `docs/public/reference/nornicdb-write-shape-pitfalls.md`. The audit of every
  other production `UNWIND` writer, with four more fixes and a reusable guard,
  is in [6782-unwind-missing-row-key-audit.md](6782-unwind-missing-row-key-audit.md).
- Section 3 lists the differences that still pass on both backends: the
  transitive `CALLS` semantics (3a, #6849), the INHERITS count that varies
  on both backends (3b, #6850), and the unidentified element gap (3c).
- The two call-chain routes return different numbers of chains. NornicDB's
  breadth-first walk returns up to 5 chains across depths, while Neo4j returns
  one shortest path per endpoint pair. This is pre-existing, and a candidate
  for the #6782 Tier-B response diff.
