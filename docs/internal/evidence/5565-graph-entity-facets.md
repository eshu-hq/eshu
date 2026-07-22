# #5565 graph entity facet counts

`GET /api/v0/graph/entities` used to execute eight serial graph requests before
it could return the facet row. Selecting a facet added a ninth request for the
entity page. Each count query was label-bounded, but the handler paid session,
transport, parsing, and result-collection overhead eight times.

The count phase now sends one query containing eight scalar `CALL` subqueries,
one per concrete label. Each subquery returns one named count column and the
outer clause only passes through those columns. This detail is required on
NornicDB: grouping or summing `CALL` output can corrupt columns, and an aggregate
branch with no matches can return a zero count while nulling literal facet
metadata. Go validates the single scalar row, restores the catalog order, and
computes the total.

## User-visible behavior

| Request | Before | After |
| --- | --- | --- |
| No `kind` filter | Eight serial label-count requests | One request with eight scalar label-count subqueries |
| Selected `kind` | Eight count requests, then one bounded list request | One count request, then one bounded list request |
| Empty facet | A separate count request returned zero | Its named scalar column returns the same zero |
| Missing or corrupt backend result | A failed request stopped at that label | Missing, extra, malformed, or negative columns fail the whole response closed |
| Facet order | Fixed catalog loop order | Same fixed catalog order, independent of backend column order |

The response schema, facet names, totals, list ordering, search behavior,
offset/limit handling, and truncation flag do not change.

## Theory gate and zero-label correction

The first proof used a throwaway Bolt harness against the pinned
`timothyswt/nornicdb-cpu-bge:v1.1.11` image. It seeded 91,000 synthetic nodes:
700 nodes for each of the eight production facet labels and 85,400 unrelated
nodes. Twenty-five alternating warm samples compared the exact production
eight-request sequence with an initial union candidate.

| Metric | Eight requests | Initial union | Result |
| --- | ---: | ---: | --- |
| First observed count phase | 21.531 ms | 16.060 ms | exact counts |
| Warm median | 1.047 ms | 0.185 ms | 5.665x faster |
| Warm p95 | 1.205 ms | 0.219 ms | 5.502x faster |
| Graph round trips | 8 | 1 | seven removed |

This first proof isolated the query and transport shape, but every facet label
was populated. An authenticated run on the current Compose NornicDB then found
the missing case: an empty aggregate branch returned `c=0` while nulling the
literal facet key and label. The decoder failed closed with HTTP 500. That union
shape was rejected.

A throwaway compatibility probe compared grouped, optional-match, seeded-union,
and scalar-subquery shapes on the same backend. Only the scalar shape returned
all named columns with exact zeros for empty labels. The checked-in SLO now
deletes one populated label after its timed runs and compares the old and new
ordered results again, so this regression cannot hide behind an all-populated
performance corpus.

## Production-path SLO proof

The checked-in `TestGraphEntityInventoryInteractiveSLO` test rebuilt the same
91,000-node shape in an isolated pinned NornicDB container and executed the
production builder, `Neo4jReader`, and decoder four times. It also ran the old
eight-request reference on the same data before every candidate sample and
compared the ordered rows and total exactly.

| Metric | Before | After | Delta |
| --- | ---: | ---: | ---: |
| First read | 21.165 ms | 3.397 ms | 84.0% lower |
| Warm median | 1.286 ms | 0.206 ms | 84.0% lower |
| Checked-in SLO | 3.000 s | 3.000 s | all candidate runs passed |
| Ordered facet result | baseline | identical | no change |
| Zero-label result | baseline | identical | explicit zero preserved |

Pinned NornicDB v1.1.11 did not return PROFILE db-hit counters within the
time-boxed diagnostic, so the authoritative NornicDB SLO leaves profiling off
and records `db_hits=unavailable`. The same 91,000-node test on the pinned Neo4j
compatibility backend enables profiling and reports 8 db hits for the
eight-request reference and 8 for the scalar candidate. The optimization removes
seven transport/session round trips; it does not claim less label-count work.

Performance Evidence: the theory and production-path runs use the same backend
image, node counts, facet distribution, query boundaries, and storage state for
each before/after pair. The change is a handler win on the fixed facet-count
stage.

For the issue's OLD/CURRENT/CANDIDATE accounting: the retained OLD route timed
out cold at 60 seconds and returned HTTP 500 after an 11.2-second warm attempt;
round-trip and db-hit detail was unavailable from those failed requests. The
controlled CURRENT reference above is 8 round trips, 21.165 ms first, and
1.286 ms warm median. The CANDIDATE is 1 round trip, 3.397 ms first, and
0.206 ms warm median. Neo4j compatibility PROFILE reports 8/8 db hits; pinned
NornicDB reports db hits as unavailable.

## Authenticated route proof

The final worktree API ran against the credential-free B-7 corpus on the current
Compose NornicDB. An authenticated request for `kind=repositories&limit=10`
returned HTTP 200 in 9.6 ms with all eight facet rows in catalog order, total
169, repository count 25, ten entities, and `truncated=true`. The identity/IAM
and networking facets both returned explicit zero counts. The same request had
returned HTTP 500 before the scalar correction. The corpus and credential were
synthetic; no retained identifier or secret is recorded here.

## Accuracy and edge cases

Focused tests cover:

- an empty graph, a graph with one populated label, and all eight labels;
- scalar columns presented independently of display order;
- a selected kind and the optional second list request;
- unknown kinds;
- case-insensitive name search;
- `limit + 1` truncation;
- missing, extra, non-integer, and negative count columns;
- the exact eight scalar CALL subqueries, absence of a union, absence of an
  all-node scan, and absence of outer aggregation.

## Query-plan and observability evidence

`QP-GRAPH-ENTITY-COUNT` binds to the production scalar builder and keeps
`AllNodesScan` and `UnboundedExpand` forbidden. The live Neo4j planner resolves
the scalar counts through eight label count-store anchors and exactly seven
one-row `CartesianProduct` joins. The closed policy admits that count only for
this fixed scalar shape; every other registered query still requires zero
Cartesian products. The list variant remains registered separately as
`QP-GRAPH-ENTITY-LIST`.

The `query.graph_entity_inventory` span records:

- `eshu.query.graph_entity_inventory.round_trip_count`;
- `eshu.query.graph_entity_inventory.facet_row_count`;
- `eshu.query.graph_entity_inventory.result_count`;
- `eshu.query.graph_entity_inventory.truncated`.

Each value is a bounded count or boolean. The span does not include entity,
repository, account, tenant, or credential values. Shared per-endpoint metrics
continue to provide route latency and server-error rate.

Observability Evidence: the route span distinguishes count-only requests from
count-plus-list requests and records the returned page state. Existing
`neo4j.query` child spans retain statement-level timing, while
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total` retain route-level latency and failures.

## Focused verification

```text
cd go
go test ./internal/query -run '^(TestGraphEntityInventory|TestGraphEntityKindCounts|TestDecodeGraphEntityKindCounts)' -count=1
go test ./internal/query -run '^TestHandlerQueryplanManifestBindsProductionBuilders$' -count=1
go test ./internal/queryplan -run '^TestHotCypherManifestCoversEveryProductionQueryCall$' -count=1
ESHU_GRAPH_ENTITY_INVENTORY_SLO_LIVE=1 \
ESHU_GRAPH_ENTITY_INVENTORY_SLO_ISOLATED=1 \
go test -tags graph_entity_inventory_slo_live ./internal/query \
  -run '^TestGraphEntityInventoryInteractiveSLO$' -count=1 -v
```

The live SLO container was removed after the proof. The harness used synthetic
identities only; no retained repository names, account IDs, host details,
credentials, or local machine paths are recorded here.
