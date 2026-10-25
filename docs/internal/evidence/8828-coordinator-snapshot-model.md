# #8828: coordinator snapshot served from a stored summary row

## What changed

The reducer's status summary writer now writes a third row, `coordinator_snapshot`,
after `active_work_summary` and `terraform_state`. The row holds the whole result
of `readCoordinatorSnapshot` (`go/internal/storage/postgres/workflow_status.go`):
collector instances, workflow run, work-item and completeness status counts,
claims, collector backpressure with failure-class counts, the recent-failure
counts, and the collector terminal failures. The status store and the runtime
`/metrics` scrape read that row behind the existing flags
(`ESHU_STATUS_SUMMARY_READ_ENABLED`, default `false`;
`ESHU_STATUS_SUMMARY_STALE_AFTER`, default `33s`). No flag, environment
variable, or migration was added: `status_summary_snapshots` is keyed by
`model_key`.

Why: every status read and every scrape of every hosted runtime (about 30
binaries at 30 s) ran statements whose cost is linear in `workflow_work_items`.
The #7009 model removed only the active-work read.

## Design

- Third companion in `Runner.Companions`, last, so its compute (which grows with
  the table) never delays the other two rows. Own transaction, own database
  clock, own `as_of` guard, own digest, same advisory lock key. Any number of
  reducer replicas may run it: the try-lock lets one compute per tick, the
  guarded upsert (`existing.as_of < EXCLUDED.as_of`) keeps `as_of` monotonic.
- Codec: `go/internal/storage/postgres/workflow/snapshot` (strict decode, a
  round trip of every field including the terminal failures the status JSON
  excludes). The digest covers all nine statement texts, the two window rules,
  and the encoding tag, so a rolling upgrade falls back instead of mixing.
- Reader: its own `ModelReader`, so its fallback is independent of the other
  models. A status route that cannot serve the row (missing, not installed,
  version, row_count, stale, decode) runs the live statements for the whole
  section and reports the reason in `coordinator_source` and in
  `eshu_dp_status_summary_read_total{model_key="coordinator_snapshot"}`. A
  database error fails the read. A scrape (`StoredActiveWorkOnly`) never runs
  them: it serves the newest decodable row or no coordinator section, and
  exports `eshu_runtime_status_summary_stale` and
  `eshu_runtime_status_summary_age_seconds` under
  `model_key="coordinator_snapshot"`.
- Freshness: counts and terminal failures are true at `as_of`; the oldest
  pending, retry and claim ages grow by the row's age and the next-retry delay
  shrinks by it (`snapshot.AdvanceAges`). `age_seconds` bounds the whole
  section.
- Retention (#8829): the stored counts are counts of retained rows. The model
  reads one row whatever the table size, so the retention-off case
  (`ESHU_WORKFLOW_COORDINATOR_WORK_ITEM_RETENTION_AGE=0`) is covered; only the
  writer pass grows with the table.

## Measurement

Fixture: scratch PostgreSQL 18.6, the full bootstrap schema (251 migrations,
rebased on `origin/main` e39c3bb949); 600,000 `workflow_work_items` (498,000 completed, 60,000
failed_terminal, 18,000 pending, 12,000 expired, 6,000 claimed, 6,000
failed_retryable, 5 collector instances, 20,000 scopes), 6,000 active claims,
5,000 runs, `ANALYZE`d, retention off, default `shared_buffers` (the table is 490
MB with its indexes). `EXPLAIN (ANALYZE, BUFFERS)` of each prepared statement, third consecutive
run, warm cache.

Performance Evidence: before, the coordinator statements of one snapshot cost
156.9 ms and 62,529 shared buffers; after, the same snapshot reads one
primary-key row, 0.005 ms and 1 buffer, so the per-snapshot work no longer depends
on `workflow_work_items`.

| Statement | Before exec (ms) | Before buffers | Dominant node |
| --- | ---: | ---: | --- |
| work-item status counts | 47.2 | 13,941 | Parallel Seq Scan (2 workers) |
| claim snapshot (oldest pending) | 7.3 | 6,348 | Bitmap Heap Scan, 18,000 rows |
| collector backpressure | 69.0 | 28,161 | Parallel Seq Scan twice (2 workers) |
| backpressure failure classes | 32.0 | 13,941 | Parallel Seq Scan (2 workers) |
| run status counts | 0.7 | 51 | Seq Scan, 5,000 rows |
| instances, completeness, recent failures, terminal failures | 0.7 | 87 | index seeks and small scans |
| **Total** | **156.9** | **62,529** | |

| Read after | Exec (ms) | Buffers |
| --- | ---: | ---: |
| summary clock | 0.002 | 0 |
| keyed row (22 entries, 1,198 byte payload) | 0.003 | 1 |

The scan nodes launch two parallel workers, so the before cost in CPU time is
about three times the wall figure. An independent reviewer re-seeded a similar
table (different scope and timestamp shape) and measured 129 ms and 49,832
buffers before and 0.002 ms and 1 buffer after. The writer pass that replaces
them took 135 to 146 ms at the same size (three passes, `ReadCoordinatorSummaryEntries` through
the production runner), once per 10 s tick on the primary regardless of how many
processes read status; before, about 30 scrapes per 30 s each paid the 157 ms.
The statements and their plans are unchanged (the digest pins the texts), so no
index was added.

Concurrency: `TestCoordinatorWritersBesideQueueTrafficLive` runs four writer
replicas against the same ticks for six rounds beside four claim-style
`FOR UPDATE SKIP LOCKED` workers on `workflow_work_items`; no replica errored,
`as_of` never went backwards, exactly one row remained, claims were not blocked,
and the settled row equals the live read.

Accuracy: `TestCoordinatorModelServedEqualToLiveLive` recomputes the live read at
the stored row's `as_of` in one REPEATABLE READ snapshot and requires the
entries to be equal tuple for tuple, at an empty database and with every status,
claims, instances, completeness, a dead letter, and an unacknowledged plus an
acknowledged terminal failure; the production reader then serves the row with
counts equal to the live read.

Observability Evidence: no new metric. The existing
`eshu_dp_status_summary_writer_*`, `eshu_dp_status_summary_read_total`,
`eshu_dp_status_summary_read_age_seconds`, `eshu_dp_status_summary_scrape_total`,
`eshu_runtime_status_summary_stale` and `eshu_runtime_status_summary_age_seconds`
gained `model_key="coordinator_snapshot"` series, and
`eshu_dp_status_snapshot_read_duration_seconds` gained `read="coordinator_model"`.
The status payload carries `coordinator_source` with `age_seconds`. A stalled
writer shows as the stale gauge at `1` for the model (the existing
`EshuHostedStatusSummaryStale` alert groups by `model_key`), a growing
`age_seconds`, and `live_fallback` reasons `stale` or `missing` on status reads.

## Not done

- Routes that derive from the coordinator section without rendering it
  (collector runtime health and promotion proofs read the terminal failures) take
  the same stored section with the same freshness bound, but do not carry
  `coordinator_source`; only the routes listed in the HTTP reference do.
- Scaled (remote) runs; the measurement is one scratch PostgreSQL.
