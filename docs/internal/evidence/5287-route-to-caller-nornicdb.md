# #5287 — route-to-caller code reads NornicDB-safe

`go/internal/query/code_route_to_caller_graph.go`. Three multi-clause reads
corrupt on the pinned NornicDB backend (`eshu-nornicdb-pr261`, v1.1.11 base).
Neither the file nor the handler branches on backend, so the unsafe shapes run
directly on NornicDB.

Classification: **Correctness win** (behavior fix — the old reads returned
corrupt/empty graph truth). No hot-path regression: the split reads are
single-clause, label-anchored, and per-request bounded (limit + depth capped by
the route-to-caller request normalizer).

## Before (live, Bolt driver, DB nornic)

Seed: `(:Workload)-[:EXPOSES_ENDPOINT]->(:Endpoint)<-[:HANDLES_ROUTE]-(:Function
handler)`, `(:Function caller)-[:CALLS]->(handler)-[:CALLS]->(:Function callee)`,
`(handler)-[:RUNS_IN]->(:Workload)`, `(:Repository)-[:EXPOSES_ENDPOINT]->(endpoint)`.

| read | old shape | old result |
| --- | --- | --- |
| `routeToCallerRouteRows` | endpoint MATCH + OPTIONAL MATCH handler → computed RETURN | handler columns = literal text `"handler.id"` etc. |
| `routeToCallerRelationshipRows` | `CALL { … UNION ALL … RETURN caller as entity }` + computed outer RETURN | entity columns = literal text `"entity.id"`; direction/depth null |
| `routeToCallerImpact` | 4× OPTIONAL MATCH → `collect(DISTINCT {…})[0..$limit]` | lists containing the literal string `"DISTINCT {id: …}"` |

Two NornicDB corruptors were isolated live (beyond the documented multi-clause
and CALL-aggregation pitfalls):

1. **A variable-length path START must carry a label.**
   `(handler)-[:CALLS*1..N]->(x)` with an unlabeled anchored end returns zero
   rows; `(handler:Function)-[:CALLS*1..N]->(x)` returns the paths. (A
   both-endpoints-bound path — `buildNornicDBCallChainCypher` — works because the
   endpoints are pre-bound; a fresh-variable far end needs the anchored end
   labelled.)
2. **Node-identity inequality `a <> b` between whole nodes returns zero rows.**
   Replace with a property inequality
   `coalesce(a.id, a.uid) <> coalesce(b.id, b.uid)`.

## Fix

- **`routeToCallerRouteRows` (Q3)**: split into a single-clause endpoint read
  (`MATCH (endpoint:Endpoint)` or `(workload:Workload)-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)`
  when a service filter is set) and a single-clause handler read
  (`MATCH (handler)-[route:HANDLES_ROUTE]->(endpoint:Endpoint)`), left-joined in
  Go on the endpoint id so endpoints with no handler still surface (the prior
  OPTIONAL MATCH semantics). Framework = `firstNonEmpty(route_framework,
  endpoint_framework)`; ordering mirrors the prior
  `repo_id, path, http_method, handler_name, handler_id`.
- **`routeToCallerRelationshipRows` (Q4)**: resolve the handler's label first
  (`head(labels(handler))`, whitelisted against the code-entity labels via
  `codeEntityLabelAllowed`), then run one single-clause directional path per
  direction with the KNOWN handler as the labelled path start —
  `(handler:<Label>)<-[:CALLS*1..N]-(caller)` / `-[:CALLS*1..N]->(callee)` —
  projecting raw `nodes(path)`. The discovered caller/callee is the far endpoint,
  extracted in Go by `routeToCallerEntityFromChain` (driver-aware seam in
  neo4j.go, last node of the chain, both `neo4j.Node` and map shapes). Node
  inequality uses the id/uid property form.
- **`routeToCallerImpact` (Q5)**: split into three single-clause set reads —
  endpoint workloads (`(:Workload)-[:EXPOSES_ENDPOINT]->(:Endpoint)`), runtime
  workloads (`(handler)-[:RUNS_IN]->(:Workload)`), and repositories
  (`(:Repository)-[:EXPOSES_ENDPOINT]->(:Endpoint)`), returning scalar
  id/name/repo_id columns assembled into maps in Go and merged with the existing
  `mergeRouteToCallerMaps`. Scoped access is now a required-match `AND` predicate
  (fail-closed) rather than the prior OPTIONAL `IS NULL OR` escape.

## After (live)

`TestLiveRouteToCallerReadsAreNornicDBSafe` drives the shipped handler methods
and asserts: the route selects endpoint `rc5287:ep` + handler `rc5287:handler`
(GET, handleOrders, flask, app.py); callers = `[main/rc5287:caller]`, callees =
`[queryDB/rc5287:callee]` with full file/line detail; impact workloads include
the endpoint (`rc5287:wl`) and runtime (`rc5287:rtwl`) workloads and repositories
= `[rc5287:repo2]`.

## Verification

- Backend-required live: `TestLiveRouteToCallerReadsAreNornicDBSafe`
  (`ESHU_INFRA_AGG_PROVE_LIVE=1 ESHU_NEO4J_URI=bolt://localhost:17687`) — green;
  reverting any of the three rewrites reproduces the literal-text/empty columns.
- Unit guards: `TestCodeEntityLabelAllowedGatesInterpolatedLabel` (whitelist +
  injection rejection), `TestRouteToCallerEntityFromChainDecodesBothBackends`
  (neo4j.Node + NornicDB map), `TestRouteToCallerEntityFromChainPrefersUidAndRelativePath`.
- Handler mock tests (`code_route_to_caller_test.go`) reworked for the split-query
  dispatch (endpoint/handler/label-resolve/direction/impact) and green: exact
  handler + bounded callers, unsupported-without-HANDLES_ROUTE, ambiguous→409,
  service-scope workload edge, scoped path-node filter, out-of-grant→404,
  missing-route→404.

No-Regression Evidence: The reads change shape but stay bounded. Each split read
is single-clause and label-anchored (`Endpoint`, `HANDLES_ROUTE`-anchored handler,
labelled-start CALLS path, `Workload`/`Repository` set reads); none introduces an
unlabeled whole-graph start. The route/impact reads replace one combined query
with a small fixed number of anchored reads (2 for routes, 1 label-resolve + 2
directional for relationships, 3 for impact), each `LIMIT`-bounded, joined in Go
over the already-limited row sets — no new whole-graph scan and no change in
per-request cardinality class. Same pinned backend, correctness-only delta.

No-Observability-Change: No metrics, spans, logs, or status fields are added,
removed, or renamed; only the Cypher shapes and their Go decoding changed.
