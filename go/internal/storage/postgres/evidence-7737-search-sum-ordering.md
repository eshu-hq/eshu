# Evidence #7737 — ordered BM25 score summation

The persisted keyword score summed per-term BM25 contributions with a bare
`SUM(float8)`. Float addition is not associative, so the plan-dependent row
order reaching the aggregate changed score bits and tie order for
three-or-more-term queries. The fix orders the aggregate on the stable
per-term key (`t.term_key, t.term`) in
`go/internal/storage/postgres/search/index/store.go`
(`BuildEshuSearchIndexQuery`); the outer `score DESC, document_id ASC`
tie-break is unchanged.

## Performance Evidence:

Measured `EXPLAIN (ANALYZE, BUFFERS)` of the production query built by
`BuildEshuSearchIndexQuery` for a 3-term query (`alpha beta gamma`,
`LIMIT 31`), before (origin/main text) vs after (ordered aggregate).
Both texts are genuine builder outputs captured from the code, not
hand copies.

- Baseline: median 28.8 ms over 5 runs (28.7, 28.7, 28.8, 29.1, 29.1).
- After: median 35.3 ms over 5 runs (35.3, 35.3, 35.3, 35.4, 35.5).
- Backend: local PostgreSQL 16.15 scratch instance
  (`initdb`, trust auth, port 55432).
- Input shape: 3000 documents, ~9600 postings across 5 terms with varying
  term frequencies and document lengths; 7200 matched postings feed the
  aggregation; 3000 output groups; page limit 31.
- Row counts / I/O: shared buffer hits identical before/after
  (14708 vs 14705) — the same reads, no new scans. The delta is CPU in
  the pre-aggregation quicksort: the sort key grows from
  `(scope_id, generation_id, document_id, document_count)` to also carry
  `(term_key, term)` (row width 61 to 125, sort memory 811 kB to
  1318 kB). Plan shape is unchanged: Sort, GroupAggregate, top-N
  heapsort. The covering posting-index live proof still passes with
  0 heap fetches and no sequential document scan.
- Why safe: the sorted set is bounded by the query's matched postings,
  not the corpus; the absolute delta is single-digit milliseconds; and
  the ordering is required for score determinism — no cheaper
  formulation fixes the summation order. Byte-identical output across
  the default, `enable_sort=off`, and
  `enable_nestloop=off, enable_hashagg=off` plans is proven by
  `TestEshuSearchIndexBM25ScoreStableAcrossPlansLive`, which runs all
  three plans with genuinely different shapes (Hash Join vs Nested Loop,
  different scan/sort placement).

## No-Observability-Change:

No metrics, spans, structured-log fields, or status surfaces were added,
removed, or renamed. The change edits one SQL expression; the existing
persisted-index spans (`stats_complete`, `query_returned`, `first_row`,
`rows_complete`) fire unchanged, and the live plan-stability test
executes the full `Search` path including that instrumentation.
