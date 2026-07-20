# #5275 Documentation Query-Plan Evidence

## Decision

This work accepts the documentation-findings partial-index correction and
rejects a dedicated free-text documentation-facts search index. The findings
change fixes a stale predicate and preserves the result set. The search indexes
made reads faster, but the fastest candidate made a production-shaped write
4.61 times slower while the production-shaped 1.6-million-row search completed
in 1.122 seconds without it.

No free-text query or search index ships from this investigation.

## Acceptance disposition

| Issue acceptance item | Disposition | Evidence |
| --- | --- | --- |
| Capture `EXPLAIN (ANALYZE, BUFFERS)` for both reads | Proved | The findings and production-shaped search reads were measured on PostgreSQL 18.4. Sanitized plan nodes, rows, loops, buffers, and timings are below; the exact disposable shim is checked in beside this note. |
| Restore or replace the findings partial index | Implemented | Migration 064 creates the replacement index with the active `documentation_finding` predicate before migration 065 drops the stale legacy index. |
| Redesign free-text search around an indexable column | Superseded by measurement | Three index hypotheses were tested. The fastest plan added a 4.61x write cost, while the current production-shaped search stayed below the local interactive target. No search schema or query change lands. |
| Preserve exact results | Proved | The findings read returned the same 66 rows and digest before and after the correction. The accepted patch does not change free-text search behavior. |
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
- Search input: the exact five-field lowercase expression and seven
  documentation fact kinds used by `list_documentation_facts`.
- Write input: the production 17-column `fact_records` insert and conflict shape
  used by the streaming writer, applied to 10,000 documentation rows.
- Machine profile: Apple Silicon `arm64`, 18 logical CPUs, 128 GiB physical
  memory, APFS solid-state storage, macOS 26.5.1. The PostgreSQL container had
  no explicit CPU or memory limit.
- `absolute_target_applicable=false`: this is a named local proof profile, not
  an accepted cross-machine reference profile. The timing supports the local
  disposition and same-machine comparison; it is not a universal latency claim.

The production query and write shapes come from
`go/internal/query/documentation_read_model.go` and
`go/internal/storage/postgres/facts_streaming.go`. The proof changes only the
candidate index definition between measurements. The exact fixture, queries,
candidate DDL, and write probe are in
[`5275-documentation-query-plan-shim.sql`](5275-documentation-query-plan-shim.sql).
The verified shim SHA-256 is
`03203861e2cd9d40fcddc300eea9748d9fd10779903e29b93c9a556f360cd6a0`.

## Accepted findings-index result

| Measure | Stale predicate | Corrected predicate | Result |
| --- | ---: | ---: | --- |
| Execution time | 13.745 ms | 0.319 ms | 13.426 ms faster; 43.09x |
| Terminal rows | 66 | 66 | identical |
| Result digest | `b99ca6f196274d3a9b33333c686878a8` | `b99ca6f196274d3a9b33333c686878a8` | identical |
| Fact-read plan | `Parallel Seq Scan` | `Index Scan using fact_records_documentation_findings_read_idx` | intended plan delta |
| Actual rows / loops | 22 rows x 3 worker loops | 66 rows x 1 index loop | same 66 terminal rows |
| Fact-read buffers | 13,334 shared hits | 66 shared hits, 4 reads | 13,264 fewer shared block accesses |
| Whole-plan buffers | 13,400 shared hits | 198 shared hits, 4 reads | join included |
| Planning | 0.239 ms, 17 shared hits | 0.466 ms, 49 hits and 1 read | recorded, not optimized |

The stale partial predicate forced a sequential scan. The corrected predicate
made the current disclosure read eligible for its replacement partial index.
The result identity shows that the speedup did not change disclosure behavior.

## Current search baseline

| Measure | Result |
| --- | --- |
| Input | 1,600,000 synthetic documentation rows across the seven production kinds |
| Plan | `Parallel Seq Scan on fact_records` |
| Actual rows / loops | 533.33 rows x 3 worker loops; 532,800 rows filtered per loop |
| Buffers | 144,712 shared hits at the scan; 144,818 for the whole plan |
| Planning / execution | 0.162 ms / 1,121.505 ms |
| Terminal result | 51 rows; digest `2c5a286517da306b3461ba075696a3fa` |

The 1.122-second result is below the 2-3 second target on this named local
profile. Because this machine is not the accepted cross-machine reference,
that does not establish a universal SLO. It does establish that a new write-heavy
index is not justified by the current local path.

## Rejected search-index hypotheses

| Candidate | Build result | Read result | Disposition |
| --- | --- | --- | --- |
| Broad expression GIN with `gin_trgm_ops` | 16.810 s, 315 MB | `Bitmap Index Scan`, 1,600 rows x 1 loop, 22 index hits; whole plan 1,622 hits; 5.047 ms execution | Fastest read, but its repeated write cost was 4.61x baseline. |
| GiST with `gist_trgm_ops(siglen=64)` | Failed: one index row required 11,912 bytes; PostgreSQL maximum was 8,191 bytes | No valid plan | Rejected for this expression and fixture shape; this is not a general rejection of GiST. |
| Scoped multicolumn GIN using `btree_gin` | 16.925 s, 320 MB | `Bitmap Index Scan`, 1,600 rows x 1 loop, 476 index hits; whole plan 2,076 hits; 8.619 ms execution | Slower and larger than the broad GIN; repeated write cost was 4.44x baseline. |

All candidate indexes and the candidate `btree_gin` extension were removed
after the proof. None is part of the accepted schema.

## Write-tax proof

Both valid search candidates were measured against the production insert and
conflict shape on the same 10,000-row workload. Each warmup and measured insert
ran in a transaction that rolled back, so the table state stayed comparable.

| Run | Execution time | Shared buffers | Dirtied / written | Multiple of baseline |
| --- | ---: | ---: | ---: | ---: |
| No candidate | 91.946 ms | 122,348 hits | 1,174 / 1,174 | 1.00x |
| Broad expression GIN | 423.593 ms | 252,657 hits | 1,250 / 1,250 | 4.61x |
| Scoped multicolumn GIN | 407.802 ms | 260,514 hits | 1,297 / 1,297 | 4.44x |

The fastest read candidate added 331.647 ms to the repeated 10,000-row insert.
The write comparison is a bounded same-machine shim, not an end-to-end ingestion
duration, so no corpus-wide time is extrapolated from it.

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

Performance Evidence: the accepted findings index changed a 13.745 ms parallel
sequential scan into a 0.319 ms index scan with the same 66 rows and digest. The
production-shaped 1.6-million-row search completed in 1,121.505 ms. The fastest
rejected search index reduced it to 5.047 ms but increased the repeated
10,000-row production-shaped write from 91.946 ms to 423.593 ms, so it did not
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
proof_db=eshu_5275_<run-id>
createdb "$proof_db"
psql -X -d "$proof_db" \
  -f docs/internal/evidence/5275-documentation-query-plan-shim.sql
dropdb "$proof_db"

cd go
go test ./internal/storage/postgres \
  -run 'TestDocumentation(ReadIndexFinalSchemaMatchesCurrentQueries|ReadIndexContractRejectsEveryMissingKey|ReadIndexConcurrentMigrationsAreIsolated|RejectedAndLegacyIndexesAreNotReplayed)' \
  -count=1
go test ./internal/storage/postgres -count=1
go test ./internal/testutil/postgresproof -count=1
golangci-lint run ./internal/storage/postgres ./internal/testutil/postgresproof

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
refuses every other database. The verified run cleaned up its database; a
catalog check returned zero remaining `eshu_5275_%` databases.

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
