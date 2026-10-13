# Unscoped Content Search Signals

A file-content or entity-content substring search with no repository filter
runs inside a work budget (#7730, #7009; see
[Unscoped file search](../http-api/unscoped-file-search.md) and
[Unscoped entity search](../http-api/unscoped-entity-search.md)). These are the
signals an operator uses to see whether the budget is cutting real searches
short, and on which route.

Every metric below carries `table` = `content_files` (the file route) or
`content_entities` (the entity route). Split by `table` before you read a
share or a rate: the two routes have different figures and different classes.

## Metrics

| Metric | Type | Labels | Reading it |
| --- | --- | --- | --- |
| `eshu_dp_content_search_unscoped_total` | counter | `table`; `outcome` = `exact`, `exhausted`, `partial` | `exact`: the page was filled. `exhausted`: the whole key space was read (the answer is complete and may be short). `partial`: the budget ended first. A rising `partial` share means callers are paying for unscoped searches that cannot finish; the fix is scoping (`repo_id`) or a larger `ESHU_CONTENT_SEARCH_BUDGET_MS`. |
| `eshu_dp_content_search_tail_cancel_total` | counter | `table` | Trigram tails the server cancelled at their statement timeout. Rises with common tokens: the tail rechecks every candidate and cannot finish inside the budget. |
| `eshu_dp_content_search_step_cancel_total` | counter | `table` | Key-ordered windows (the probe and continuation steps) the server cancelled at their statement timeout. A cancelled window advances nothing; a rising rate means windows that hold large documents are stopping walks. |
| `eshu_dp_content_search_no_progress_total` | counter | `table` | Partial results that did not advance the cursor. Resuming one repeats the identical request, so each is a dead end for the caller. |
| `eshu_dp_content_search_unscoped_duration_seconds` | histogram | `table`, `outcome` | SQL wall time of one search. Compare its tail with `ESHU_CONTENT_SEARCH_BUDGET_MS`. |
| `eshu_dp_content_search_unscoped_overrun_seconds` | histogram | `table` | How far a cancelled statement ran past its own timeout (0 when none). PostgreSQL cannot interrupt the lowercase copy and decompression of one value, so a large candidate adds to the wall time. Above 0.1 s the partial reason is `budget_exceeded_on_large_document`. |

## Span

`postgres.query` with `db.operation=search_file_content_any_repo_page` (file
route, `db.sql.table=content_files`) or
`db.operation=search_entity_content_any_repo_page` (entity route,
`db.sql.table=content_entities`, `search.content_kind=entity`) carries
`search.scope=unscoped`, `search.budget_ms`, `search.elapsed_ms`,
`search.overrun_ms`, `search.probe_rows_visited`, `search.continuation_steps`,
`search.continuation_rows`, `search.tail_ran`, `search.tail_cancelled`,
`search.outcome` (`exact`, `partial`, `exhausted`), `search.cursor_present`,
`search.phase_one_cap_ms` (the continuation cap at the call's budget: 440 on
the file route and 160 on the entity route at the default budget),
`search.ended_in`, `search.steps_cancelled`, `search.progressed` and
`search.want` (offset + limit + 1, the matches the walk had to find; a high
value on a partial says the caller asked for a large page).

`search.ended_in` names the decision that shaped the walk: `probe`,
`continuation`, `tail`, or `continuation_after_tail` for the last statement
issued; `budget` when less than the tail floor was left after the capped
continuation and the tail was skipped; `tail_skipped_no_trigram` when the
pattern has no three-character alphanumeric run and the tail was skipped for
that reason (the post-skip continuation keeps the skip's name, and the
partial reason is `pattern_has_no_trigram`). `search.progressed` is false
only for a partial that advanced nothing.

## Log

One `content_search.unscoped_partial` (file route) or
`content_search.unscoped_entity_partial` (entity route) line (INFO) per partial
result, with
`reason`, `budget_ms`, `elapsed_ms`, `overrun_ms`, `rows_scanned_in_order`,
`rows_matched`, `tail_ran`, `tail_cancelled`, `progressed`, `steps_cancelled`
and `ended_in`.

At 3 AM: the `partial` share of `eshu_dp_content_search_unscoped_total` by
`table` says how often the budget ends searches on each route; `eshu_dp_content_search_no_progress_total`
says how many of those are dead ends; `search.ended_in` says which phase ends
walks; and `eshu_dp_content_search_step_cancel_total` beside the tail-cancel
counter says whether large windows or the tail are being cancelled.

No span attribute, metric label, or log line carries the search pattern.
