# #6858: unscoped /iac/resources from infra_resource_entities

## Performance Evidence: unscoped list search and summary moved off the fact_records CTE

Baseline (merged main, read-API latency gate): unscoped
`GET /api/v0/iac/resources` p95 1.23s, 1.53s, 2.07s across GREEN runs,
3.196s on a RED run and 3.01s/3.12s on CI-runner-class runs, all over the
2s ceiling, with flat work counters (2 calls, 108,152 shared buffers,
51 rows). The cost is the `current_iac` DISTINCT ON sort over
`fact_records`, which spills to disk at corpus scale.

After (this change): the same gate at the same corpus
(800 scopes, 150k graph nodes per infra label, 150k IaC facts,
NornicDB v1.3.3, Postgres 18, 20 counted iterations) reports p95
28.7ms with 3 calls, 24,975 shared buffers, 52 rows -- inside both the
2s latency ceiling (with the #6858 exemption removed) and the
committed work budget (13 calls, 324,456 buffers, 102 rows).

Backend/version and input shape for the mechanism proof: EXPLAIN
(ANALYZE, BUFFERS) on shipped SQL at 60k TerraformResource rows,
Postgres 18, same machine, unscoped kind=resource page (limit 51).
CTE: 191ms, 40,908 shared buffers plus temp spill (external merge
sort, ~10MB). Table: 7.6ms, 1,315 shared buffers, in-memory top-N
heapsort. No new index was added: the scan is already an order of
magnitude inside budget, and per index doctrine a covering index
follows only with its own EXPLAIN.

Terminal counts: gate exit 0; live parity suite asserts identical
identity/name order across a 10-search battery and byte-identical
summaries between both paths; scoped, pre-marker, and marker-cleared
reads verified on the CTE via statement capture. The change is safe
because selection semantics are unchanged (same filters, same keyset
order, same facet mapping through one shared helper), graph hydration
is untouched, and currency still fails loud on count/identity/name
mismatch.

## Observability Evidence: existing instruments cover the new path

No new telemetry was added. The route keeps reporting through
`eshu_dp_iac_resource_list_duration_seconds` and
`eshu_dp_iac_resource_list_errors_total` (same names, same kind/reason
labels), the hybrid truth envelope is unchanged, and the gate's
per-route Postgres work meter distinguishes the paths: 108,152 buffers
(CTE) versus 24,975 buffers (table) at full scale. An operator pages
the route's latency and errors exactly as before; a future regression
that pushes reads back onto the CTE shows up first as a ~4x buffer
rise on the existing work report.
