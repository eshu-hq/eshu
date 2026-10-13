# #8002 cost 3: no-trigram tail bound

Theory proof ran before the change, on the QA environment read replica
(read-only session, 2026-10-13), using the exact
shipped statement text from `go/internal/query/search/unscoped/entities.go`
under `SET LOCAL enable_indexscan = off`, as production runs the tail.
Corpus: `content_entities` ~2,715,727 rows, 8,422 MB. Predictions were
posted on #8002 before measuring.

Performance Evidence: pattern `db_` (no three-character alphanumeric run,
so pg_trgm extracts no trigram) plans `Parallel Seq Scan` + Gather Merge:
Execution Time 3,930 ms warm (5,264 ms cold), Buffers shared hit=385,316
read=183,162. Against the 400 ms tail cap at the default 800 ms budget,
that tail is cancelled on every call, so the class pays the cap plus
recheck overrun for a statement that can never complete. After this change
the walk skips the tail for this class and spends the remaining budget in
key order instead: the fake-clock rows
`TestSearchSkipsTailWhenPatternHasNoTrigram` /
`TestSearchKeepsTailWhenPatternHasTrigram` pin that the tail statement is
never issued for `db_` and still runs for `needle`. No-regression read on
the touched path: trigram-bearing patterns keep the identical phase order
(control row above), and the from-start `decode` tail on the same replica
measured 1,407 ms warm (Bitmap Index Scan 26,758 rows, Heap Blocks
exact=23,622), unchanged by this change.

Observability Evidence: no new metric or log type. The skipped tail is
visible on the existing signals: `search.ended_in` gains the value
`tail_skipped_no_trigram` (span attribute and partial log field), the
partial reason is `pattern_has_no_trigram`
(`querycontract.SearchPartialPatternHasNoTrigram`), and
`search.tail_ran` stays false. The pattern text itself is never recorded,
per the package invariant.

Scope: cost 3 of #8002 only. Cost 1 (resume pays the tail again: measured
1,307 ms from a ~10% cursor vs 1,407 ms from start, same replica) and
cost 2 (density extension, blocked on the deployed first change reaching
the QA environment) are separate measured changes; the issue stays open for them.
