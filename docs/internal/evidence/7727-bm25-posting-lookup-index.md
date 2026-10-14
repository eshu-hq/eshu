# Evidence: #7727 — hybrid BM25 scores every posting with a heap document lookup

## The defect

In a production-shaped deployment with the semantic embedder off,
`POST /api/v0/search/semantic` in hybrid mode is served by the persisted BM25
reader (`BuildEshuSearchIndexQuery`). A single-letter query (limit 30) on a
repository with about 22k indexed documents took 1.32 s; typical calls take
0.10-0.16 s.

Measured on a read replica (`EXPLAIN (ANALYZE, BUFFERS)`, build `7a882524b`):

| probe | result line | time |
| --- | --- | --- |
| 5,075 postings, pages cached | `Execution Time: 141.154 ms`, `shared hit=46373` | 141 ms |
| 2,218 postings, first run | `shared hit=17631 read=2768` | 984 ms |
| same repository, second run | `Execution Time: 62.994 ms` | 63 ms |
| 5,187 postings, first run | `shared hit=43043 read=4362` | 996 ms |
| 1,040 postings, first run, `track_io_timing=on` | `I/O Timings: shared read=512.303` of 570 ms | 570 ms |
| rare term, 81 postings, first run | `Execution Time: 143.685 ms` | 144 ms |

The scoring query joins every matching posting to
`eshu_search_index_documents` with a single-row lateral `SELECT d.*` before
the page limit, plus one stats join per posting. The query needs only
`repo_id` and `document_length` from that table on the hot path.

## Hypothesis ledger

- C1 (carry `document_length`/`repo_id` on the posting row): schema and writer
  change. Held as a follow-up; not implemented here.
- C2b (covering INCLUDE index on the documents pkey + narrow per-posting
  lateral + stats-row hoist): proven below and implemented in migration 175
  plus `BuildEshuSearchIndexQuery`.
- Rejected until proven: dropping high-frequency terms (changes ranking).

## Scratch proof of C2b

Scratch container `eshu-7727-bm25-proof`, PG 18.6, corpus: 22,000 documents
(~1.5 KB JSONB each, 43 MB relation), 228,321 postings, hot term `a`
scattered over 5,000 postings, mid term 2,200, `oneterm` 1,040, rare term 81 —
matching the issue's probes. VACUUM ANALYZE before each phase.

Warm baseline (old builder) P1 shows the defect shape: per-posting lateral
`Index Scan using eshu_search_index_documents_pkey`, 20,000 buffer hits for
5,000 postings (~79% of the 25,364 total), and a `Seq Scan` of the 1-row stats
table executed 5,000 times (5,000 buffer hits). The final page fetch is
already narrow (120 buffers for 30 rows).

With migration 175 applied and the narrowed query, P1's lateral is
`Index Only Scan using eshu_search_index_documents_bm25_posting_idx` with
`Heap Fetches: 0`, and the stats table is read once.

| shape | baseline | candidate | fingerprint |
| --- | --- | --- | --- |
| P1 hot 5,000 postings, cold (restart) | read=4064, 72.9 ms | read=569, 43.2 ms | `5141e6db…` == |
| P1 warm | 25,364-30,370 bufs | 15,317-20,372 bufs | == |
| P2 2,200 postings, warm | 10,287-11,227 bufs | 6,731-6,829 bufs | `f6ae6e25…` == |
| P3 81 postings, warm | 483-535 bufs | 355-375 bufs | `fb250427…` == |
| P4 hot + source-kind, warm x4 | 28,695 bufs | 25,362 bufs, faster 3/3 repeats | `1563f83d…` == |
| P5 hot + language, warm | 27,861 bufs | 25,362 bufs | == |
| P6 hot + service anchor, warm | 25,486 bufs | 25,362 bufs | `20e873e9…` == |
| P7 hot, limit 31, warm | 30,365 bufs, 31 rows | 20,367 bufs, 31 rows | `6a35dafe…` == |

Ranking equivalence: top-N order plus score, hashed byte-exact, 7/7 shapes
identical between the pre-fix and post-fix builders (6/6 at limit 30, plus
limit-31 top-31). Filtered shapes (P4/P5/P6) keep their predicates and still
fetch the payload for the final page only.

Caveats: host page cache stays warm across container restarts in this
sandbox, so cold wall times are warm-ish and the read-count volume is the
cold evidence. Warm wall-time medians (P1 47.1 -> 39.2 ms over repeats) are
same-host comparisons, not production claims.

## Cost on production

Measured on the scratch corpus: the covering index is 1,680 KB against a
1,296 KB pkey at 22k rows (key duplicated, two narrow INCLUDE columns).
Write amplification is one narrow btree entry per document row; no existing
index is dropped or replaced. The repo-anchor simplification removes two
bind parameters on the hot shape and is exact (`A AND (A OR B)` = `A` when
the anchor is the repo scope); every other anchor shape keeps its predicate
byte-for-byte.

## Cold cache

Not reproduced on production. On the scratch clone the restart-cold read
volume drops 4,064 -> 569 blocks (-86%, about 27 MB of cold I/O at 8 KB
pages), the same order as the 2,768-4,362 production cold reads the fix
removes from the per-posting path.

## Production confirmation (not yet done)

After migration 175 is applied in production, run read-only
`EXPLAIN (ANALYZE, BUFFERS)` of a broad hybrid term on the read replica.
Expected: the per-posting lateral is an Index Only Scan on
`eshu_search_index_documents_bm25_posting_idx` with `Heap Fetches: 0`, and a
first run shows cold reads in the hundreds, not thousands.

## Performance Evidence

Performance Evidence: the pre-fix P1 lateral is an Index Scan on the
documents pkey with 20,000 buffer hits for 5,000 postings plus 5,000 stats
rescans (25,364 buffers warm, 4,064 reads restart-cold). With migration 175
and the narrowed query the lateral is an Index Only Scan on
`eshu_search_index_documents_bm25_posting_idx` with Heap Fetches 0 (15,317
buffers warm, 569 reads restart-cold). Top-31 order and scores byte-identical
7/7 shapes. Production confirmation is pending (above).

## Observability Evidence

No-Observability-Change: the change adds one btree index and reshapes one
query; spans keep their names (`query_returned`, `rows_complete`,
`candidate_count`). Index build progress and size are visible through
`pg_stat_progress_create_index` and `pg_stat_user_indexes`; per-query buffer
behavior stays visible in existing `EXPLAIN (ANALYZE, BUFFERS)` evidence.
