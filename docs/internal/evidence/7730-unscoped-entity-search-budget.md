# 7730 unscoped entity search under a statement budget

## Context

Issue eshu-hq/eshu#7730: `search_entity_content` with no repository filter
ran one unbounded trigram statement over the whole corpus. On the QA read
replica the `from_event` call cost 1.4578 to 1.4805 s in the statement plus
0.16 to 0.20 s decode for 50 rows with `truncated=true` and no partial
envelope; the issue records 4.762 s cold on first call and 0.84 s warm
(30,782 trigram TIDs, planner estimate 146 times low). The fix ports the
proven #7836 file-twin walk to `content_entities`: a 200-row key-ordered
probe, 500-row continuation steps capped at 0.55 times the budget (440 ms
at the 800 ms default), a bounded trigram tail, and a reason-coded partial
envelope (`truncated` plus reason, progressed flag, four-part resume
cursor) instead of silent truncation. Scoped searches, the file path,
remainder costs, and the large-document recheck are untouched.

## Performance and observability summary

Performance Evidence (#7730): baseline is the shipped
`searchEntityContentAnyRepoPage` statement
(`source_cache ILIKE '%' || $1 || '%' ORDER BY repo_id, relative_path,
start_line, entity_id LIMIT 51`); after is the budgeted walk. Backend:
Postgres 18.6 scratch shaped like production (2,680,000
`content_entities` rows, production index set, `VACUUM ANALYZE`, same
storage state back-to-back), plus the QA baseline above. Input shape is
the unscoped entity substring page (`LIMIT 51`, common token `request`
with 616,400 matches, `handler` with 833,480, selective `zqxjqqq` with
8,040). Before on scratch: `request` page 12.848 s (1,012,585 rows
removed by recheck, 34,279 exact plus 33,025 lossy heap blocks),
`handler` page 14.399 s, selective token 14.5 ms. After on the same
scratch: probe (first 200-row ordered window, want 51) 1.297 ms with Heap
Fetches 0; continuation capped at 440 ms by construction (QA
continuation-shim class costs: partial tokens 289 to 409 ms, zero-match
tail 268 ms, selective tails 300 ms); the unbounded tail shape (12.866 s)
is refused by the budget stop with a reason-coded partial instead of
waiting. Exact first pages equal the old statement row for row: old 51
keys vs walk 51 keys with 0 mismatched positions on scratch, plus the
hermetic differential and the `postgres_ci` live differential. It is safe
because the walk reads the same 10 columns through the same metadata
decode in one read-only snapshot transaction with savepoint discipline,
the partial envelope is explicit (never silent), and the worst case of a
cold page cache is a bounded partial with a resume cursor, not an
unbounded wait.

Observability Evidence: new operator signals in
`go/internal/query/search/unscoped/entity_telemetry.go`. One
`postgres.query` span with `db.operation` set to
`search_entity_content_any_repo_page` (distinct from the file search so
dashboards split them) plus `search.budget_ms`, `search.elapsed_ms`,
`search.overrun_ms`, probe and continuation row/step counts,
`search.tail_ran`, `search.tail_cancelled`, `search.outcome`,
`search.cursor_present`, `search.ended_in`, and `search.progressed`
attributes; the shared `ContentSearchUnscoped` outcome counter; and one
structured `content_search.unscoped_entity_partial` log line (reason,
budget, elapsed, overrun, rows scanned and matched, tail flags,
progressed) on every partial. None carries the search pattern. An
operator at 3 AM tells a budget stop from a tail stop from
`search.ended_in` and `search.outcome` without re-running the query.
