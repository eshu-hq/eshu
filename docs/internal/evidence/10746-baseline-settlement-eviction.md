# NVD baseline: a settlement eviction is counted and logged (#10746)

The independent review of #10835 found that the extension host's post-commit
stash in `go/internal/collector/extensionhost/nvd_baseline_settle.go` evicted
its oldest settlement when all 64 slots were taken, with no counter and no log.
If that settlement belonged to a chunk that had committed, the chunk's
forward-rotation count and `nvd baseline chunk committed` INFO line were lost
without a trace. `stash` now returns the row it evicted, and the caller reports
it after the stash lock is released: one count on
`eshu_dp_nvd_baseline_settlement_evictions_total` and one WARN naming the dropped
work item. The choice of victim is now deterministic: the earliest stash time,
with equal times falling to the smaller work item id, where before Go's map
iteration order picked among equal times. No production caller fills the
baseline request fields yet (the issue's own statement), so nothing reaches the
stash outside tests today.

## Performance Evidence

Performance Evidence: the stash that every baseline chunk runs, below its bound,
is unchanged, and the full-stash path costs the same within the noise of a
shared host. `go test -p 1 ./internal/collector/extensionhost/ -run '^$' -bench
'BenchmarkBaselineSettlementsStash|BenchmarkNVDBaselineSettlementEvictionReport'
-benchmem -benchtime=100000x -count=7` on darwin/arm64, Apple M5 Max, go1.26.11,
with other agents running on the host (load average 17 to 48). Base is
`origin/main` `ea7e3f91e9`, measured by putting its `nvd_baseline_settle.go` back
and moving `nvd_baseline_settle_eviction_test.go` aside for the run, because
that file calls `reportSettlementEviction`, which the base does not have, so the
base run covers the two stash benchmarks only. They call `stash` as a statement,
so one benchmark file compiles against the base, where `stash` returns nothing,
and against the new code, where it returns the evicted row. Median with the
range of 7 runs:

| Benchmark | Base | After, first run | After, second run |
| --- | --- | --- | --- |
| stash below the bound (one claim per worker) | 62.3 ns (57.4 to 69.1), 0 allocs | 59.2 ns (57.9 to 74.4), 0 allocs | 56.5 ns (53.1 to 58.8), 0 allocs |
| stash into a full stash (evicts) | 1337 ns (1314 to 1402), 0 allocs | 1547 ns (1348 to 2539), 0 allocs | 1174 ns (1101 to 1378), 0 allocs |
| report one eviction (new, off the lock) | not present | 1298 ns (1222 to 1942), 56 B, 4 allocs | not run |

The first after run overlapped a heavier host load than the base run and the
second ran lighter, so the full-stash cost moves by more between runs than
between versions. No speedup or slowdown is claimed. The eviction scan still
reads the same 64 rows. The new work inside the lock is one extra time
comparison for each row that is not earlier than the current oldest, and a string
comparison only when two stash times tie.

## No-Regression Evidence

No-Regression Evidence: the conflict domain is the one mutex that every commit
worker of a host process shares, around a map of at most 64 settlements keyed by
work item id. A stash below the bound runs the same lookup and write under the
same lock as before. A stash into a full stash runs the same scan and delete,
then returns the evicted row, and the counter add and the WARN run after
`Unlock`, so the lock hold time does not grow with a slow log sink. No
goroutine, channel, or lock is added, and the lock order is unchanged: the
stash lock is taken alone and never while the report runs.
`TestNVDBaselineSettlementEvictionReportsOutsideTheStashLock` installs a log
handler that re-enters the stash from the WARN and requires the writer to finish
within 5 seconds, so a report moved under the lock deadlocks and fails.
`TestNVDBaselineSettlementEvictionCountsExactlyUnderConcurrentStashes` stashes
200 distinct work items from 200 goroutines and requires 136 evictions, 136
distinct WARN records, and a stash of exactly 64 rows afterwards. Both pass
under `go test -race`. Ten mutants each fail a test: evicting the newest entry,
logging the incoming id, reporting under the lock, dropping the counter,
treating a re-stash of a held key as an eviction, a capacity off by one, a
tie-break left to map order, a labelled counter, a victim picked by the smallest
work item id alone, and a counter skipped when no logger is wired. The last two
survived the earlier eviction tests. Their one descending-id test ran against a
constant clock, so every stash time tied and the smallest id won, which is also
what a victim chosen by id alone does. None of them built the policy with
instruments and no logger.
`TestNVDBaselineSettlementEvictionTakesTheEarliestStashNotTheSmallestID` stashes
the ids in descending order, so the earliest stash holds the largest id, and
requires that one to be evicted.
`TestNVDBaselineSettlementEvictionCountsWithoutALogger` builds the policy with
instruments and no logger and requires the counter. Both fail under their mutant
with `go test -race` and pass on the shipped code.

## Observability Evidence

Observability Evidence: an eviction is no longer silent.
`eshu_dp_nvd_baseline_settlement_evictions_total` counts each one and carries no
label, so no work item or run id becomes a label value. The WARN `nvd baseline
settlement evicted at capacity, post-commit signals for that chunk may be lost`
carries `baseline_run_id`, `year`, `work_item_id` (the dropped item, never the
incoming one), `collector_instance_id`, and `capacity`, and never a scope id.
The wording says "may" because the stash cannot tell a stale entry, whose commit
never landed, from a chunk that committed and is still waiting for its hook. At
3 AM, a non-zero `increase(eshu_dp_nvd_baseline_settlement_evictions_total[1h])`
means `eshu_dp_nvd_baseline_seed_rotations_total{direction="forward"}` and the
committed-chunk line may undercount for that window. The facts and the checkpoint
are unaffected, so read the stored seed identity, not the rotation counter, to
see which seed a year is on. Each `nvd baseline chunk committed` line carries its
`work_item_id`, so the work item in the WARN can be matched against them.

## Not checked

- Behavior on a deployed host; nothing reaches the stash there until the rest of
  #10746 fills the baseline request fields.
- `scripts/verify-telemetry-coverage.sh` does not discriminate this counter: it
  requires a coverage row only for instruments registered in `instruments.go` and
  for new stage files, and this counter is registered in
  `instruments_nvd_baseline.go` beside its siblings. The script exits 0 with and
  without the row. The row is there because every other NVD baseline metric has
  one.
- Timings are from a shared, loaded host and are not a latency budget.
