# Unscoped File Search Signals

A file-content substring search with no repository filter runs inside a work
budget (#7730; see [Unscoped file search](../http-api/unscoped-file-search.md)).
These are the signals an operator uses to see whether the budget is cutting real
searches short.

## Metrics

| Metric | Type | Labels | Reading it |
| --- | --- | --- | --- |
| `eshu_dp_content_search_unscoped_total` | counter | `outcome` = `exact`, `exhausted`, `partial` | `exact`: the page was filled. `exhausted`: the whole key space was read (the answer is complete and may be short). `partial`: the budget ended first. A rising `partial` share means callers are paying for unscoped searches that cannot finish; the fix is scoping (`repo_id`) or a larger `ESHU_CONTENT_SEARCH_BUDGET_MS`. |
| `eshu_dp_content_search_tail_cancel_total` | counter | none | Trigram tails the server cancelled at their statement timeout. Rises with common tokens: the tail rechecks every candidate and cannot finish inside the budget. |
| `eshu_dp_content_search_unscoped_duration_seconds` | histogram | `outcome` | SQL wall time of one search. Compare its tail with `ESHU_CONTENT_SEARCH_BUDGET_MS`. |
| `eshu_dp_content_search_unscoped_overrun_seconds` | histogram | none | How far a cancelled statement ran past its own timeout (0 when none). PostgreSQL cannot interrupt the lowercase copy and decompression of one value, so a large candidate adds to the wall time. Above 0.1 s the partial reason is `budget_exceeded_on_large_document`. |

## Span

`postgres.query` with `db.operation=search_file_content_any_repo_page` carries
`search.scope=unscoped`, `search.budget_ms`, `search.elapsed_ms`,
`search.overrun_ms`, `search.probe_rows_visited`, `search.continuation_steps`,
`search.continuation_rows`, `search.tail_ran`, `search.tail_cancelled`,
`search.outcome` (`exact`, `partial`, `exhausted`) and `search.cursor_present`.

## Log

One `content_search.unscoped_partial` line (INFO) per partial result, with
`reason`, `budget_ms`, `elapsed_ms`, `overrun_ms`, `rows_scanned_in_order`,
`rows_matched`, `tail_ran` and `tail_cancelled`.

No span attribute, metric label, or log line carries the search pattern.
