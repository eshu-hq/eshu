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
candidate label and unions them, applying `ORDER BY`/`LIMIT` once after the
final branch, instead of one `MATCH (n) WHERE (n:A OR n:B OR ...)` scan. A
label-disjunction `MATCH (n:A|B|C)` was rejected as a smaller alternative
because `docs/public/reference/nornicdb-pitfalls.md` documents it matching
zero rows on the pinned NornicDB backend (the same reason the handler's
existing `searchArgoCDCategoryRows` special case already avoids OR-based
label matching for the argocd-only path). Plain `UNION` (not `UNION ALL`) is
used deliberately so a node cannot appear twice in the result even if a
future schema change lets it carry more than one candidate label.

## No-Regression Evidence

Backend: NornicDB v1.1.11 base (pinned `eshu-nornicdb-pr261:149245885258`,
commit `1492458852588c884c32f70d27ea2ee07086769c`), isolated Compose project
`eshu-5271`, own ports/volumes. Corpus: one representative repository bootstrapped
from a private fixture source (436 files, 2,182 entities, 14,148 facts).

Focused tests: `cd go && go test ./internal/query/... -run
'TestSearchInfraResources|TestInfraSearch|TestOpenAPIInfraSearch|TestAuthMiddlewareWithScopedTokensAllowsInfraSearch'
-count=1` — all pass, including the updated
`TestSearchInfraResourcesArgoCDWithAdditionalFilterKeepsGenericQuery` (now
asserts the fixed per-label-anchored shape instead of the old OR-scan it
previously required) and the new
`TestSearchInfraResourcesNeverUsesUnlabeledWholeGraphScan` regression guard
(free-text and structured-filter paths, asserts zero `MATCH (n)\n` occurrences
and exactly `len(allInfraLabels)-1` `UNION` branches for the unfiltered/worst
case). Full package: `cd go && go test ./internal/query/... -count=1` — pass
(1.128s). Query-plan gate: `cd go && go test ./internal/queryplan/... -count=1`
— pass, with a new `QP-INFRA-RESOURCE-SEARCH` registered entry.

Exactness proof (old shape vs. new shape, same live data): three synthetic
nodes were created directly against the live Bolt HTTP endpoint, one per
label (`CloudResource`, `TerraformResource`, `K8sResource`), each named
`web-server-a*`. The reconstructed old query
(`MATCH (n) WHERE (n:CloudResource OR n:K8sResource OR ... OR n:HelmValues) AND
(coalesce(n.name,'') CONTAINS $query OR ...) RETURN ... ORDER BY n.name`) and
a live call to the fixed `POST /api/v0/infra/resources/search` endpoint with
`{"query":"web-server-a","limit":10}` both returned the identical three rows
in the identical order (`cr-1`/`CloudResource`, `k8s-1`/`K8sResource`,
`tf-1`/`TerraformResource`) — a `0/0` result-set difference. The synthetic
nodes were deleted after the comparison. A live call against the real
(non-synthetic) bootstrapped repository with `{"query":"boatsdotcom","limit":10}`
returned `HTTP 200` with the correct empty result (`{"count":0,...}`) in
137ms, proving the 27-branch unioned statement (the worst case, no category
filter) is valid Cypher that executes successfully end-to-end on the pinned
backend.

## Observability Evidence

No-Observability-Change: the fix changes only the internal Cypher shape of an
existing handler. It adds no metric, span, log field, queue stage, worker
knob, or runtime data contract. The existing `SpanQueryInfraResourceSearch`
span and handler-level telemetry wrap the request unchanged regardless of how
many UNION branches the generated statement contains.
