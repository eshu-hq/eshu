# Owned-package hash-slot window: cost and signals (#9422)

## What changed at runtime

`FactStore.ListOwnedPackageDependencyTargets`
(`go/internal/storage/postgres/owned_package_targets.go`) is the one read
behind derived vulnerability-intelligence and package-registry targets. Before
this change it returned `limit + 1` rows from a rotated rank,
`MOD(target_rank - MOD(bucket * limit, T) + T, T)`. Because T (the distinct
target count) changes between windows as Git generations activate, each window
started at an effectively random rank. In the QA environment no window pair in
five consecutive pairs advanced by one page.

The read now gives each distinct target a fixed slot, `sha256(key)` low 24 bits
mod P, with P the smallest power of two where `P * target_limit >= 2 * T`. A
window returns slot `bucket mod P` in the global order, plus the window stats
(T, P, slot, slot_size, rows read). A one-row `window_stats` CTE (T from
`COUNT(*)` of the distinct targets, then P and the slot) is LEFT JOINed to the
slot rows, so an empty slot or T = 0 still returns one row carrying the stats.
Scope, limits and cardinality:

- Scope: the same predicate as before (active generation, `content_entity`,
  `git`, `Variable`, `config_kind = dependency`, the configured
  `package_manager` values). The scan is unchanged.
- Limit: the read limit is still `target_limit + 1`. The fill rule keeps the
  mean slot at or below `target_limit / 2`, so a slot fits in one read.
- Timeout: no new statement timeout. The read runs under the coordinator's
  existing context.
- Cardinality: the hash runs on the T distinct rows (30,091 at measurement),
  not the 2.7M scanned rows. The slot predicate depends on `COUNT(*) OVER ()`,
  so the planner cannot push it below the window aggregate.

Performance Evidence: shim lane `RE:shim9422-1` measured the shipped statement
(S) against the hash-slot statement with the MATERIALIZED fence (M, the first
version of this PR, before the `window_stats` join), OFFSET 0 (O) and no fence
(U) on the QA read
replica at origin/main `5f205c4d7c`. Each ran 6 times, 24 runs in total, between
17:22:05Z and 17:28:28Z on 2026-10-28, interleaved with the first mover rotated
each round. The bucket varied per run (996441 to 996464). Every run used
`statement_timeout` 15 s, and none timed out. The figure is EXPLAIN (ANALYZE,
BUFFERS) Execution Time.

| Variant | n | Mean | Median | Min | Max | sd | Delta vs S |
| --- | --- | --- | --- | --- | --- | --- | --- |
| S shipped, limit 101 | 6 | 13.933 s | 13.894 s | 13.588 s | 14.378 s | 0.283 s | |
| M MATERIALIZED, limit 2001 | 6 | 14.016 s | 14.009 s | 13.614 s | 14.398 s | 0.264 s | +0.083 s |
| O OFFSET 0 | 6 | 13.937 s | 13.929 s | 13.809 s | 14.112 s | 0.111 s | +0.004 s |
| U no fence | 6 | 13.955 s | 13.987 s | 13.708 s | 14.112 s | 0.159 s | +0.022 s |

The acceptance bar was at most +0.5 s over the 13.7 to 14.1 s baseline band.
All variants pass, and every delta is inside one sd of the shipped runs: about
14 s either way. Buffers per run were the same within noise (S round 1: hit
8,742,017, read 904,476; M round 1: hit 8,746,323, read 900,170). The plan is
the same index scan of 2,715,673 rows, then Unique to 30,091, a 5.5 MB
quicksort for the slot window, and 879 to 1,000 rows per slot under the 2,001
read cap. The fence is not load-bearing on this plan, so no test asserts it.

Slot balance at P = 32, L = 2,000 (one snapshot, T = 30,091): min 879, max
1,000, mean 940.3, sd 29.9. Every ecosystem with inventory (composer, go,
maven, npm, pypi, rubygems) has targets in all 32 slots. rubygems has 6 to 15
per slot.

### Re-measure of the shipped text (window_stats join)

The `window_stats` CTE and the LEFT JOIN changed the statement after the shim
run, so the text this PR ships (N) was measured again on the same QA read
replica on 2026-10-28, interleaved with the shipped statement (S) and the M
text. Each variant ran 4 times between 19:53:30Z and 19:56:47Z, with the first
mover rotated each round (S M N, N S M, M N S, S N M). The bucket varied per run
(997201 to 997212). N and M were rendered from the Go builders for the
vulnerability caller. The figure is EXPLAIN (ANALYZE, BUFFERS) Execution Time.

A first batch at `statement_timeout` 15 s (19:48 to 19:51Z) timed out on all
12 runs, S included. Nothing else was active on the replica, and the heap is
86 GB, so these were cold-cache runs. One discarded S warm-up at 30 s then ran
in 14.98 s. The measured batch used 30 s, the limit the arbiter ruling allows
when a baseline run times out. No run reached it.

| Variant | n | Mean | Median | Min | Max | sd | Delta vs S |
| --- | --- | --- | --- | --- | --- | --- | --- |
| S shipped, limit 101 | 4 | 14.852 s | 14.819 s | 14.650 s | 15.118 s | 0.199 s | |
| M first PR text, limit 2001 | 4 | 14.778 s | 14.850 s | 14.383 s | 15.030 s | 0.278 s | -0.074 s |
| N shipped text, limit 2001 | 4 | 14.988 s | 15.057 s | 14.638 s | 15.199 s | 0.243 s | +0.136 s |

N is within one sd of S in the same session (+0.136 s). The whole session ran
about 0.9 s above the shim's run: S itself averaged 14.85 s against 13.93 s,
with T at 30,669 instead of 30,091 and 2,813,766 scanned index rows instead of
2,715,673. Against the shim's absolute band (13.7 to 14.1 s, plus 0.5 s) N's
mean is 0.39 s over, and today's S is 0.25 s over the same bar. The comparison
that holds the replica's state constant is the interleaved one.

Plan (EXPLAIN VERBOSE, bucket 997250, 14.41 s): the same index scan and
Unique to 30,669 distinct targets. `sha256(convert_to(...))` is in the output
of the 30,669-row WindowAgg, so the hash still runs on the distinct rows. The
stats side is an Aggregate over a second CTE Scan of the materialized
`distinct_targets` (30,669 rows in memory, no new buffers). It feeds a Hash
Right Join with the 30,669-row slot window, which returns 983 rows. Buffers
match S within noise (round 1: S hit 8,975,219 read 1,002,056; N hit 8,959,270
read 1,018,661).

Exact text (PREPARE and EXECUTE of the rendered N text with binds, in one
REPEATABLE READ READ ONLY transaction): for bucket 997300, N returned 964 rows
equal to the M text's 964 rows (T 30,669, P 32, slot 20, slot_size 964). With
L = 1 (P 65,536) the slot was empty, and N returned one row with NULL target
columns and T 30,669, P 65,536, slot 14,260, slot_size 0. For ecosystems with
no inventory (hex, swift), N returned one row with T 0, P 1, slot 0, both for
L = 2,000 and for single_pass (L = 0).

This is a no-regression result, not a speedup. The cost is still the full
scan, which #9426 addresses separately.

Coverage proof (local, PostgreSQL 18.6, isolated bootstrap schema):
`TestListOwnedPackageDependencyTargetsSlotCoverageLive` seeds 2,000 npm targets
and 319 rubygems targets (rubygems sorts last), reads 32 consecutive windows
from bucket 996,226 at `target_limit` 150, and adds one npm target per window
(T rises from 2,319 to 2,351). The shipped offset reader left 19 of the 319
rubygems targets unplanned. An arithmetic model of the shipped formula
predicted exactly 19 for that bucket, and misses at least one target from 56%
of 2,000 starting buckets. The hash-slot reader plans all 319.

Bound: with a 30-minute interval and `target_limit` 2,000, P is 32 (16 hours)
while T <= 32,000, and 64 (32 hours, half the load per window) past that. At
the default limit of 100, P is 1,024 at the QA T, so the deploy pairs the image
with `target_limit: 2000`.

Observability Evidence: each read adds the `owned_package_targets.read` span.
It carries `owned_package_targets.total_targets`, `slot_count`, `slot`,
`slot_size`, `rows_read`, `selected` and `truncated` attributes, and an
`owned_package_targets.window` event with the per-ecosystem selected counts.
`TestListOwnedPackageDependencyTargetsSlotWindowStatsLive` asserts the event
values against the returned window, including for an empty slot (its subtests
also cover T = 0 and pin the D1 hash bytes with a Go oracle). The coordinator logs one INFO line per read,
`workflow coordinator read owned package target window`, with `total_targets`,
`slot_count`, `slot`, `slot_size`, `rows_read`, `truncated`, `selected`,
`selected_ecosystems` and `selected_by_ecosystem`
(`TestLogOwnedPackageWindowWritesTheSlotStats`).
`eshu_dp_postgres_query_duration_seconds` gains the 5, 10, 30, 60, 120 and
300 s buckets, so the coordinator's 19 to 129 s planner reads land in a finite
bucket instead of `+Inf`. A slot that the read limit cuts short is reported
as `derived_target_budget_exhausted` skip evidence on the run.

## What an operator checks

- Two consecutive windows log consecutive `slot` values (`bucket mod P`) with
  the same `slot_count` unless T crossed a power-of-two multiple of
  `target_limit / 2`.
- On a rotating instance `truncated` stays false. If it is true, the slot
  outgrew the read limit, and the run's skip evidence names the unread count.
  A `single_pass` instance reads one slot holding the whole set (P = 1), so it
  logs `truncated=true` whenever T is above `target_limit + 1` and reports the
  targets past the read limit as `derived_target_budget_exhausted` by design:
  single_pass never plans them.
- An empty slot, or T = 0, still logs T, P and the slot, with `slot_size` 0.
- The read lands in a finite `eshu_dp_postgres_query_duration_seconds` bucket.
