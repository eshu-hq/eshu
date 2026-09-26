# #7033 — indexed unscoped topic probes

## Decision and scope

`investigate_code_topic` can probe `content_files.relative_path` without a
repository constraint. The primary key starts with `repo_id`, so it cannot
bound that substring probe. Migration 126 adds
`content_files_relative_path_trgm_idx` as a `gin_trgm_ops` index. It runs with
`CREATE INDEX CONCURRENTLY` for normal upgrades. Cold bootstrap records its
deferred no-op variant and `EnsureContentSearchIndexes` builds the same index
only after the write-heavy projection drain.

A populated live upgrade must use migration 126's concurrent build, then
validate the catalog before migration 127 publishes readiness.
`EnsureContentSearchIndexes` is a non-concurrent, transactional finalizer for
deferred bootstrap, not a live substitute for migration 126.

Migration 127 extends `eshu_content_substring_indexes_valid()` to require the
exact path-index shape. An existing wrong, partial, invalid, or absent
same-name index therefore prevents the ready state and guarded reads until the
finalizer produces the exact index.

## Performance Evidence

Performance Evidence: all timings below are local PostgreSQL 18.6 synthetic
fixture measurements, not an ops-qa endpoint claim. Paired runs use the same
145,000 files, 2.6 million entities, 838 repositories, backend, and
interleaved request order. Index-only comparisons necessarily differ by the
presence of the path index; the old/candidate query comparison uses one
indexed database and unchanged table contents.

| Measurement | Before median | After median | Result |
| --- | ---: | ---: | --- |
| Selective 16-term SQL probe | 72,172.144 ms | 137.006 ms | Same 5,235-row digest; isolates the new path GIN |
| Isolated `relative_path` probe | 293.461 ms | 3.178 ms | Path predicate uses GIN |
| Local production `CodeHandler` HTTP overlay, path GIN only | 48,295 ms | 45,763 ms | Same ordered 25 rows; insufficient for the under-1-second budget |
| Ordered static entity branches, with path GIN | 1,029.130 ms | 152.169 ms | Five interleaved pairs; 26/26 ordered rows equal, `EXCEPT` 0/0, both pools capped |
| Built `CodeHandler` HTTP, capped, path GIN in both variants | 80.000 ms | 161.730 ms | Five interleaved pairs; old has two ~35 s tails and five different row digests; candidate is stable and under 1 s |
| Concurrent index build, 145,000 rows | N/A | 558 ms median, 12 MiB | One-time upgrade cost |
| Cold ingest | Baseline | +24.9% when built during ingest | Deferred by design |

The first two rows prove only the selective path-index hypothesis. The
production-shaped measurement disproved path GIN alone as a complete endpoint
fix: the remaining cost was one broad `content_entities` scan per term. The
ordered static entity branches preserve the entity-name-or-source-cache
predicate, scope/readiness/language filters before each cap, and the final
deterministic ordering while allowing the existing entity substring indexes to
select candidates. The cold-ingest cost is why the migration is not built
during deferred bootstrap.

The built-handler comparison used the same indexed PostgreSQL 18.6 fixture
(145,000 files, 2.6 million entities, 838 repositories) and the same 16 terms,
with 500 planted entity matches per term against a 250-row cap. Calls alternated
old/candidate in the temporal order B,C,C,B,B,C,C,B,B,C, each in a fresh
process and read-only database connection. Baseline samples in that order were
78.771, 35,562.428, 80.000, 35,429.045, and 79.298 ms; candidate samples
were 163.396, 161.548, 165.472, 161.730, and 159.776 ms. All returned HTTP
200, 25 rows, and both truncation markers. The candidate's row digest was
identical in all five calls; the old unordered candidate pool changed on every
call. This is **not a median speedup** on the capped fixture. The candidate
removes its observed over-budget tail and makes selection deterministic.

The old query used the same `Seq Scan` plan in a 33,346.235 ms run and a
79.438 ms run. Its entity scans rejected about 2.433 million versus 3,615
rows per term, with 600,645 versus zero shared-buffer reads. A read-only
`synchronize_seqscans=off` probe took 35,507.078 ms and rejected about 2.596
million rows per term. PostgreSQL permits synchronized scans to start in the
middle of a table, changing which rows an unordered `LIMIT` finds first
([PostgreSQL 18 documentation](https://www.postgresql.org/docs/18/runtime-config-compatible.html)).
The ordered LATERAL reference and static candidate returned the same 26
ordered SQL rows and pool flag. The built candidate handler's 25 returned
evidence identities matched the reference's first 25, with the 26th overflow
row present. These are local behavior and latency proofs, not ops-qa results.

The live ops-qa endpoint baseline is 21.463 s. Live after measurement is
**NOT_CHECKED**. An index-absent/index-present schema rollout cannot be
interleaved on one live database without reversing the schema; it must instead
record ordered pre/post endpoint samples with the exact corpus, backend,
commit, and storage state. The old/candidate query-shape comparison can be
interleaved on the same indexed storage state. A built HTTP after measurement
is also required. The local measurements do not establish ops-qa latency or
its under-1-second budget.

## Lifecycle proof

Disposable PostgreSQL 18 live tests exercise the production tracked bootstrap
entry point rather than a direct `ApplyDefinitions` call:

- populated pre-126 ready state plus indexed content, tracked 126 concurrent
  migration, tracked 127 lifecycle migration, then a guarded unscoped
  `relative_path ILIKE` read;
- wrong btree and partial GIN same-name path indexes while the other three
  lifecycle indexes are exact; 127 moves state to `not_built`, finalization
  fails closed, then removing the malformed index lets the finalizer recover to
  `ready` with the exact GIN;
- invalid interrupted concurrent-index cleanup, concurrent production-entry
  migration calls, and reapply behavior.

With PostgreSQL 18.6 and an explicitly disposable administrative database,
the following command ran all three named live tests and passed twice (7.615 s
and 7.513 s; both exit 0):

```bash
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='<disposable admin DSN>' \
  ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
  CC=clang CGO_CFLAGS=-std=gnu17 \
  go test ./internal/storage/postgres \
  -run '^TestContentFilesRelativePathIndex.*Live$' -count=1
```

The test container and its volume were removed afterward. This local receipt
does not authorize using the deferred finalizer on a populated live database.

The opt-in `issue7033_rollout` test runner selects only the embedded, checksum-
matched migrations 126 and 127. It verifies the target system identifier,
database, schema, primary role, prerequisite receipts and three exact existing
GIN indexes in a read-only preflight. It applies tracked migration 126,
checks the exact new index, then applies tracked migration 127 and checks the
four-index readiness contract. On a disposable PostgreSQL 18.6 database, the
runner applied those two migrations and retried idempotently. Wrong target,
incomplete index state, and a mismatched prerequisite ledger receipt all
failed before target DDL. The separate live regression also proved that
migration 125 can remain unapplied during this scoped rollout and later be
applied by normal bootstrap without reapplying 126 or 127.

An opt-in `issue7033_canary_startup` test used disposable PostgreSQL and
Neo4j to prove a Neo4j-backed API starts with both backfill markers complete,
disabled admin bootstrap and OIDC refresh, and a read-only PostgreSQL pool.
Health, readiness, and the code-topic route returned HTTP 200; the attempted
best-effort startup audit did not persist an event. This is local startup
proof, not permission to create an ops-qa canary or a live after measurement.

## Observability Evidence

No-Observability-Change: this adds no metric, span, log key, route, worker,
lease, or runtime knob. Normal migration uses the existing
`bootstrap.postgres.migration.concurrent_index_build` start/finish events;
deferred finalization continues to use existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`. Operators can inspect the exact
catalog shape, `content_substring_index_state`, and those existing migration
and finalization signals when a guarded read remains unavailable.
