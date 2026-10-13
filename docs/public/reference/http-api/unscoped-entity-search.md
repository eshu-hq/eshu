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
2. A **probe** of 1,000 rows in `repo_id, relative_path, start_line,
   entity_id` order. Its timeout is the remaining budget.
3. **Continuation** steps of 5,000 rows, capped at 0.20 x the budget (160 ms
   at the default, about 35,000 rows). Common tokens whose matches sit near the
   start of the key order are answered here, without the tail. Each step's
   timeout is the room left in the cap.
4. A **trigram tail** with a timeout of `min(remaining, 0.80 x budget)`
   (up to 640 ms at the default). If less than the floor (150 ms at the
   default) remains, the tail is skipped. It is also skipped when the pattern
   has no three-character alphanumeric run (for example `db_`), because the
   trigram index has nothing to extract; step 5 then gets the whole remaining
   budget.
5. **Continuation** with the rest of the budget.

These figures are not the file search's. An ordered entity window costs about
0.3 ms plus 0.34 us per row plus 10.2 ns per byte of `source_cache`, so a short
phase one walks many rows, and the rare-token tails cost 400 to 462 ms, so the
tail gets most of the budget. With the file figures (200, 500, 0.55, 0.50) two
rare-token classes that are exact today end partial. All figures: #7009 S1
shim, QA environment, warm.

One disclosed trade: a common token whose 201st match lies past the first
phase (about 50,000 rows deep) at `limit` 200 ends partial with about 149 rows,
and one resume completes it. Limits up to 100 on the same token stay exact in
one call.

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
      "rows_scanned_in_order": 36000,
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
- One large value cannot be interrupted: its recheck costs 11 to 13 ms per MB,
  and the largest entity `source_cache` measured is 13.1 MB (a recheck of 146
  to 176 ms). A tail cancel that fires about 30 to 170 ms into the tail waits
  for it. At the default budget the tail timeout is about 640 ms, outside that
  window. Budgets under about 220 ms put the tail cancel inside the block
  (extra wait up to about 140 ms); the `budget_exceeded_on_large_document`
  reason needs a tail timeout under about 70 ms. All figures: S1c, QA warm.
  Issue [#7975](https://github.com/eshu-hq/eshu/issues/7975) tracks large
  values.
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
partial.cursor only from a reply without budget_page. `partial.rows_matched`
counts the rows the cap trimmed, so resuming from `partial.cursor` with
`offset - rows_matched` from a trimmed page would skip them. The tool leaves
`next_offset` out when it would pass the 10,000 offset cap: a reply with
`budget_page` and no `next_offset` is such a trimmed page, so do not follow
`partial.cursor` from it; repeat the call with a `limit` no larger than
`budget_page.rows_returned`. The HTTP route has no byte cap.

## Operator signals

The entity search shares the unscoped search counters, elapsed and overrun
histograms, and `search.*` span attributes with the file search (see
[Unscoped Content Search Signals](../telemetry/unscoped-file-search.md)). Every
metric carries `table`: filter on `table="content_entities"` for this route,
for example `eshu_dp_content_search_unscoped_total{table="content_entities",outcome="partial"}`.
Its spans carry `search.content_kind="entity"`,
`db.operation=search_entity_content_any_repo_page`,
`db.sql.table=content_entities` and `search.want` (offset + limit + 1), and
its partial log line is `content_search.unscoped_entity_partial`. The search
pattern is never recorded.
