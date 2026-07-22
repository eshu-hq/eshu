# #5564 call-graph metrics one-pass read

`POST /api/v0/code/call-graph/metrics` used two query shapes. The hub query
repeated repository containment expansion while counting callers and callees;
the recursive query expanded both directions and repeated repository
containment for each partner. On the retained target graph, both shapes exceeded
a 75-second request deadline even though the repository contained only 42,197
`CALLS` relationships.

The finished path reads the repository's directed `CALLS` edges once. Go then
deduplicates source-target pairs, calculates distinct in/out degree or reverse
pairs, applies the language filter, sorts with complete tie breakers, and takes
the requested `limit+1` page. The response contract and runtime settings do not
change.

## What changes for a user

| Request | Before | After |
| --- | --- | --- |
| Hub functions | The graph repeatedly expanded repository containment while counting each function's callers and callees. | One repository-scoped edge read feeds exact distinct-degree aggregation. |
| Recursive functions | The graph expanded both call directions and repeated repository membership checks. | One edge read builds a reverse-edge lookup and emits each self or mutual pair once. |
| Duplicate `CALLS` edges | Duplicate physical edges could do extra work and could repeat recursive matches. | A directed source-target pair contributes once. |
| Paging ties | Path, line, and name ties had no final stable key. | Function and partner IDs finish the ordering before offset and limit are applied. |

## Theory and target-backend proof

The retained NornicDB graph had 43,167 `Function` nodes and 42,197 `CALLS`
relationships. The current hub and recursive builders each exceeded the
75-second deadline. Those cancelled reads left the shared validation host under
memory pressure, so no candidate number is presented from that host.

The candidate was instead exercised against the exact pinned NornicDB commit
`1492458852588c884c32f70d27ea2ee07086769c` with direct in-memory storage
seeding. At the same 43,167-function/42,197-edge cardinality, the production
edge query returned all 42,197 physical edges in 392.262750 ms. This number
isolates NornicDB query execution and result materialization; it does not include
Bolt, HTTP, or Go aggregation and is not compared numerically with the retained
host timeout.

A separate exactness fixture on the same pinned backend returned all four
seeded physical edges, including a duplicate edge and a self-call. A 250-node,
249-edge same-process shim measured the current hub shape at 1.464834 ms and the
one-pass edge shape at 1.033541 ms. NornicDB did not expose a PROFILE plan, so no
db-hit value is invented for that backend.

## Same-data result and performance proof

Neo4j 2026.05.0 used an isolated disposable database with the same 43,167
functions and 42,197 physical `CALLS` edges. The graph included duplicate
one-way edges, self-calls, mutual pairs, non-mutual calls, two languages, and
stable ordering ties. OLD and CURRENT are the same query bytes because no
intervening change altered these builders.

Each first-read number followed a graph-process restart. The immediate repeat
used the same query, parameters, data, and result boundary. Hub rows are the
requested 25 plus the truncation sentinel; recursive rows are the four exact
pairs in the fixture.

| Shape | Version | Rows | First read | Immediate repeat | PROFILE db hits | Result check |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| Hub | OLD | 26 | 1.841459125 s | 172.453959 ms | 1,484,995 | baseline |
| Hub | CURRENT | 26 | 1.841459125 s | 172.453959 ms | 1,484,995 | same bytes as OLD |
| Hub | CANDIDATE | 26 | 2.811989792 s | 299.101125 ms | 889,047 | exact ordered equality |
| Recursive | OLD | 4 | 1.777042292 s | 76.474500 ms | 724,357 | baseline |
| Recursive | CURRENT | 4 | 1.777042292 s | 76.474500 ms | 724,357 | same bytes as OLD |
| Recursive | CANDIDATE | 4 | 1.531497292 s | 422.738000 ms | 889,047 | exact ordered equality |

The hub plan removes 595,948 db hits (40.1%). Its first read is slower because
the candidate materializes the complete repository edge set before exact global
ranking, but it remains below the checked-in 3-second `local_full_stack` budget
for an indexed workspace. The recursive plan adds 164,690 Neo4j db hits and has
a slower immediate repeat; that tradeoff is accepted because the same query is
392 ms on the target NornicDB engine, avoids the retained target's timeout, and
stays below the checked-in budget on Neo4j. No timeout, worker, connection, or
concurrency setting changed.

The checked-in production-path SLO gate independently seeds the same corpus and
runs `CodeHandler.callGraphMetricsData` four times per variant. It measured hub
at 611.849250 ms on the first read and 346.091250 ms warm median; recursive
measured 219.390958 ms on the first read and 213.175792 ms warm median. These
post-seed timings verify the executable regression gate. The restart-separated
table above remains the cold-process comparison and is not replaced by them.

## Accuracy and edge cases

Focused tests cover:

- duplicate physical edges without inflated hub counts;
- self-calls counting once in each direction;
- one stable row for self and mutual recursion;
- non-mutual calls excluded from recursive results;
- source and partner language filtering;
- empty repositories;
- exact path, line, name, and ID ties;
- offset plus `limit+1` truncation behavior.

## Query-plan and observability evidence

Both capability variants bind to the shared one-pass builder in the production
query-plan manifest. The query anchors both `Function` endpoints by `repo_id`.
It intentionally has no Cypher `LIMIT`: exact global degree and reverse-edge
answers require the complete repository edge set before paging.

The existing `query.call_graph.metrics` span now records:

- `eshu.query.call_graph.metric_type`;
- `eshu.query.call_graph.expanded_edge_count`;
- `eshu.query.call_graph.expanded_node_count`;
- `eshu.query.call_graph.result_count`;
- `eshu.query.call_graph.truncated`.

These are bounded values or counts. They do not expose repository IDs, function
IDs, names, paths, source text, or credentials.

## Verification

```text
cd go
go test ./internal/query -run 'CallGraphMetrics' -count=1
go test ./internal/query ./internal/queryplan -count=1
go test ./internal/telemetry -count=1
go test ./cmd/golden-corpus-gate -count=1
ESHU_CALL_GRAPH_METRICS_SLO_LIVE=1 \
ESHU_CALL_GRAPH_METRICS_SLO_ISOLATED=1 \
go test -tags call_graph_metrics_slo_live ./internal/query \
  -run '^TestCallGraphMetricsInteractiveSLO$' -count=1 -v
cd ..
bash scripts/test-verify-golden-corpus-gate.sh
bash scripts/verify-query-plan-profile.sh
```

The exact NornicDB proof used the pinned source commit and
`go test -tags nolocalllm ./pkg/cypher -run '^TestEshu5564TheoryScaledWall$' -count=1 -v`.
Connection details, hostnames, retained repository identifiers, and credentials
are intentionally omitted.

The full live golden-corpus gate was also attempted twice on isolated ports.
Both runs reached a clean projection-intent drain but stopped on the same 24
pre-query `fact_work_items` dead letters. Every row was in the unrelated
`codeowners_ownership` domain with `projection_bug: codeowners ownership
materialization edge writer is required`. This branch does not change
collection, fact work, CODEOWNERS projection, graph writes, or reducer wiring;
the focused B-7 unit/static contracts above pass. The full live gate is recorded
as blocked by that existing pipeline failure rather than reported as green.
