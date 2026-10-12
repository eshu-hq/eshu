# Unscoped file search

`POST /api/v0/content/files/search` and the MCP tool `search_file_content`
answer a file-content substring search. When the request names no repository
(`repo_id` / `repo_ids`) and the caller's token does not restrict repositories,
the search spans every indexed repository and runs inside a work budget. A
search with a repository filter is unchanged.

## Why it is budgeted

PostgreSQL cannot bound a substring search over a whole corpus. The planner
prices the pattern from a histogram of values of at most 1 KB, so one `ANALYZE`
can flip the same pattern between a 9 ms ordered walk and a 3 s trigram bitmap;
and a common token's cost is the byte mass of the files the trigram index
returns, not the number of matches. Eshu bounds the work it issues instead of
trusting the plan.

## What the server does

One call is one read-only snapshot transaction:

1. `eshu_require_content_substring_indexes_ready()` first. Not ready answers
   `503` as before.
2. A **probe** of 200 rows in `repo_id, relative_path` order. Its timeout is
   the remaining budget.
3. **Continuation** steps of 500 rows, capped at 0.55 x the budget (440 ms at
   the default). Common tokens whose matches sit a few thousand rows deep in
   key order are answered here, without the tail. Each step's timeout is the
   room left in the cap, so a window that holds one large file completes when
   the cap can pay for it.
4. A **trigram tail** with a timeout of `min(remaining, 0.5 x budget)`. If less
   than the floor (150 ms at the default) remains, the tail is skipped.
5. **Continuation** with the rest of the budget. At the default ratios
   (continuation cap 0.55, tail cap 0.5) a cancelled tail leaves too little
   budget for another step, so this phase issues no statement.

A window shorter than its size means the key space ended, so the answer is
complete. The page is the first `limit` matches in key order after `offset`
matches; the server reads `offset + limit + 1` matches to set `truncated`.

The budget is `ESHU_CONTENT_SEARCH_BUDGET_MS` (default 800, accepted
100..10000; see [Environment Variable Reference](../env-registry.md)). Every
internal timeout scales with it.

## Partial results

If the budget ends first, the answer is **HTTP 200**, never an error:

```json
{
  "data": {
    "results": [{"repo_id": "...", "relative_path": "..."}],
    "count": 1, "limit": 50, "offset": 0,
    "truncated": true,
    "partial": {
      "reason": "candidate_budget_exceeded",
      "rows_scanned_in_order": 7700,
      "rows_matched": 1,
      "cursor": {"repo_id": "...", "relative_path": "..."},
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
  100: one candidate's recheck (the whole-value lowercase copy and the
  decompression) cannot be interrupted, so a cancelled statement can run past
  its timeout. Otherwise it is `candidate_budget_exceeded`. A token whose
  trigram candidates include one very large document (about 55 MB on the
  measured corpus, about 0.7 s to recheck) is answered partial and over the
  budget by that recheck; issue
  [#7975](https://github.com/eshu-hq/eshu/issues/7975) tracks the fix.
- The call can exceed `budget_ms` by `overrun_ms`; the server reports it
  instead of promising a hard wall bound.

## Resuming

Send `partial.cursor` back as the `cursor` request field and
`offset = max(0, offset - partial.rows_matched)`. The next call reads strictly
after the cursor, so pages have no gap and no duplicate. A cursor on a request
that has a repository filter, or on the entity search, is refused with `400`.
Adding `repo_id` turns a partial into an exact answer.

`partial.progressed` says whether the call advanced the cursor. When it is
`false` the call scanned no rows inside its budget (the first window after the
cursor costs more than the whole budget, for example because it holds very
large documents), `cursor` is the cursor you sent (empty on a
first call) and `rows_matched` is `0`, so resuming repeats the identical
request and will not progress. Do not retry it unchanged: scope the search with
`repo_id`, ask the operator to raise the server setting
`ESHU_CONTENT_SEARCH_BUDGET_MS` (read at startup), or stop. In any case bound a
resume loop (for example a fixed number of resumes) rather than looping until
`partial` disappears; the `hint` then names the next step.

## Operator signals

See [Unscoped File Search Signals](../telemetry/unscoped-file-search.md): the
`eshu_dp_content_search_unscoped_total{outcome}` counter, the tail-cancel
counter, the elapsed and overrun histograms, the `search.*` span attributes,
and one log line per partial. The search pattern is never recorded.
