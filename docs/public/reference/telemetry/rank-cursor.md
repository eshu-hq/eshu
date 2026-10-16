# Relevance Walk Telemetry

A `find_code` or `search_entity_content` request that sends `rank_cursor`
walks the first 200 lexical rows in hybrid relevance order (#7746, see
[Relevance walk](../http-api/rank-walk.md)). These signals show how walks end
and what each page costs.

## Counter

`eshu_dp_query_rank_cursor_total` counts walk requests. Both labels are closed
sets:

| Label | Values |
| --- | --- |
| `tool` | `find_code`, `search_entity_content`, `search_file_content` (refusals only; the file route has no walk) |
| `outcome` | `served`, `window_end`, `basis_mismatch` (409), `invalid` (400), `unsupported` (400) |

It never carries the query text, a repository ID, or a cursor value.

## Request-span attributes

Every walk request sets these on the request span:

| Attribute | Meaning |
| --- | --- |
| `search.rank_cursor.outcome` | The `outcome` label value. |
| `search.rank_cursor.served` | Rows the walk has served through this page (0 on an error). |
| `search.rank_cursor.reason` | The `details.reason` of a 400 or 409, for example `rank_order_changed`. |

The scoring pass emits the `query.hybrid_rerank` child span (see
[Traces](traces.md)) with `search.rerank.rows` equal to the window size, so
its duration is the per-page scoring cost.

## What an operator reads at 3 AM

| Symptom | Signal | Reading |
| --- | --- | --- |
| Clients restart walks often | `rate(eshu_dp_query_rank_cursor_total{outcome="basis_mismatch"}[5m])` | Split by `search.rank_cursor.reason` on the request spans. `candidate_set_changed` means the content index changed under the walk (ingest in progress). `rank_order_changed` means replicas order the same window differently: check for mixed architectures or binaries behind the load balancer. |
| Clients send bad cursors | `outcome="invalid"` | A client edits, truncates, or reuses a cursor across tools. |
| Walks refused | `outcome="unsupported"` with `search.rank_cursor.reason` | `ranker_disabled` means semantic search is off in this deployment. |
| Walk pages are slow | `query.hybrid_rerank` duration on walk requests | About 19 ms of CPU per page at 200 rows on linux/arm64. A large rise points at host CPU pressure, not the store. |

```promql
sum by (tool, outcome) (rate(eshu_dp_query_rank_cursor_total[5m]))
```

Observability Evidence: `TestRecordOutcomeCountsAndTagsTheRequestSpan` and
`TestRecordErrorMapsEveryCodeToItsOutcome` (`go/internal/query/search/rank`)
prove the counter labels and the request-span attributes;
`TestOutcomeLabelsAreClosed` pins the closed outcome set.
