# #7365 entity write replay: secret-lines triggers do not explain the gap

## Problem

The matched 984-repository Neo4j/PostgreSQL full-corpus runs for #7206 showed
`content_write` up from 3,768.989 to 5,452.122 cumulative worker-seconds
(+44.7%), with `upsert_entities` up 957.992 cumulative worker-seconds across
610 bootstrap repositories with identical entity row and batch counts. In the
10–15 minute window, `>5s` entity INSERT plans went from 5 calls / 35.2 seconds
to 68 calls / 649.6 seconds. The candidate change (#7206, migration 138) adds
statement-level secret-lines triggers on `content_files`, so the suspect was
trigger cost spilling into the entity path.

## Theory proof (cheapest shim first)

Before any production change, a throwaway A-B-B-A shim replayed the real
`ContentWriter.Write` path (100 files, 10,000 entities, 300-row batches,
pool 10, entity concurrency 4) on two identically seeded disposable
PostgreSQL 16.15 databases: A kept the migration-138 triggers and side table,
B dropped them. Both kept the full GIN index schema. Each round wrote the same
corpus after path-scoped deletes and a CHECKPOINT, alternating A-B-B-A /
B-A-A-B. Captured per write: wall, `upsert_entities` / `upsert_files` stage
durations, WAL bytes, `pg_statio` heap/index reads, `pg_stat_bgwriter`
checkpoints, and 100 ms wait-event samples; plus one rolled-back
`EXPLAIN (ANALYZE, BUFFERS)` 300-row entity batch per database.

Host: `shared_buffers=128MB`, `max_connections=100`, `synchronous_commit=on`,
`max_wal_size=1GB`, `checkpoint_completion_target=0.9`.

| Metric (median of 4+4) | Run 1 | Run 3 |
| --- | --- | --- |
| wall A vs B | 0.653s vs 0.632s (-3.1%) | 0.999s vs 0.978s (-2.1%) |
| `upsert_entities` A vs B | 0.430s vs 0.369s (-14.2%) | 0.508s vs 0.507s (-0.2%) |
| `upsert_files` A vs B | 0.166s vs 0.195s (B slower) | — |
| WAL A vs B | -2.1% | +3.9% (noise band) |
| entity heap-block reads | 0 vs 0 | 0 vs 0 |

Run 1's -14.2% entity-stage delta sits inside its own A/A run-order spread
(A range 0.077s vs the 0.061s A/B median gap) and is confounded by cache
warming and an autovacuum delay sampled during B's last write; run 3 shows
the stages identical to 1 ms. The batch plans are the same shape:
`Insert on content_entities`, arbiter `content_entities_pkey`, 300 tuples,
A 5.589 ms / 9,714 shared hits vs B 5.319 ms / 9,509 shared hits. Wait samples
are WAL/extend/CPU dominated on both sides with no lock-wait signature.

## Verdict

The secret-lines trigger factor does not reproduce the matched-run entity
gap at this scale: wall, WAL, block I/O, and plan shape are equal within
run-order spread. The prior isolated replays agree (10,000 files ± trigger:
13.0s vs 13.1s; 10,000 entities ± GIN: 0.21s vs 0.83s measures index cost,
not the matched-run difference). No trigger or index rewrite is justified on
this evidence, so this change ships instrumentation only and no production
behavior change.

## Limits

- 10k-entity synthetic corpus on one idle host, not the 984-repository
  corpus with concurrent projector lanes, so GIN pending-list growth,
  checkpoint pressure under parallel writers, autovacuum debt, and table
  bloat at full-corpus size are all untested.
- Base seed is small; the measured writes land on warm, mostly cached pages
  (entity heap reads ≈ 0).
- Entities carry no fingerprint metadata, so `upsert_fingerprints` cost is
  understated equally on both sides.

## Next diagnostic step (before another 984-repository run)

Run the committed replay at the prescribed scale on the perf host:

```bash
ESHU_CONTENT_ENTITIES_BENCH=1 \
ESHU_CONTENT_ENTITIES_BENCH_FILES=500 \
ESHU_CONTENT_ENTITIES_BENCH_ENTS=100 \
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN=<admin-dsn> \
ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
go test ./internal/storage/postgres/ -run TestContentEntitiesWriteCostBenchLive -v -count=1
```

in `go/internal/storage/postgres/content_entities_7365_writebench_live_test.go`.
If the gap repeats beyond the A/A spread there, perturb one proven factor at
a time with `ESHU_CONTENT_ENTITIES_BENCH_NO_TRIGGERS=1`, then
`ESHU_CONTENT_ENTITIES_BENCH_NO_SOURCE_GIN=1`. If it still does not reproduce,
the suspect moves off the write statement itself: compare full-corpus
`pg_stat_io`, GIN pending-list pages, checkpoint timings, autovacuum runs,
and per-index bloat between the preserved candidate and baseline databases,
and re-check whether the matched 610 repositories were matched on rows but
not on bytes (wider `source_cache` values inflate GIN maintenance without
changing row counts).

## Change

- New scheduled live bench
  `go/internal/storage/postgres/content_entities_7365_writebench_live_test.go`
  (env-gated, ledger-classified, asserts parity only) plus unit-pinned
  helpers in `content_entities_7365_writebench_test.go`.
- Ledger row in `specs/live-tests.d/live-tests-04.yaml` beside the
  secret-lines file bench.

No-Regression Evidence: the bench changes no production statement, index,
batch size, worker, lease, or concurrency knob; `go test
./internal/storage/postgres -run 'TestEntityBench' -count=1` pins the corpus,
median, override, and stage-capture contract, and the live bench was exercised
end to end against disposable PostgreSQL 16.15 at 100 files / 10,000 entities
(parity asserts green, medians logged, rolled-back batch plans captured).

No-Observability-Change: no new metric, span, log key, route, worker, lease,
or runtime knob; per-stage timings reuse the existing `logStage` shape the
bench captures through a test-only handler.
