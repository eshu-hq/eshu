# Unscoped entity search

`POST /api/v0/content/entities/search` and the MCP tool `search_entity_content`
answer an entity source-snippet substring search. When the request names no
repository (`repo_id` / `repo_ids`) and the caller's token does not restrict
repositories, the search spans every indexed repository and runs inside a work
budget. A search with a repository filter is unchanged.

## Why it is budgeted

PostgreSQL cannot bound a substring search over a whole corpus. The planner
prices the pattern from a histogram of values of at most 1 KB, so one `ANALYZE`
can flip the same pattern between a millisecond ordered walk and a tens of
seconds trigram bitmap over the whole candidate set; and a common token's cost
is the byte mass of the entities the trigram index returns, not the number of
matches. Eshu bounds the work it issues instead of trusting the plan.

## What the server does

One call is one read-only snapshot transaction:

1. `eshu_require_content_substring_indexes_ready()` first. Not ready answers
   `503` as before.
2. A **probe** of 200 rows in `repo_id, relative_path, start_line, entity_id`
   order. Its timeout is the remaining budget.
3. **Continuation** steps of 500 rows, capped at 0.55 x the budget (440 ms at
   the default). Common tokens whose matches sit a few thousand rows deep in
   key order are answered here, without the tail. Each step's timeout is the
   room left in the cap, so a window that holds one large document completes
   when the cap can pay for it.
4. A **trigram tail** with a timeout of `min(remaining, 0.5 x budget)`. If less
   than the floor (150 ms at the default) remains, the tail is skipped.
5. **Continuation** with the rest of the budget. At the default ratios
   (continuation cap 0.55, tail cap 0.5) a cancelled tail leaves too little
   budget for another step, so this phase issues no statement.

A window shorter than its size means the key space ended, so the answer is
complete. The page is the first `limit` matches in key order after `offset`
matches; the server reads `offset + limit + 1` matches to set `truncated`. An
exact page carries the same rows, in the same order, with the same payload
(`source_cache`, `metadata`) as the old unbounded statement.

The budget is `ESHU_CONTENT_SEARCH_BUDGET_MS` (default 800, accepted
100..10000; see [Environment Variable Reference](../env-registry.md)), shared
with the unscoped file search. Every internal timeout scales with it.

## Partial results

If the budget ends first, the answer is **HTTP 200**, never an error:

```json
{
  "data": {
    "results": [{"entity_id": "...", "repo_id": "...", "relative_path": "..."}],
    "count": 1, "limit": 50, "offset": 0,
    "truncated": true,
    "partial": {
      "reason": "candidate_budget_exceeded",
      "rows_scanned_in_order": 7700,
      "rows_matched": 1,
      "cursor": {"repo_id": "...", "relative_path": "...", "start_line": 12, "entity_id": "..."},
      "budget_ms": 800, "elapsed_ms": 785, "overrun_ms": 0,
      "progressed": true,
      "hint": "add repo_id (repo_ids in the MCP tool) to scope the search"
    }
  },
  "truth": {"level": "partial", "truncated": true, "reason": "candidate_budget_exceeded"}
}
```

- `results` is an ordered prefix of the exact answer. `truncated` is always
  true with `partial`.
- `truth.level` is `partial`: weaker than `derived`, stronger than `fallback`.
- `reason` is `budget_exceeded_on_large_document` when `overrun_ms` is above
  100: one candidate's recheck cannot be interrupted, so a cancelled statement
  can run past its timeout. It is `pattern_has_no_trigram` when the pattern
  has no three-character alphanumeric run (for example `db_`): the trigram
  tail cannot run as a bounded bitmap scan, so the search skips it and spends
  the whole budget in key order. Otherwise it is `candidate_budget_exceeded`.
- The call can exceed `budget_ms` by `overrun_ms`; the server reports it
  instead of promising a hard wall bound.
- The cursor names one entity (`repo_id`, `relative_path`, `start_line`,
  `entity_id`) because several entities share a file.

## Resuming

Send `partial.cursor` back as the `cursor` request field and
`offset = max(0, offset - partial.rows_matched)`. The next call reads strictly
after the cursor entity, so pages have no gap and no duplicate. A cursor on a
request that has a repository filter is refused with `400`. A cursor without
`entity_id` is refused with `400` too: a file search cursor names no entity,
and resuming after a file key alone would skip the rest of that file's
entities. Adding `repo_id` turns a partial into an exact answer.

`partial.progressed` says whether the call advanced the cursor. When it is
`false` the call scanned no rows inside its budget, `cursor` is the cursor you
sent (empty on a first call) and `rows_matched` is `0`, so resuming repeats the
identical request and will not progress. Do not retry it unchanged: scope the
search with `repo_id`, ask the operator to raise the server setting
`ESHU_CONTENT_SEARCH_BUDGET_MS` (read at startup), or stop. In any case bound a
resume loop (for example a fixed number of resumes) rather than looping until
`partial` disappears; the `hint` then names the next step.

## MCP byte cap and partial on one page

The MCP tool caps a reply at 262,144 bytes. Entity rows carry up to several KiB
each, so a partial page can also be cut to whole rows by that cap; the reply
then carries `budget_page` and `next_offset` beside `partial`. When both are
present, page with next_offset first, keeping the same cursor; follow
partial.cursor only from a reply without next_offset. `partial.rows_matched`
counts the rows the cap trimmed, so resuming from `partial.cursor` with
`offset - rows_matched` from a trimmed page would skip them. The HTTP route has
no byte cap.

## Operator signals

The entity search shares the unscoped search counters, elapsed and overrun
histograms, and `search.*` span attributes with the file search (see [Unscoped
File Search Signals](../telemetry/unscoped-file-search.md)), plus
`search.content_kind="entity"` on its spans. Its `db.operation` is
`search_entity_content_any_repo_page` on the `content_entities` table, and its
partial log line is `content_search.unscoped_entity_partial`. The search
pattern is never recorded.
