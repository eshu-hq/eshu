# infra/resources/search Per-Label Anchoring Evidence

This note records the focused local proof for issue #5271's
`POST /api/v0/infra/resources/search` fix.

## Theory correction

#5271 opened on the theory that a NornicDB Cypher plan cache warms per query
shape and that prewarming it would close a reported 2-5000x cold/warm latency
gap across nine read routes. That theory is disproven: `QueryPlanCache.Get`/
`.Put` are never called in NornicDB v1.1.11's non-test `pkg/cypher` source
(confirmed via `git clone --depth 1 --branch v1.1.11` and
`rg '\.planCache\.(Get|Put)\('`), and the live pinned binary's `/metrics`
endpoint exposes no plan-cache series of any kind. The actual root cause for
`infra/resources/search`, confirmed by source inspection, is the same
unindexed-anchor defect class already fixed for other handlers under #5244
(PR #5276): `InfraHandler.searchResources` anchored on an unlabeled
`MATCH (n)` scan filtered by an `(n:A OR n:B OR ...)` predicate, which forces
a whole-graph scan on every call regardless of corpus size or how selective
the later `AND` conditions are. Full analysis recorded on the issue:
https://github.com/eshu-hq/eshu/issues/5271#issuecomment-4995698888

## Fix

`go/internal/query/infra.go`'s `searchResources` now builds one
`MATCH (n:Label) WHERE true <same AND-conditions as before>` branch per
candidate label and unions them inside a `CALL {...} RETURN ...` subquery,
applying `ORDER BY`/`LIMIT` once after the subquery closes, instead of one
`MATCH (n) WHERE (n:A OR n:B OR ...)` scan. A label-disjunction
`MATCH (n:A|B|C)` was rejected as a smaller alternative because
`docs/public/reference/nornicdb-pitfalls.md` documents it matching zero rows
on the pinned NornicDB backend (the same reason the handler's existing
`searchArgoCDCategoryRows` special case already avoids OR-based label
matching for the argocd-only path). Plain `UNION` (not `UNION ALL`) is used
deliberately so a node cannot appear twice in the result even if a future
schema change lets it carry more than one candidate label.

The `CALL {...}` wrapper is load-bearing, not stylistic, and was added after
a second, larger-scale proof pass caught a real correctness bug in the first
version of this fix (bare top-level `UNION`, no `CALL` wrapper): see
"Bug found during scaled proof" below. New pitfall documented in
`docs/public/reference/nornicdb-pitfalls.md` ("A Bare Top-Level UNION Returns
Nothing When Its First Branch Is Empty").

## No-Regression Evidence

No-Regression Evidence: Backend NornicDB v1.1.11 base (pinned `eshu-nornicdb-pr261:149245885258`,
commit `1492458852588c884c32f70d27ea2ee07086769c`), isolated Compose project
`eshu-5271`, own ports/volumes. Corpus: two representative repositories
bootstrapped from a private fixture source — one general-purpose application
repository (436 files, 2,182 entities) and one real Terraform module
repository (448 files, 3,211 entities, 3,178 infra-labeled graph nodes:
589 `TerraformResource`, 1,481 `TerraformVariable`, 232 `TerraformOutput`,
36 `TerraformProvider`, plus others) — 6,508 total graph nodes, 7,377 edges.

Focused tests: `cd go && go test ./internal/query/... -run
'TestSearchInfraResources|TestInfraSearch|TestOpenAPIInfraSearch|TestAuthMiddlewareWithScopedTokensAllowsInfraSearch'
-count=1` — all pass, including the updated
`TestSearchInfraResourcesArgoCDWithAdditionalFilterKeepsGenericQuery` (now
asserts the fixed per-label-anchored shape instead of the old OR-scan it
previously required), `TestSearchInfraResourcesNeverUsesUnlabeledWholeGraphScan`
(free-text and structured-filter paths, asserts zero `MATCH (n)\n` occurrences
and exactly `len(allInfraLabels)-1` `UNION` branches for the unfiltered/worst
case), and `TestSearchInfraResourcesWrapsUnionInCallSubquery` (asserts the
`CALL {...}` wrapper is present, added after the bug below). Full package:
`cd go && go test ./internal/query/... -count=1` — pass (1.329s). Query-plan
gate: `cd go && go test ./internal/queryplan/... -count=1` — pass, with a new
`QP-INFRA-RESOURCE-SEARCH` registered entry.

### Bug found during scaled proof

The first version of this fix (bare top-level `MATCH (n:Label) ... UNION ...`,
no `CALL` wrapper) passed every unit test and an initial exactness check on a
3-node synthetic fixture, but was **wrong** at realistic cardinality. Proven
directly against the live pinned backend: `allInfraLabels` starts with
`CloudResource`; on the two-repository corpus above, `CloudResource` has zero
matching nodes for most queries while `TerraformVariable`/`TerraformResource`
have real matches. A bare top-level `UNION` (and `UNION ALL`) returns **zero
rows for the entire query** whenever its first branch matches nothing, even
though later branches have real matches — reproduced with the exact generated
Cypher extracted from a Go test (`reader.lastCypher`) run directly against
`http://localhost:7474/db/nornic/tx/commit`, and independently through the
live HTTP API. Query `{"query":"cluster",...}` against the real corpus
returned `count: 0` even though `cluster_name`, `ecs_cluster_name`, and
27 other real matches existed. Wrapping the same union in
`CALL {...} RETURN ...` fixed it in every branch ordering tried; this is now
documented as a new NornicDB pitfall (see Fix above) and guarded by
`TestSearchInfraResourcesWrapsUnionInCallSubquery`.

### Exactness and performance proof (fixed shape, real cardinality)

Old query (reconstructed) vs. new query (`CALL`-wrapped, exact Cypher
extracted from the running fix), both executed directly against the live
Bolt HTTP endpoint on the identical two-repository corpus, `query="cluster"`:

| | Cold | Warm (identical repeat) | Rows |
|---|---:|---:|---:|
| Old (`MATCH (n) WHERE (n:A OR n:B OR ...)`) | 656.78ms | 5.94-6.36ms | 29 |
| New (`CALL`-wrapped per-label `UNION`) | 6.54-10.20ms | 1.50-2.35ms | 29 |

Identical row count and content (`0/0` difference) at both cold and warm —
the fix is two orders of magnitude faster cold (656ms to ~8ms) on this
corpus, and the warm numbers on both sides are consistent with `SmartQueryCache`
exact-match hits, not a faster query plan (see the theory-correction section
above). A live call against the same corpus through the full HTTP API with a
fresh, never-before-used query string (`{"query":"policies-fresh-cold-2",...}`)
also returned `HTTP 200` with correct results, proving the fix end-to-end
through the real handler, not just the raw Cypher.

## Observability Evidence

No-Observability-Change: the fix changes only the internal Cypher shape of an
existing handler. It adds no metric, span, log field, queue stage, worker
knob, or runtime data contract. The existing `SpanQueryInfraResourceSearch`
span and handler-level telemetry wrap the request unchanged regardless of how
many UNION branches the generated statement contains.
