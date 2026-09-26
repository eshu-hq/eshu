# #7033 — unscoped topic relative-path index

## Decision and scope

`investigate_code_topic` can probe `content_files.relative_path` without a
repository constraint. The primary key starts with `repo_id`, so it cannot
bound that substring probe. Migration 125 adds
`content_files_relative_path_trgm_idx` as a `gin_trgm_ops` index. It runs with
`CREATE INDEX CONCURRENTLY` for normal upgrades. Cold bootstrap records its
deferred no-op variant and `EnsureContentSearchIndexes` builds the same index
only after the write-heavy projection drain.

Migration 126 extends `eshu_content_substring_indexes_valid()` to require the
exact path-index shape. An existing wrong, partial, invalid, or absent
same-name index therefore prevents the ready state and guarded reads until the
finalizer produces the exact index.

## Performance Evidence

Performance Evidence: the candidate was measured before implementation with
the same local corpus, backend, storage state, and interleaved request order.
The full query used the same unscoped 16-term topic request in every sample;
its result digest was identical across old and new plans and contained 5,235
rows.

| Measurement | No path GIN median | Path GIN median | Result |
| --- | ---: | ---: | --- |
| Full 16-term unscoped topic query | 72,172.144 ms | 137.006 ms | Same 5,235-row digest |
| Isolated `relative_path` probe | 293.461 ms | 3.178 ms | Candidate predicate uses GIN |
| Concurrent index build, 145,000 rows | N/A | 558 ms median, 12 MiB | One-time upgrade cost |
| Cold ingest | Baseline | +24.9% when built during ingest | Deferred by design |

The cold-ingest cost is why the migration is not built during deferred
bootstrap. These are local Postgres measurements, not an ops-qa endpoint
claim. The live ops-qa endpoint baseline is 21.463 s; live after measurement
is **NOT_CHECKED** and must be interleaved against the same corpus and storage
state after the approved rollout. A built API/MCP endpoint proof is likewise
pending separately and is not substituted by the SQL result above.

## Lifecycle proof

Disposable PostgreSQL 18 live tests exercise the production tracked bootstrap
entry point rather than a direct `ApplyDefinitions` call:

- populated pre-125 ready state plus indexed content, tracked 125 concurrent
  migration, tracked 126 lifecycle migration, then a guarded unscoped
  `relative_path ILIKE` read;
- wrong btree and partial GIN same-name path indexes while the other three
  lifecycle indexes are exact; 126 moves state to `not_built`, finalization
  fails closed, then removing the malformed index lets the finalizer recover to
  `ready` with the exact GIN;
- invalid interrupted concurrent-index cleanup, concurrent production-entry
  migration calls, and reapply behavior.

## Observability Evidence

No-Observability-Change: this adds no metric, span, log key, route, worker,
lease, or runtime knob. Normal migration uses the existing
`bootstrap.postgres.migration.concurrent_index_build` start/finish events;
deferred finalization continues to use existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`. Operators can inspect the exact
catalog shape, `content_substring_index_state`, and those existing migration
and finalization signals when a guarded read remains unavailable.
