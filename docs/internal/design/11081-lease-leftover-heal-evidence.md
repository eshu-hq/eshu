# #11081 lease leftover heal: performance and observability evidence

Refs #11081. Part of #10860. Refs #10879.

This note records the proof for the claim-path change that stops a leftover row
in `fact_work_item_leases` from wedging the general reducer. It covers the
claim, heartbeat, and retirement-fence statements, migration 326, and the new
telemetry.

## What changed on the claim path

- The claim now clears a lease row the main table disowns before it inserts its
  own row for the same conflict key. "Disowns" is one definition shared by the
  claim CTEs, heartbeat, migration 326, and the orphan gauge: a row
  is live only when its main row matches `work_item_id`, `stage='reducer'`,
  status `claimed` or `running`, an unexpired `claim_until`, and the same
  `lease_owner` and `last_attempt_at`.
- The retirement fence (`fenceMarkReducerRowsQuery`) deletes the lease rows of
  the work items it marks, by `work_item_id`, in the same statement that marks
  them superseded and nulls their `lease_owner` and `claim_until` (as the other
  terminals do, `last_attempt_at` is left in place). It does not use the shared
  liveness test.
- A claim deferred by a live lease conflict is counted and logged; before, it
  returned an empty result with no signal.
- Lock order stays main row first, then lease row, in every statement.

Performance Evidence: claim-path benchmark before and after, PostgreSQL 18.

Backend: PostgreSQL 18 (disposable container, `postgres:18`), host load1 5.9 to
14 during the runs (shared host, so absolute numbers are noisy; compare the
pairs, not the absolute values).

Command, run once on the pre-fix claim statements (the five fix files reverted
to `origin/main`, new tests and telemetry kept so it compiles) and once on the
branch:

```
ESHU_REDUCER_CLAIM_BENCH_DEPTHS=1000,10000 go test -p 1 -run '^$' \
  -bench BenchmarkReducerQueueClaimDeepQueue -benchtime 200x -count 3 \
  ./internal/storage/postgres/
```

| Depth | Before (ns/op, 3 runs) | After (ns/op, 3 runs) |
| --- | --- | --- |
| 1,000 | 7,044,651 / 7,461,851 / 7,552,927 | 7,412,858 / 7,841,438 / 7,836,923 |
| 10,000 | FAIL: `Claim() depth 10000 claimed = false` in all 3 runs | 30,619,379 / 30,651,067 / 30,475,643 |

At depth 1,000 the claim cost is the same within noise (about 5% on the median,
inside the spread of the runs). Allocations are unchanged at 105 allocs/op and
about 5,160 B/op.

At depth 10,000 the pre-fix claim stops handing out work. A bisect with
`-benchtime` showed `10x` passing and `50x` failing on the pre-fix statements.
The benchmark's own reset
(`resetReducerClaimBenchmarkWork`) returns the main row to `pending` and leaves
its lease row behind, which is the exact #11081 leftover shape. The branch
passes 200 iterations three times. This is the issue's symptom reproduced by
the existing benchmark, not a new harness.

Why depth 1,000 passes and depth 10,000 wedges: the benchmark spreads rows over
at most 1,024 conflict keys. At depth 1,000 every key has one row, so the
leftover lease row the reset leaves behind is that row's own, and the claim's
`ON CONFLICT (work_item_id)` update absorbs it. Once a key has two rows, a
claim of the second row meets the first row's leftover on the shared key and
the unique index rejects it. Checked on the pre-fix statements at 200
iterations: depth 1,024 (one row per key) passes at 7.34 ms/op, depth 1,025
(one key now has two rows) and depth 2,048 both fail with `claimed = false`.

Plan proof: `TestReducerQueue11081ClaimExplainUsesLeaseIndex` runs `EXPLAIN (ANALYZE)` on
the batch claim statement against real Postgres and requires the clear to use the
`fact_work_item_leases_live_lease_uniq` index rather than a sequential scan.

Heartbeat path: the heal adds a join and a `DELETE` probe to every renewal,
and renewals run about twice per claim, so it was measured on its own.
`TestReducerQueue11081HeartbeatExplainUsesIndexes` runs
`EXPLAIN (ANALYZE, BUFFERS)` of `heartbeatReducerWorkQuery` on the 8,000-row
lease-test schema with a 4,000-row mirror and a stale sibling to clear. The
plan has no sequential scan of `fact_work_items` or `fact_work_item_leases`. The
sibling `DELETE` finds its rows through `fact_work_item_leases_live_lease_uniq`
(index condition on stage, conflict domain and the coalesced key), and the
holder and work lookups use `fact_work_items_pkey`. Execution time was 0.19 ms
with 32 buffer hits. The unchanged `hb` update itself uses
`fact_work_items_claim_until_idx` with a filter on `work_item_id`, as it did
before the heal.

Before and after, same container, statements alternated in one session (3,000
renewals each after a 200-renewal warmup, healthy mirror so both statements
succeed; the pre-fix statement errors on a stale sibling, which is the bug).
Three runs, p50 / p95:

| Run | Before | After |
| --- | --- | --- |
| 1 | 0.943 / 1.076 ms | 0.958 / 1.095 ms |
| 2 | 0.916 / 1.109 ms | 0.936 / 1.121 ms |
| 3 | 1.050 / 2.978 ms | 1.071 / 2.999 ms |

The heal adds about 15 to 20 microseconds per renewal (about 2%), and a
renewal is dominated by the round trip.

No-Regression Evidence: concurrency and lease safety, PostgreSQL 18.

Concurrency and lease safety, on PostgreSQL 18 (`go test -p 1 -count=1`, no
`-race`):

- `TestReducerQueue11081ConcurrentClaimsRaceStaleLeftover`: two `ClaimBatch`
  calls on separate sessions race on one key with a stale leftover. Exactly one
  hands out the sibling, neither returns an error (no 23505 or 40P01 escapes),
  and the final state is one lease row owned by the winner.
- `TestReducerQueue11081ClaimBatchClearsExplicitKeyLeftover`,
  `...ClaimBatchClearsStaleLeftover`, `...ClaimBatchClearsStaleRetryLeftover`,
  `...ClaimClearsStaleLeftover`, `...HeartbeatClearsStaleSibling`,
  `...StaleMirrorRowClearedByClaim`: each seeds a stale lease row and checks the
  claim or heartbeat heals it.
- `TestRetirementFence11081ClearsLeaseMirror`: an expired claimed reducer row
  leaves the fence superseded, with `lease_owner` and `claim_until` nulled and
  its lease row deleted in one statement.
- `TestReducerQueue11081CleanupMigration`: migration 326 deletes only
  `stage='reducer'` rows the main table disowns (including a claimed main row
  whose owner, `claim_until` and `last_attempt_at` are all NULL), keeps live
  rows and projector rows, and a replay is a no-op. The liveness test is a
  `NOT EXISTS` over the locked rows, the same shape as the claim and the
  gauge.
- The existing `TestReducerQueue10879*` lease-table tests still pass.

These tests fail on the pre-fix statements (11 of 12 fail; the migration test
passes there because the migration file is kept in that arrangement).

Observability Evidence: what an operator sees.

An operator at 3 AM sees the problem in three places:

- `eshu_dp_reducer_claim_live_lease_conflicts_total` (labels `claim_mode`:
  `batch`, `single`; `constraint`: `main`, `mirror`) counts each claim a live
  lease conflict defers.
- The WARN `reducer claim deferred by live lease conflict` carries `claim_mode`,
  `constraint`, and `lease_owner`.
- `eshu_dp_reducer_lease_orphan_rows` (no labels) is the count of reducer lease
  rows with no live main row. It is read from the Postgres gauge snapshot
  (refresh `ESHU_POSTGRES_GAUGE_REFRESH_INTERVAL`), never on the claim path or
  on scrape. A sustained nonzero value means a hold-end leaked its lease clear
  or an older build is still writing.

Both are documented in `docs/public/reference/telemetry/metrics.md` and the
reducer-core and runtime-surfaces coverage pages.

## Not measured here

- QA leftover-row removal and the items-per-request recovery on the QA
  environment (the last acceptance item) need the deploy and an approved change;
  they are not part of this branch's local proof.
- No `-race` run and no repo-scale corpus run. The claim benchmark and the
  heartbeat pair above are the only throughput measurements, both on a shared
  host and the 8,000-row test schema.
