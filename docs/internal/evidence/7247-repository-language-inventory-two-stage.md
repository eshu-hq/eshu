# #7247 repository language-inventory aggregate

## Contract and cause

`GET /api/v0/repositories/language-inventory` reads global `content_files` language counts unless a scoped token supplies repository or scope grants. The repository named in the latency sweep labels its argument set; it does not filter this route. The handler requests `limit+1` rows and reports truncation. On QA PostgreSQL 18, the previous `COUNT(DISTINCT repo_id)` plan sorted all 145,050 file rows by normalized language and repository before computing 33 language groups. Its warm sort used 14,362 kB. A limit of 21 could not bound that scan or sort.

The replacement groups by normalized language and repository first, then counts those groups and sums their file counts by language. The grant predicate remains inside the first grouping, before any count or timestamp is computed. The query stays one statement and one content-index snapshot. `content_files.repo_id` is non-null, so one row per `(language, repo_id)` group yields the same repository count as `COUNT(DISTINCT repo_id)`. `SUM(file_count)::bigint` retains the old `COUNT(*)` scan type. Normalization, `MAX(indexed_at)`, ordering, offset, and limit are unchanged. PostgreSQL can use two hash aggregates rather than the large distinct-count sort. No schema, index, or writer path changes.

## Theory proof before code

A read-only QA same-statement differential compared old and proposed SQL, with `EXCEPT ALL` in both directions over language, repository count, file count, and last indexed time. The global result was 33 old rows, 33 new rows, zero differences, and an equal ordered top-21 page. A scoped grant for the recorded 21-file repository returned six rows on both sides, zero differences, and the same ordered page. The full SQL and commands are retained in the user-local `7247-language-inventory-two-stage-probe.md` packet.

On 145,050 files, warm `EXPLAIN (ANALYZE, BUFFERS, TIMING OFF)` A/B/B/A samples with all heap buffers hit were:

| Statement | First | Second | Plan |
| --- | ---: | ---: | --- |
| Previous | 316.383 ms | 288.387 ms | full scan, 14,362 kB sort, GroupAggregate |
| Two-stage candidate | 85.313 ms | 89.394 ms | full scan, 3,842-group hash, 33-group hash; no spill |

The two-stage hash used 793 kB and 32 kB for its aggregate stages. Both statements still scanned the table. These are SQL times, not endpoint times or deployed p95.

## Finished-query proof

The regression assertion failed before the SQL edit on the old `COUNT(DISTINCT repo_id)` path, then passed after the edit. `TestRepositoryLanguageInventoryTwoStageLive` ran the built `ContentReader` against a disposable PostgreSQL 18 database with the production bootstrap schema. It passed global count and tie order, null/empty language normalization, overlapping repository and scope grants without double count, first page, offset page, high offset, and empty grant. The disposable database was dropped by the test; the isolated local container and its anonymous volume were removed after proof.

A second read-only QA A/B/B/A probe extracted the previous and edited SQL directly from `content_reader_language_inventory.go` and ran them in one psql session. After the first cold/warming statements, all four measured plans reported 28,830 shared heap hits and no heap reads:

| Statement | First warm | Second warm |
| --- | ---: | ---: |
| Previous | 692.438 ms | 868.958 ms |
| Edited source SQL | 188.782 ms | 206.622 ms |

The first previous statement read 28,645 shared blocks and took 1,018.290 ms; the immediately following edited statement was warmer and took 148.560 ms. That pair is **not comparable**. The later warm series is internally comparable but ran at a different time and load from the theory proof, so the two series must not be combined into one speedup. The exact-source plan log is retained user-locally as `7247-language-inventory-built-sql-abba.log`.

## Limits and operator signal

Recorded same-image set-3 endpoint observations of 0.339 and 0.512 s disprove a fixed 0.9-1.0 s endpoint floor; a separate older-image sample was 0.880 s. Their load/cache conditions differ, so they are not a before/after endpoint comparison. The existing `postgres.query` span and `db.operation=repository_language_inventory` attribute still time the store call; this change adds no observability contract.

**Performance Evidence:** the read-only QA probes above show the bounded SQL improvement on the measured corpus. The route still scans every authorized file, so larger corpora or hash spill can change its cost. **No-Observability-Change:** the existing store span and operation name are unchanged. Owner deployment, exact recorded-argument cold and warm endpoint p95, and the other #7247 route tails are **NOT_CHECKED**. Keep #7247 open.
