# #5275 Documentation Query-Plan Evidence

## Decision

This work retains three complementary documentation-findings partial indexes
and rejects a dedicated free-text documentation-facts search index. The
order-first index serves the ordinary unfiltered page, the filter-first index
serves selective pages, and the ACL-visible index serves aggregate reads. Both
list shapes preserve their result sets. The search indexes made reads faster,
but the fastest candidate made the returning-aware production streaming write
2.20 times slower while the complete production 1.6-million-row search
completed in 1.169 seconds without it.

No free-text query or search index ships from this investigation.

## Acceptance disposition

| Issue acceptance item | Disposition | Evidence |
| --- | --- | --- |
| Capture `EXPLAIN (ANALYZE, BUFFERS)` for both reads | Proved | The findings and production-shaped search reads were measured on PostgreSQL 18.4. Sanitized plan nodes, rows, loops, buffers, and timings are below; the exact disposable shim is checked in beside this note. |
| Retain the findings partial indexes | Implemented | Migration 065 creates the order-first unfiltered-list index, migration 066 creates the filter-first selective-list index, and the final schema plus migration 003 retain `fact_records_documentation_findings_visible_idx` for ACL-filtered aggregate total, group, and inventory scans. |
| Redesign free-text search around an indexable column | Superseded by measurement | Three index hypotheses were tested. The fastest plan added a 2.20x median write cost, while the complete production search stayed below the local interactive target. No search schema or query change lands. |
| Preserve exact results | Proved | The ordinary page returned the same 51 rows and digest, and the fully selective page returned the same 66 rows and digest. The accepted patch does not change free-text search behavior. |
| Record before and after plans and wall times | Proved | The accepted findings result and every rejected search candidate are recorded below. |

The third acceptance item is intentionally closed by a measured rejection, not
by an unproven implementation. Revisit it only if the production-reachable
search exceeds its latency target and a candidate avoids the measured write
tax.

## Proof boundary

- Backend: `postgres:18-alpine`, PostgreSQL 18.4.
- Retained store: 8,080,369 total facts, including 1,600,582 documentation rows.
- Findings performance fixture: 200,000 representative rows. The retained store
  had no findings, so this proof used an isolated disposable fixture.
- Migration lifecycle fixture: 2,000 representative findings. This smaller
  fixture tests restart and concurrency behavior; it is not the performance
  fixture.
- Findings inputs: the complete projection, join, predicates, ordering, and
  `limit+1`/offset emitted by `buildDocumentationFindingsSQL` for both the
  no-filter 50-row request and a request using all six optional payload
  filters.
- Search input: the complete SELECT, projection, FROM shape, five-field
  lowercase expression, seven documentation fact kinds, scope, ordering, and
  bounds emitted by `buildDocumentationFactsSQL`.
- Write input: one exact production batch from `buildUpsertFactBatchQuery`:
  500 documentation envelopes, 17 columns, 8,500 bound arguments, the shipped
  multi-value `VALUES` encoding, returning-aware conflict suffix, fencing
  predicate, and `RETURNING fact_id` clause used by the streaming path.
- Machine profile: Apple Silicon `arm64`, 18 logical CPUs, 128 GiB physical
  memory, APFS solid-state storage, macOS 26.5.1. The PostgreSQL container had
  no explicit CPU or memory limit.
- `absolute_target_applicable=false`: this is a named local proof profile, not
  an accepted cross-machine reference profile. The timing supports the local
  disposition and same-machine comparison; it is not a universal latency claim.

The production query and write shapes come from
`go/internal/query/documentation_read_model.go` and
`go/internal/storage/postgres/facts_streaming.go`. The proof changes only the
candidate index definitions between measurements. The exact read fixture,
queries, and candidate DDL are in
[`5275-documentation-query-plan-shim.sql`](5275-documentation-query-plan-shim.sql);
the returning-aware write probe is in
`go/internal/storage/postgres/documentation_facts_write_index_live_test.go`.
The verified SQL shim SHA-256 is
`6d11e61ab057088621b2f4e829a281c7f11375393ee8986fdba8d43cc285c76d`.
The complete current shim was rerun successfully after the two list shapes and
aggregate comparator were bound to production. The findings, aggregate, and
search plan results below come from that run; the returning-aware write-tax
samples come from the exact Go live proof named above.

The measured SQL is mechanically bound to production, rather than trusted as
a hand-maintained copy. Query-package tests render the complete filtered and
unfiltered `buildDocumentationFindingsSQL` statements plus the complete
`buildDocumentationFactsSQL` statement and arguments, then compare them with
the three prepared reads. The write proof calls `buildUpsertFactBatchQuery` directly;
its unit guard checks the production 500-row cardinality, 8,500 arguments,
multi-value `VALUES` encoding, prefix, and complete returning-aware conflict
suffix. Its warmup calls `upsertFactBatchReturningAccepted`, and its measured
`EXPLAIN ANALYZE` uses the same returning-aware query builder and arguments.
The live proof records three rollback-isolated samples for each candidate.
Mutation tests prove that changes on either findings shape or the production
search side break the corresponding guard. The source hashes for this run are:

- `go/internal/query/documentation_read_model.go`:
  `02825515bd6b620cc5c4fa63e4a8d7cb60066b8fa2f6bde46e59099e5b48a1cb`;
- `go/internal/storage/postgres/facts_streaming.go`:
  `daf07fe4672a0b4d914a690d639852370836e4086f2cd98cdff55e0790c967e3`;
- exact aggregate-plan live proof:
  `4b3ad332201d26862efe682c33044fb7fe5149178cbaeb127fbac9e367e1dbfe`;
- exact write-batch live proof:
  `f9b6366e0ba7d47667f116e82bea3c2adff5cdef90a6ebd188d425815fe85f4a`;
- `go/internal/storage/postgres/schema_fact_records_documentation.go`:
  `8ea9f49e147b2e27665601827d6756875924800711844dc3051046a19f970a2c`;
- migration 003:
  `17e98ba4f72588f0913d2ed631e6699c24e52958674b367ca4b9218c1f2a47d1`;
- migration 065:
  `3c6aaf70f96f6bbbc71543bfd71ee8be4aaf0a5d66587d07f6fefe4063da95d9`;
- migration 066:
  `c6c1c8b8193d97f6a0d8eca5b07d54f8ad1943b25b5d1df3d7f893f723f9e8f6`.

## Accepted findings-list index results

The route has two production-reachable shapes that need different leading
keys. The ordinary page has no optional payload predicate and orders newest
first; a fully selective page constrains all six optional payload fields and
then uses the same ordering.

| Shape | Baseline | Accepted plan | Terminal result |
| --- | --- | --- | --- |
| Unfiltered, limit 51 | 151.750 ms; parallel scan + top-N sort; 13,373 shared hits | 0.175 ms; `fact_records_documentation_findings_read_idx`; 7 hits + 3 reads | 51 rows; digest `d74da658d3ef55feb49974eb8c8af836` |
| Six-filter, limit 67 | 11.625 ms; parallel scan + sort; 13,400 shared hits | 0.292 ms; `fact_records_documentation_findings_filter_idx`; 198 hits + 4 reads | 66 rows; digest `b99ca6f196274d3a9b33333c686878a8` |

The theory shim explicitly proves that the order-first index alone does not
fix the selective shape: it still took 12.453 ms and selected the parallel
scan. The filter-first companion then reduced that query to 0.292 ms. Conversely,
the unfiltered query cannot use a filter-first index for its ordering. Keeping
the concerns under distinct stable names gives each actual route shape an
eligible index without changing disclosure behavior or terminal rows.

## Aggregate-visible index retention proof

`CountDocumentationFindings` executes one total plus status, truth-level, and
freshness-state grouped scans; inventory executes one additional grouped scan.
Their shared ACL predicate exactly matches
`fact_records_documentation_findings_visible_idx`, so the two list indexes do
not replace it. The retained 200,000-row planner timing and 500-row
`INSERT ... SELECT` write timing remain theory-shim evidence only. The opt-in
live proof executed the production total, grouped, and inventory builders with
the aggregate-visible index selected: total 7.123 ms, grouped status 5.524 ms,
and inventory status 5.311 ms. Each index scan returned 10,000 rows with 10,351
shared hits and zero shared reads; the live test completed in 5.94 s. The
broad-only comparator remains theory-shim evidence only.

The checked-in theory shim also compares the exact aggregate predicates with
and without the ACL-visible index on the same 5%-visible fixture:

| Builder | Final-index execution | Final-index buffers / plan | List-only execution | List-only buffers / plan |
| --- | ---: | --- | ---: | --- |
| Total | 0.863 ms | 1,551 shared hits; `fact_records_documentation_findings_visible_idx` | 13.091 ms | 13,334 shared hits; `Parallel Seq Scan` |
| Grouped status | 4.271 ms | 10,161 shared hits; `fact_records_documentation_findings_visible_idx` | 12.588 ms | 13,412 shared hits; `Parallel Seq Scan` |

The final-index and list-only aggregate results have bidirectional set
difference 0/0 and the same digest,
`1f713158f4cc5b3d724244b56cbeb292`. This proves that retaining the ACL-visible
index changes the plan and timing, not the aggregate answer.

The exact returning-aware production 500-row write proof recorded visible-only
samples 3.675/3.812/3.959 ms and final-three-index samples
4.735/4.739/5.486 ms: medians 3.812 and 4.739 ms, ratio 1.243x, below the
1.50x rejection threshold.

## Current search baseline

| Measure | Result |
| --- | --- |
| Input | 1,600,000 synthetic documentation rows across the seven production kinds |
| Plan | `Parallel Seq Scan on fact_records` |
| Actual rows / loops | 533.33 rows x 3 worker loops; 532,800 rows filtered per loop |
| Buffers | 144,712 shared hits at the scan; 144,818 for the whole plan |
| Planning / execution | 0.209 ms / 1,169.485 ms |
| Terminal result | 51 rows; digest `2c5a286517da306b3461ba075696a3fa` |

The 1.169-second result is below the 2-3 second target on this named local
profile. Because this machine is not the accepted cross-machine reference,
that does not establish a universal SLO. It does establish that a new write-heavy
index is not justified by the current local path.

## Rejected search-index hypotheses

| Candidate | Build result | Read result | Disposition |
| --- | --- | --- | --- |
| Broad expression GIN with `gin_trgm_ops` | 16.353 s, 315 MB | `Bitmap Index Scan`, 1,600 rows x 1 loop, 22 index hits; whole plan 1,622 hits; 7.558 ms execution | Fastest read, but its exact returning-aware 500-row production batch had a 2.20x median write cost. |
| GiST with `gist_trgm_ops(siglen=64)` | Failed: one index row required 11,912 bytes; PostgreSQL maximum was 8,191 bytes | No valid plan | Rejected for this expression and fixture shape; this is not a general rejection of GiST. |
| Scoped multicolumn GIN using `btree_gin` | 16.420 s, 320 MB | `Bitmap Index Scan`, 1,600 rows x 1 loop, 476 index hits; whole plan 2,076 hits; 11.212 ms execution | Slower and larger than the broad GIN; its exact returning-aware 500-row production batch had a 2.33x median write cost. |

All candidate indexes and the candidate `btree_gin` extension were removed
after the proof. None is part of the accepted schema.

## Write-tax proof

Both valid search candidates were measured with the returning-aware production
statement for one full 500-row streaming batch. The warmup called
`upsertFactBatchReturningAccepted`; each candidate then received three measured
`EXPLAIN ANALYZE` inserts using that statement. Every insert ran in a
transaction that rolled back, so the 1.6-million-row table state stayed
comparable.

| Run | Median execution | Median shared buffers | Median dirtied / written | Multiple of baseline |
| --- | ---: | ---: | ---: | ---: |
| No candidate | 4.207 ms | 6,092 hits | 46 / 46 | 1.00x |
| Broad expression GIN | 9.257 ms | 7,092 hits | 109 / 109 | 2.20x |
| Scoped multicolumn GIN | 9.786 ms | 7,092 hits | 117 / 117 | 2.33x |

The fastest read candidate added 5.050 ms to the median exact production batch.
The three baseline samples were 4.689, 4.207, and 4.154 ms; broad-GIN samples
were 9.367, 9.164, and 9.257 ms; scoped-GIN samples were 9.936, 9.495, and
9.786 ms. This is a bounded same-machine per-batch comparison, not an
end-to-end ingestion duration, so no corpus-wide time is extrapolated from it.

The read improvement is real, but it is not an accuracy fix and it does not
remove an active SLO violation. The write regression therefore fails the
repository's accuracy-then-performance decision order.

## Migration and restart safety

The lifecycle proof starts from the populated pre-065 schema and checks these
boundaries:

- migration 003 and final schema retain the ACL-filtered aggregate-visible
  index, migration 065 creates the order-first list index, and migration 066
  creates the filter-first list index;
- the 2,000 fixture rows remain unchanged throughout the migration;
- a repeated bootstrap keeps all three index definitions and objects unchanged;
- an invalid index left by a failed concurrent build is removed and rebuilt;
- two concurrent bootstrap calls converge on the same three stable indexes.

This proof separates schema lifecycle safety from the 200,000-row query-plan
fixture so a small concurrency test is not presented as performance evidence.

## Evidence markers

Performance Evidence: the accepted order-first index changed the ordinary page
from a 151.750 ms parallel scan and sort to a 0.175 ms index scan with the same
51 rows and digest. The filter-first companion changed the selective page from
11.625 ms to 0.292 ms with the same 66 rows and digest. The
production aggregate builders selected the retained ACL index in
7.123/5.524/5.311 ms, and the exact production write-batch ratio for the final
three-index shape was 1.243x. The complete production 1.6-million-row search
completed in 1,169.485 ms. The fastest rejected search index reduced it to
7.558 ms but increased the median
exact returning-aware 500-row production batch from 4.207 ms to 9.257 ms, so it
did not land.

No-Regression Evidence: both accepted list shapes preserved terminal row count
and result digest. Free-text search behavior is unchanged. Migration tests
cover populated upgrades, invalid-index recovery, repeated bootstrap, and
concurrent bootstrap.

No-Observability-Change: no metric, span, route, worker, queue, lease, or runtime
setting changes. Operators continue to see both reads through `postgres.query`
spans and `eshu_dp_postgres_query_duration_seconds`, with `db.operation` set to
`list_documentation_findings` or `list_documentation_facts`.

## Verification commands

```bash
proof_db=eshu_5275_<run-id>
createdb "$proof_db"
psql -X -d "$proof_db" \
  -f docs/internal/evidence/5275-documentation-query-plan-shim.sql
dropdb "$proof_db"

guard_db=eshu_guard_5275_<run-id>
createdb "$guard_db"
! psql -X -d "$guard_db" \
  -f docs/internal/evidence/5275-documentation-query-plan-shim.sql
psql -X -d "$guard_db" \
  -c "SELECT to_regclass('public.fact_records') IS NULL"
dropdb "$guard_db"

cd go
go test ./internal/query \
  -run 'TestDocumentation(QueryPlanShim|FindingAggregate)' -count=1
go test ./internal/storage/postgres \
  -run 'TestDocumentation(WriteProofUsesCompleteProductionBatch|FindingsIndexesCoverUnfilteredAndSelectiveLists|ReadIndexFinalSchemaMatchesCurrentQueries|ReadIndexContractRejectsEveryMissingKey|FilterIndexContractRejectsEveryMissingKey|ReadIndexConcurrentMigrationIsIsolated|AggregateVisibleIndexAndRejectedIndexesAreReplayed|AggregateVisibleIndexIsRetainedAcrossBootstrapPaths)' \
  -count=1
ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN=<admin-postgres-dsn> \
ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/query ./internal/storage/postgres \
    -run 'TestDocumentation(FindingsIndexRestartSafety|FactsSearchIndexWriteTax|FindingIndexWriteTax|FindingAggregateBuildersUseVisibleIndex)Live$' \
    -count=1 -v
go test ./internal/storage/postgres -count=1
go test ./internal/testutil/postgresproof -count=1
golangci-lint run ./internal/query ./internal/storage/postgres ./internal/testutil/postgresproof

cd ..
bash scripts/test-verify-performance-evidence.sh
ESHU_PERFORMANCE_EVIDENCE_BASE=origin/main \
  bash scripts/verify-performance-evidence.sh
bash scripts/test-verify-package-docs.sh
ESHU_PACKAGE_DOCS_BASE=origin/main bash scripts/verify-package-docs.sh
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

The SQL shim requires a fresh database name beginning with `eshu_5275_` and
refuses every other database. The negative guard proof exited 3 on the deliberate
division-by-zero assertion before creating `fact_records`; the catalog check
returned `NULL` for that table. The checked-in query binding test also rejects a
bare `\quit`, because psql can use it to return a successful exit status. The
verified positive run cleaned up its database; a catalog check returned zero
remaining `eshu_5275_%` databases.

The live restart test requires
`ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN` and the explicit
`ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE=1` guard. It creates only
its own disposable database and must not run against an application database.

## PostgreSQL references

- [CREATE INDEX](https://www.postgresql.org/docs/current/sql-createindex.html)
- [`pg_trgm`](https://www.postgresql.org/docs/current/pgtrgm.html)
- [`btree_gin`](https://www.postgresql.org/docs/current/btree-gin.html)
- [Multicolumn indexes](https://www.postgresql.org/docs/current/indexes-multicolumn.html)

This note intentionally omits connection strings, hostnames, account and scope
identifiers, document identifiers, and private image references. The linked
shim uses synthetic identifiers and `.invalid` URIs. Aggregate corpus counts,
plan statistics, timings, and synthetic result digests are safe to publish.
