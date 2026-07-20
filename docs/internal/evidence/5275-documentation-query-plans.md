# #5275 Documentation Query-Plan Evidence

## Decision

This work accepts the documentation-findings partial-index correction and
rejects a dedicated free-text documentation-facts search index. The findings
change fixes a stale predicate and preserves the result set. The search indexes
made reads faster, but the strongest candidate made a production-shaped write
6.68 times slower while the existing worst retained search was already within
the 2-3 second interactive target.

No free-text query or search index ships from this investigation.

## Acceptance disposition

| Issue acceptance item | Disposition | Evidence |
| --- | --- | --- |
| Capture `EXPLAIN (ANALYZE, BUFFERS)` for both reads | Proved | The findings read and production-reachable search shapes were measured on PostgreSQL 18. Sensitive literals and connection details are omitted here. |
| Restore or replace the findings partial index | Implemented | Migration 064 creates the replacement index with the active `documentation_finding` predicate before migration 065 drops the stale legacy index. |
| Redesign free-text search around an indexable column | Superseded by measurement | Three index hypotheses were tested. The best read plan added an unacceptable 6.68x write cost, while the existing worst search remained below the interactive target. No search schema or query change lands. |
| Preserve exact results | Proved | The findings read returned the same 66 rows and digest before and after the correction. The accepted patch does not change free-text search behavior. |
| Record before and after plans and wall times | Proved | The accepted findings result and every rejected search candidate are recorded below. |

The third acceptance item is intentionally closed by a measured rejection, not
by an unproven implementation. Revisit it only if the production-reachable
search exceeds its latency target and a candidate avoids the measured write
tax.

## Proof boundary

- Backend: PostgreSQL 18.
- Retained store: 8,080,369 total facts, including 1,600,582 documentation rows.
- Findings performance fixture: 200,000 representative rows. The retained store
  had no findings, so this proof used an isolated disposable fixture.
- Migration lifecycle fixture: 2,000 representative findings. This smaller
  fixture tests restart and concurrency behavior; it is not the performance
  fixture.
- Search input: the exact five-field lowercase expression and seven
  documentation fact kinds used by `list_documentation_facts`.
- Write input: the production 17-column `fact_records` insert and conflict shape
  used by the streaming writer, applied to 10,000 documentation rows.

The production query and write shapes come from
`go/internal/query/documentation_read_model.go` and
`go/internal/storage/postgres/facts_streaming.go`. The proof changes only the
candidate index definition between measurements.

## Accepted findings-index result

| Measure | Stale predicate | Corrected predicate | Result |
| --- | ---: | ---: | --- |
| Wall time | 26.286 ms | 0.129 ms | 26.157 ms faster |
| Terminal rows | 66 | 66 | identical |
| Result digest | `e0030a0775056615341dae6a6bea1ea6` | `e0030a0775056615341dae6a6bea1ea6` | identical |

The stale partial predicate forced a sequential scan. The corrected predicate
made the current disclosure read eligible for its replacement partial index.
The result identity shows that the speedup did not change disclosure behavior.

## Current search baselines

| Production-reachable search | Selected existing index | Wall time | Rows filtered |
| --- | --- | ---: | ---: |
| Unscoped `documentation_source` search | `fact_records_documentation_sources_observed_idx` | 7.359 ms | 995 |
| Largest retained scope, seven documentation kinds | `fact_records_scope_generation_idx` | 620.585 ms | 1,012,622 |

The 620.585 ms case is the relevant worst retained baseline. It is below the
2-3 second interactive target, so this path currently requires no additional
latency saving.

## Rejected search-index hypotheses

| Candidate | Build result | Read result | Disposition |
| --- | --- | --- | --- |
| Broad expression GIN with `gin_trgm_ops` | 21.27 s, 168 MB | Unscoped source regressed from 7.359 ms to 73.987 ms; largest scope improved from 620.585 ms to 72.372 ms | Rejected because it produced a mixed plan and was dominated by the scoped candidate. |
| GiST with `gist_trgm_ops(siglen=64)` | Build failed because an index row required 10,984 bytes and the maximum was 8,191 bytes | No valid read plan | Rejected for this expression and retained data shape. This result does not claim that GiST is unsuitable in general. |
| Scoped multicolumn GIN using `btree_gin` | 14.20 s, 172 MB | Unscoped source improved to 2.668 ms; largest scope improved to 1.007 ms | Rejected because its write cost was not justified by the current read target. |

All candidate indexes and the candidate `btree_gin` extension were removed
after the proof. None is part of the accepted schema.

## Write-tax proof

The strongest search candidate was measured against the production insert and
conflict shape on the same 10,000-row workload.

| Run | Without candidate | With candidate |
| --- | ---: | ---: |
| Cold comparison | 88.903 ms | 575.367 ms |
| Repeat comparison | 85.396 ms | 570.137 ms |

The repeat run added 484.741 ms and made the write 6.68 times slower. Applied
linearly to the retained 1.6 million documentation rows, that would add about
78 seconds. That figure is an extrapolation, not an end-to-end measurement.

The read improvement is real, but it is not an accuracy fix and it does not
remove an active SLO violation. The write regression therefore fails the
repository's accuracy-then-performance decision order.

## Migration and restart safety

The lifecycle proof starts from the populated pre-064 schema and checks these
boundaries:

- migration 064 creates the replacement before migration 065 drops the legacy
  index;
- the 2,000 fixture rows remain unchanged throughout the migration;
- a repeated bootstrap keeps the same replacement index definition and object;
- an invalid index left by a failed concurrent build is removed and rebuilt;
- two concurrent bootstrap calls converge on one stable replacement index.

This proof separates schema lifecycle safety from the 200,000-row query-plan
fixture so a small concurrency test is not presented as performance evidence.

## Evidence markers

Performance Evidence: the accepted findings index reduced the representative
read from 26.286 ms to 0.129 ms with the same 66 rows and digest. The slowest
production-reachable retained search completed in 620.585 ms. The strongest
rejected search index reduced that read to 1.007 ms but increased the repeat
10,000-row production-shaped write from 85.396 ms to 570.137 ms, so it did not
land.

No-Regression Evidence: the accepted findings change preserved terminal row
count and result digest. Free-text search behavior is unchanged. Migration tests
cover populated upgrades, invalid-index recovery, repeated bootstrap, and
concurrent bootstrap.

No-Observability-Change: no metric, span, route, worker, queue, lease, or runtime
setting changes. Operators continue to see both reads through `postgres.query`
spans and `eshu_dp_postgres_query_duration_seconds`, with `db.operation` set to
`list_documentation_findings` or `list_documentation_facts`.

## Verification commands

```bash
cd go
go test ./internal/storage/postgres \
  -run 'TestDocumentation(ReadIndexFinalSchemaMatchesCurrentQueries|ReadIndexConcurrentMigrationsAreIsolated|RejectedAndLegacyIndexesAreNotReplayed)' \
  -count=1
go test ./internal/storage/postgres -count=1

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
identifiers, document identifiers, private image references, and raw plan
literals. Aggregate corpus counts, timings, and the synthetic result digest are
safe to publish.
