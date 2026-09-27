# #7243 semantic search BM25 query proof

Date: 2026-09-27. The QA database probes below were read-only. Repository and
service identities are omitted. The recorded HTTP and MCP searches used hybrid
mode, a 10-second timeout, and repo scope. The two slow query terms were
`decode` on a 7,097-file corpus (SQL page size 13, including the lookahead
row) and `a` on a 759-file corpus (SQL page size 31).

Performance Evidence: On QA PostgreSQL 18.3, the previous SQL took 2.526366 s
(`decode`) and 6.283867 s (`a`); the final Go-built prepared SQL took
0.128611/0.035196 s and 0.520790/0.150796 s on successive reads of the same
corpora. The indexed document and posting counts, terminal page sizes, plan
shape, cache limitation, and row-equivalence result are detailed below.

## Query plans before the change

`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` of the previous persisted BM25 query:

| Term | Active indexed documents | Matching postings | SQL execution | Dominant plan behavior |
| --- | ---: | ---: | ---: | --- |
| `decode` | 136,442 | 685 | 2.526366 s | Document-first nested loop probes the term index 136,442 times; full JSONB documents are grouped before the page limit. |
| `a` | 22,332 | 5,075 | 6.283867 s | 5,075 document payloads are grouped and sorted before the page limit. |

These are SQL execution times, not HTTP or MCP endpoint measurements.

A first term-materialization and narrow-grouping trial without a bounded
document lookup regressed to 15.065246 s (`decode`) and 10.879664 s (`a`).
Its plan chose the repo document index for every matching posting, causing
millions of shared-buffer hits. That trial was rejected.

## Candidate proof

The query now materializes matching postings once, counts document frequency
across the entire active scope, looks up each matching document by its primary
key through a one-row lateral lookup, groups narrow document keys for BM25
scoring, then fetches JSONB payloads for only the bounded ranked page.
PostgreSQL's [WITH materialization](https://www.postgresql.org/docs/current/queries-with.html#QUERIES-WITH-CTE-MATERIALIZATION)
and [LATERAL evaluation](https://www.postgresql.org/docs/current/queries-table-expressions.html#QUERIES-LATERAL)
contracts informed the query shape; the QA plans above and below establish its
behavior on these corpora.

A `LIMIT 1` trial on `a` executed in 0.167810 s and used the document primary
key for 5,075 lookups. Prepared `EXPLAIN ANALYZE` of the **exact SQL built by
Go** measured first reads of 1.060934 s (`decode`) and 0.767448 s (`a`). Two
subsequent reads measured 0.039361/0.027927 s (`decode`) and
0.200266/0.167914 s (`a`). One first-read `decode` sample exceeded one
second; endpoint cold and warm p95 remain **NOT_CHECKED** until owner deploy
and the recorded sweep. Cache state changed between plan runs, so these
numbers are separate observations, not a claimed speedup ratio.

After the final SQL edit, prepared `EXPLAIN ANALYZE` of the exact Go-built
query measured 0.128611/0.035196 s (`decode`) and 0.520790/0.150796 s (`a`)
in two successive read-only runs. The second run had zero shared-block reads
for each term. These are SQL timings only.

A single-statement read-only QA comparison of the old and candidate scoring
for `a` returned 31/31 ordered rows, zero ID or document-count mismatches,
and zero score delta. Local Postgres tests cover active versus stale generations,
multiple terms, partition pruning, service and source-kind filters, and a
missing-language result on documents with null labels. The latter exposed a
pre-existing scalar-JSON error; the query now treats non-array labels as empty.

Observability Evidence: The existing `query.semantic_search` span covers the
read, while PostgreSQL statement timing and `EXPLAIN` plans isolate its BM25
cost. The SQL rewrite adds no new telemetry label. No production data or
deployment was changed for this proof.
