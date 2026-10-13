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
| `eshu_dp_content_search_step_cancel_total` | counter | none | Key-ordered windows (the probe and continuation steps) the server cancelled at their statement timeout. A cancelled window advances nothing; a rising rate means windows that hold large documents are stopping walks. |
| `eshu_dp_content_search_no_progress_total` | counter | none | Partial results that did not advance the cursor. Resuming one repeats the identical request, so each is a dead end for the caller. |
| `eshu_dp_content_search_unscoped_duration_seconds` | histogram | `outcome` | SQL wall time of one search. Compare its tail with `ESHU_CONTENT_SEARCH_BUDGET_MS`. |
| `eshu_dp_content_search_unscoped_overrun_seconds` | histogram | none | How far a cancelled statement ran past its own timeout (0 when none). PostgreSQL cannot interrupt the lowercase copy and decompression of one value, so a large candidate adds to the wall time. Above 0.1 s the partial reason is `budget_exceeded_on_large_document`. |

## Span

`postgres.query` with `db.operation=search_file_content_any_repo_page` carries
`search.scope=unscoped`, `search.budget_ms`, `search.elapsed_ms`,
`search.overrun_ms`, `search.probe_rows_visited`, `search.continuation_steps`,
`search.continuation_rows`, `search.tail_ran`, `search.tail_cancelled`,
`search.outcome` (`exact`, `partial`, `exhausted`), `search.cursor_present`,
`search.phase_one_cap_ms` (the continuation cap, 440 at the default budget),
`search.ended_in`, `search.steps_cancelled` and `search.progressed`.

`search.ended_in` names the decision that shaped the walk: `probe`,
`continuation`, `tail`, or `continuation_after_tail` for the last statement
issued; `budget` when less than the tail floor was left after the capped
continuation and the tail was skipped; `tail_skipped_no_trigram` when the
pattern has no three-character alphanumeric run and the tail was skipped for
that reason (the post-skip continuation keeps the skip's name, and the
partial reason is `pattern_has_no_trigram`). `search.progressed` is false
only for a partial that advanced nothing.

## Log

One `content_search.unscoped_partial` line (INFO) per partial result, with
`reason`, `budget_ms`, `elapsed_ms`, `overrun_ms`, `rows_scanned_in_order`,
`rows_matched`, `tail_ran`, `tail_cancelled`, `progressed`, `steps_cancelled`
and `ended_in`.

At 3 AM: the `partial` share of `eshu_dp_content_search_unscoped_total` says
how often the budget ends searches; `eshu_dp_content_search_no_progress_total`
says how many of those are dead ends; `search.ended_in` says which phase ends
walks; and `eshu_dp_content_search_step_cancel_total` beside the tail-cancel
counter says whether large windows or the tail are being cancelled.

No span attribute, metric label, or log line carries the search pattern.
