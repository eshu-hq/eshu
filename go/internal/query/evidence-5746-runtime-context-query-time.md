# Evidence: query-time runtime context for OS-package findings (#5746)

## What changed

The impact-findings handler resolves each finding's runtime context
(workloads, services, deployments, environments, catalog refs) from its
`repository_id` at READ time and attaches it as a labeled `runtime_context`
block. The resolution is one bounded Postgres read per findings page
(`ListSupplyChainImpactRuntimeContext`), added after the existing cloud
runtime probe. The baked `workload_ids`/`service_ids`/`environments` filter
fields are NOT backfilled (that filter rework is #5747).

## Why no regression

The probe is one extra query per findings page, bounded by the page's
distinct repository ids (<= ~50, the findings page limit) and a closed
4-kind fact set. The query mirrors the findings list query's
active-generation join shape and is index-served.

## Measured before/after

| Metric | Baseline | After (#5746) | Input |
|--------|----------|---------------|-------|
| B-7 golden gate wall-time | 100 s | 98 s | 20-repo cassette corpus |
| phase_first_drain | 75.0 s | 66.0 s | — |
| runtime-context join (EXPLAIN ANALYZE) | — | 0.535 ms execution | ALL 27 corpus repos (worst-case partition), index-served on `fact_records_collector_status_active_idx`, 0 rows removed by filter |
| findings page probes | 1 (cloud) | 2 (cloud + runtime-context) | per page, same bounded candidate set |

EXPLAIN ANALYZE was run against the live gate Postgres after a full B-7
bootstrap+collect+drain, on every repository scope in the corpus (the
worst-case candidate set — a page can never name more repositories than the
corpus contains). The plan: Index Scan on
`fact_records_collector_status_active_idx` keyed on
(scope_id, generation_id, fact_kind), 62 index searches, 218 shared buffer
hits, execution time 0.535 ms. Sub-millisecond per page on the real schema,
so the second probe does not change the findings handler's latency envelope.

No-Regression Evidence: B-7 golden gate `verify-golden-corpus-gate`
(credential-free cassette replay, 20-repo corpus, NornicDB v1.1.9, 502 pass /
0 required-fail, wall-time 98 s within baseline). EXPLAIN ANALYZE on the
runtime-context join: 0.535 ms execution over all 27 corpus repositories,
index-served, 0 rows removed by filter. Backend: Postgres (gate compose
stack), NornicDB v1.1.9 for the graph lane. Input: 20-repo cassette corpus,
worst-case candidate partition = all 27 repository scopes.

Observability Evidence / No-Observability-Change: the probe reports through
existing query-span attributes `eshu.query.runtime_context_findings` and
`eshu.query.runtime_context_workloads` on the already-instrumented findings
handler span (`SpanQuerySupplyChainImpactFindings`), and probe errors map to
the existing bounded graph-availability error envelope. No new metric, span,
or log format is required.
