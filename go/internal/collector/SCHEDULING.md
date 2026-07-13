# Collector Repository And Parse Scheduling

This file carries the collector's giant-repo scheduling design, the
dedicated large-lane scheduler, and byte-balanced parse-partitioning —
the full-corpus evidence trail for issues #3711 and #3839. It is split out
of the package README because the design narrative and its measured
evidence are too detailed for the README's exported-surface overview.

## Byte-balanced parse partitioning

- No-Regression Evidence: native parse subtree partitioning preserves snapshot
  composition. `go test ./internal/collector -run
  'Test(BuildParseSubtreePartitionsSplitsStableSubtrees|PartitionedConcurrentParseMatchesSequentialComposition|NativeRepositorySnapshotterLogsSnapshotStageTimings)'
  -count=1` proves stable partition planning, deterministic sequential versus
  concurrent output, and parse-stage partition logging.
- Performance Evidence: focused local parse benchmark command:
  `go test ./internal/collector -run '^$' -bench BenchmarkPartitionedParseLargeMonorepo -benchtime=1x -benchmem -count=1`.
  On 2026-06-18 on Apple M4 Pro, the same 96-file synthetic large monorepo
  fixture measured `workers_1` at 12.884 ms/op, 2.32 MB/op, 37,181 allocs/op
  and `workers_4` at 7.232 ms/op, 2.39 MB/op, 37,463 allocs/op.
- Observability Evidence: the existing `collector snapshot stage completed`
  parse log now includes `parse_partition_count` alongside `parse_workers`,
  file counts, skipped counts, and `language_parse_summary`. Existing
  `eshu_dp_file_parse_duration_seconds` and `eshu_dp_files_parsed_total` metrics
  continue to report per-file parse timing and success/skipped counts without
  adding high-cardinality path or partition labels.

### Giant-repo collection scheduling (issue #3711)

Full-corpus measurement (896 repos, remote Compose run) showed collection
wall-time dominated by a giant-repo tail: per-stage totals were parse ~1586 s,
materialize ~449 s, and pre-scan ~350 s (parallel), with a single 16,659-file
repository's parse taking ~1012 s (~0.49 s/file, ~10x the normal per-file cost).
Parse is already 8-way parallel and count-balanced, yet repositories were
dispatched in discovery order, so the giants clustered at the end and serialized
the tail.

This change orders repositories largest-first in `resolveRepositories` so the
heaviest repos start before the small-repo bulk and overlap with it instead of
serializing at the end. The file count walked for ordering is reused for the
existing small/large lane classification, so no second tree walk is added.

- Performance Evidence (measured, full-corpus 895-repository run, PostgreSQL 18 +
  NornicDB): the giant repos are enqueued first (verified by the order of the
  `large repository queued` log) and parsed concurrently with the small-repo bulk
  rather than serializing at the tail. The ordering guarantees enqueue order, not
  start order: the worker loop prefers the small lane, so the largest-first
  *start* overlap is best-effort and scheduler/backpressure-dependent (in this run
  the large-repo semaphore waits were ~90-100 s, not the full small-bulk, so the
  overlap held). Making early giant start guaranteed regardless of classifier
  timing is tracked as a follow-up (issue #3839); the byte-balanced parse win
  below is independent of scheduling and already guaranteed. The clean, attributable
  metric is the parse stage (the collection work this change targets, unconfounded
  by the downstream projection consumer): the worst single-repository parse
  dropped from ~1012 s to ~238 s and the total parse stage from ~1586 s to ~675 s
  versus the pre-change run, combining this ordering change with the byte-balanced
  partitioning below. Two giant repositories parse at a time under the existing
  large-repo semaphore (`large_repo_max_concurrent`, default 2), so giants 3+ wait
  ~90-100 s for a slot — a pre-existing cap, not introduced here. Caveat: the
  end-to-end collector-stream wall-time is pipelined against projection
  backpressure and so is dominated by the projection consumer's wall-time (which
  varied ~17% run-to-run from NornicDB write timing, a phase this change does not
  touch); the per-stage parse durations above are the isolated collection metric.
  Focused proof of the ordering and the preserved repo set: `go test
  ./internal/collector -run
  'Test(ResolveRepositoriesSortsLargestFirst|ResolveRepositoriesStableForEqualCounts|IsLargeRepositoryReturnsExactCount)'
  -count=1`.
- Observability Evidence: the per-repo `eshu_dp_repo_snapshot_duration_seconds`
  histogram now carries the bounded `repo_size_tier` (`small`/`large`)
  dimension so an operator can slice giant-repo cost by size without the
  unbounded cardinality of a raw file-count label. The exact per-repo
  `file_count` remains on the existing `collector snapshot completed` structured
  log. No new instrument or pipeline stage is introduced; `repo_size_tier` is an
  already-registered telemetry dimension.

### Dedicated large-lane scheduler — design (issue #3839)

The #3711 largest-first ordering guaranteed enqueue order but not start order:
if the classifier fully drained smallCh before any worker ran, a small-preferring
worker could grab all small repos first and delay giant start by the full
small-repo bulk. Issue #3839 hardens this to a scheduling guarantee.

The first `min(LargeRepoMaxConcurrent, workers)` workers (minus one reserved
small-lane worker when the cap meets the worker count) run `runLargePreferring`:
each reserves a semaphore slot and then block-selects the large lane, so a giant
starts the instant it is enqueued regardless of how many smalls are buffered in
smallCh. The remaining workers run `runSmallPreferring` (the original loop),
ensuring small repos drain even when largeCh is empty or slow.

- Performance Evidence: the #3839 dedicated-large-lane scheduler makes the
  largest-first early-giant-start **guaranteed** rather than
  scheduler-timing-dependent. The first `min(LargeRepoMaxConcurrent, workers)`
  workers (minus one reserved small-lane worker when the cap meets the worker
  count) run a large-preferring loop that reserves a semaphore slot then
  block-prefers the large lane, so a giant starts the instant it is enqueued.
  No new wall-time claim beyond #3711's measured full-corpus giant-repo parse
  result (1012 s → 238 s); this change hardens the overlap. Proven
  deterministically by `TestSchedulerRolePrefersGiantOverFrontLoadedSmallLane`
  (giant-first vs small-first, scheduler-level, free of the startup race) and
  the cap/leak/error/small-reserve invariant tests in
  `git_source_scheduler_invariant_test.go`. Focused proof: `go test
  ./internal/collector -run
  'Test(SchedulerRolePrefersGiantOverFrontLoadedSmallLane|SchedulerSemaphoreCapNotExceeded|SchedulerNoSemaphoreLeakOnCtxCancel|SchedulerSemaphoreReleasedOnSnapshotError|SchedulerSmallWorkerReservedWhenCapEqualsWorkers)'
  -count=1 -race`.
- No-Observability-Change: no new metric or span is added. The existing
  `eshu_dp_large_repo_semaphore_wait_seconds` histogram and
  `eshu_dp_large_repo_classifications_total` counter (emitted via
  `git_source_stream.go` and `git_source_scheduler.go`) let an operator see
  giant start order and concurrency; the `large repo semaphore acquired` /
  `large repo semaphore released` structured logs record per-giant wait and hold
  duration. These existing signals cover the change surface without new
  instruments.

### Size-aware parse-partition balancing (issue #3711)

Within a single repository, parse work was partitioned by file *count*: a
subtree was split into chunks of `ceil(fileCount/workers)` files. That balances
file count, not parse cost, so a subtree with a few huge files pinned one parse
worker while others idled. In the measured full-corpus run a single
16,659-file repository's parse took ~1012 s (~0.49 s/file, ~10x normal),
consistent with a few heavy files/subtrees dominating its partitions.

This change balances partitions by total on-disk bytes (`os.Stat` size, summed)
instead of file count. Subtrees lighter than one worker's byte target stay
whole; heavier subtrees are split into byte-balanced chunks so their heavy files
spread across workers. Stat failures fall back to a default weight so no file is
dropped. The partitions cover the exact same file set (same indexes, no drop, no
duplicate), so the parse result is byte-identical — only the worker distribution
changes.

- Performance Evidence (measured, full-corpus 895-repository run): baseline is the
  count-based partitioning where one giant repository's parse ran ~1012 s with
  heavy files clustered in a few partitions; with byte-balanced partitioning the
  same repository's worst parse stage dropped to ~238 s and the total parse stage
  across the corpus fell from ~1586 s to ~675 s, within the fixed
  `ESHU_PARSE_WORKERS` budget. The ~238 s residual is the giant repository's
  irreducible parse — a partition cannot split below a single file, so one
  multi-megabyte authored file still parses on one worker (files already skipped
  as minified/generated/vendored under #3679 are excluded; these are kept,
  authored files). `materialize` (~458 s total, serial, untouched here) is now
  co-equal with the parse residual and is the next collection target. Correctness
  and balance proof:
  `go test ./internal/collector -run
  'Test(BuildParseSubtreePartitionsCoversExactFileSet|BuildParseSubtreePartitionsSpreadsHeavyFiles|BuildParseSubtreePartitionsEdgeCases|BuildParseSubtreePartitionsSplitsStableSubtrees|PartitionedConcurrentParseMatchesSequentialComposition)'
  -count=1` proves the union of partitions equals the input file set exactly,
  heavy files spread instead of clustering, the empty/single/all-same-size edge
  cases hold, and the concurrent parse output still matches the sequential
  composition byte-for-byte.
- No-Observability-Change: parse-partition balancing changes only how files are
  grouped across the existing parse workers. The existing
  `eshu_dp_file_parse_duration_seconds`, `eshu_dp_files_parsed_total`, and the
  `collector snapshot stage completed` parse log (`parse_workers`,
  `parse_partition_count`) continue to report parse timing and partition shape;
  no metric, span, log field, or label is added or removed.

### Dedicated large-lane scheduler — small-repo starvation fix (issue #3839)

The `### Giant-repo collection scheduling (issue #3711)` section above noted that
largest-first ordering guarantees enqueue order but not start order: when the
classifier fills the small lane before any worker runs, a worker loop that prefers
the small lane can drain all small repos before it sees the large lane, making the
giant-repo overlap scheduler-timing-dependent.

This change adds a dedicated large-lane scheduler in `git_source_scheduler.go`.
`min(LargeRepoMaxConcurrent, workers)` workers are flagged as large-preferring and
block on `largeCh` in the first select arm, so a giant repo starts the instant it
is enqueued regardless of how many small repos are queued ahead.

P2 fix (small-repo starvation when `ESHU_SNAPSHOT_WORKERS <= ESHU_LARGE_REPO_MAX_CONCURRENT`):
when `largePreferring >= workers && workers > 1`, all workers become large-preferring
and starve `smallCh` until `largeCh` closes. The fix in `git_source_stream.go` clamps
`largePreferring` to `workers - 1` so at least one small-preferring worker remains.
When `workers == 1` the lone worker takes the small-preferring path and still
opportunistically drains large repos via its select fallback.

- Performance Evidence: The #3839 dedicated-large-lane scheduler makes early giant
  start deterministic: a large-preferring worker holds a semaphore slot and blocks
  on `largeCh` before the discovery goroutine runs, so a giant repo starts the
  instant it is classified regardless of small-repo queue depth or classifier
  timing. The existing full-corpus measurement (895 repos, #3711 section above)
  showed the giant-repo semaphore wait was ~90-100 s under best-effort scheduling;
  with dedicated workers the wait is bounded to the time for the first giant to
  finish parsing (semaphore capacity = `LargeRepoMaxConcurrent`, default 2), not
  the entire small-repo bulk. No new full-corpus run was required: the correctness
  and determinism proof is the test suite — `TestGiantRepoStartsBeforeSmallRepos`
  (`git_source_giant_start_test.go`) proves a giant reaches `processRepo` before
  any small repo under -race with 5 repeated runs; `TestSchedulerSmallWorkerReservedWhenCapEqualsWorkers`
  proves the P2 fix: with workers=2 and semCap=2, the small repo completes without
  waiting for `largeCh` to close. The no-regression suite
  (`TestSchedulerWorkerExitsOnCancelNoLeak`, `TestSchedulerFirstErrorWins`) proves
  no goroutine leaks and correct error propagation under -race.
  Gate: `go test ./internal/collector/ -run
  'TestGiantRepoStartsBeforeSmallRepos|TestSchedulerSmallWorkerReservedWhenCapEqualsWorkers|TestSchedulerWorkerExitsOnCancelNoLeak|TestSchedulerFirstErrorWins'
  -count=5 -race -timeout 120s`.

- Observability Evidence: The existing `eshu_dp_large_repo_semaphore_wait_seconds`
  histogram (recorded in `git_source_scheduler.go` `processLargeRepo`) and
  `eshu_dp_large_repo_classifications_total` counter (recorded in the discovery
  goroutine in `git_source_stream.go`) let an operator observe giant start order
  and peak concurrency. The semaphore wait histogram shows how long each giant
  waited for a slot; the classification counter shows how many repos entered each
  size tier. No new metric, span, status field, or label was added by this change.
