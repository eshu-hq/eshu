# Relevance Walk Telemetry

A `find_code` or `search_entity_content` request that sends `rank_cursor`
walks the first 200 rows of one lane (#7746, see
[Relevance walk](../http-api/rank-walk.md)). These signals show how walks end,
which lane and order served them, and what each page costs.

## Counter

`eshu_dp_query_rank_cursor_total` counts walk requests. Every label is a
closed set:

| Label | Values |
| --- | --- |
| `tool` | `find_code`, `search_entity_content`, `search_file_content` (refusals only; the file route has no walk) |
| `outcome` | `served`, `window_end`, `basis_mismatch` (409), `invalid` (400), `unsupported` (400) |
| `reason` | `none` on a served page; on a 409 `basis_changed` or `order_changed`; on a 400 `malformed`, `tool_mismatch`, `invalid_limit`, `ranker_disabled`, `scope_not_single_repo`, `file_search`, `offset_conflict`, or `cursor_conflict` |

It never carries the query text, a repository ID, or a cursor value.

## Request-span attributes

| Attribute | Meaning |
| --- | --- |
| `search.rank_cursor.outcome` | The `outcome` label value. |
| `search.rank_cursor.reason` | The `reason` label value. |
| `search.rank_cursor.position` | Rows the walk has served through this page (0 on an error). |
| `search.rank_cursor.lane` | `graph` or `content`; served pages only. |
| `search.rank_cursor.order` | `graph_name`, `hybrid`, or `lexical`; served pages only. |

A `hybrid` page also emits the `query.hybrid_rerank` child span (see
[Traces](traces.md)) with `search.rerank.rows` equal to the window size, so
its duration is the per-page scoring cost.

## What an operator reads at 3 AM

| Symptom | Signal | Reading |
| --- | --- | --- |
| Clients restart walks often | `rate(eshu_dp_query_rank_cursor_total{outcome="basis_mismatch"}[5m])` by `reason` | `basis_changed`: the lane or the window changed under the walk (ingest in progress, or the graph lane filled or emptied). `order_changed`: replicas order the same window differently. |
| `order_changed` on nearly every page | the same rate, split by replica | Score bits are stable per CPU architecture only. Replicas of mixed architectures (arm64 and amd64) behind one load balancer can answer `order_changed` on every hybrid page. Pin walks to one architecture, or page with `offset`. |
| Clients send bad cursors | `outcome="invalid"` by `reason` | A client edits, truncates, or reuses a cursor across tools. |
| Walks refused | `outcome="unsupported"` by `reason` | `ranker_disabled` means semantic search is off for `search_entity_content`. |
| Walk pages are slow | `query.hybrid_rerank` duration on walk requests | About 19 ms of CPU per hybrid page at 200 rows on linux/arm64. A large rise points at host CPU pressure, not the store. |

```promql
sum by (tool, outcome, reason) (rate(eshu_dp_query_rank_cursor_total[5m]))
```

Observability Evidence: `TestRecordOutcomeCountsAndTagsTheRequestSpan` and
`TestRecordErrorMapsEveryCodeToItsOutcome` (`go/internal/query/search/rank`)
prove the counter labels and the request-span attributes;
`TestOutcomeLabelsAreClosed` pins the closed outcome and reason sets.
