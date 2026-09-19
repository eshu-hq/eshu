# 6794 Status Blockage Live-Lease Filter

`/api/v0/collectors` (1.63–1.72s) and `/api/v0/collector-readiness`
(1.45–1.49s) still missed their budgets under the #6797 read-API latency gate's
seeded backlog after the #6794 status snapshot rewrite, against about 0.3s on
an idle full corpus. This note records why, the fix, and how both were
measured.

Source binding: "before" is the status SQL on `origin/main` (`925e8016f` for
the diagnosis, `c4ff6e447` for the final route measurement; the blockage SQL is
identical in both). "After" is branch `perf/6794-collectors-backlog`.

## Diagnosis

Seed: the #6797 gate's seed code (`BuildSeedPlan`, `SeedPostgres`, and
`SeedIaCFacts` in `go/cmd/read-api-latency-gate` on the #6797 branch, which is
not on `main` yet) against a local PostgreSQL 18 with the production schema:
800 scopes over every collector kind, 1,120 generations, 67,200
`fact_work_items`, and 150,000 IaC `fact_records`. Its 800 claimed or running
reducer rows all have `claim_until` NULL, so none is a live lease.

With statement logging on, one `/collectors` request issued 28 statements
totalling 80.9ms, and `activeWorkSummaryQuery` took 79.0ms of it.
`/collector-readiness` loads the same status report behind a 30s cache.

The gate seeds with `COPY` and measures right away, so the planner can run
without statistics. With statistics cleared (the state a freshly loaded table
is in), the summary query took about 1.5s; with statistics, about 75ms.

The difference was the blockage section's `blocked` join from eligible reducer
rows to live leases on the same conflict key. `eligible` is a CTE whose row
estimate is far below its real size (17 estimated against 15,200 actual even
with statistics; about 1 without). Without statistics the planner ran the join
as a nested loop, scanning `fact_work_items_reducer_live_lease_uniq` with no
index condition once per eligible row: every claimed or running row (800 here),
with `claim_until > $1` applied afterwards. That is 15,200 × 800 = 12.16M rows
and 12.28M buffer hits. With statistics the planner happened to pick a hash
join. The pre-#6794 standalone blockage query at `af4971189` behaves the same
(1,737ms without statistics, 37ms with them), so this predates the #6794 fold.

On a production-scale instance the join ran as a parameterized index probe on
the conflict key over few eligible rows, so it was cheap there. It is exposed
to the nested-loop plan whenever statistics lag a large claimable backlog.

## Fix

`inflight_leases AS MATERIALIZED` selects the live leases (reducer stage,
claimed or running, `claim_until > $1`), and `blocked` is a filter on
`eligible`, not a join:

```sql
WHERE COALESCE(
    (conflict_domain, conflict_key) IN (SELECT conflict_domain, conflict_key FROM inflight_leases),
    FALSE)
```

PostgreSQL runs an uncorrelated `IN (SELECT ...)` (an ANY sublink) as a hashed
SubPlan: the lease set is hashed once, then probed once per eligible row. No
join is left for the planner to choose. The `COALESCE(..., FALSE)` is
load-bearing for the plan, not the result: the keys are NOT NULL, but an `IN`
at the top level of `WHERE` is pulled up into a semi-join that the planner may
nested-loop again. Both sides key on `COALESCE(conflict_key, scope_id)`.

The filter returns exactly the rows the old join did. An eligible row has
`claim_until` NULL or not after `$1` and a live lease has it after `$1`, so no
row is on both sides, and the join's "lease is another row" condition never
excluded a match. `IN` emits each eligible row once whether one or several
leases share its key.

When PostgreSQL would not hash it (`subplan_is_hashable` in
`src/backend/optimizer/plan/subselect.c`, REL_18_STABLE): when the estimated
lease set, `rows × (MAXALIGN(width) + 24)`, exceeds
`work_mem × hash_mem_multiplier`. The lease rows are two text columns (width
64), 88 bytes each.

| work_mem | Hash memory | Estimated live leases needed to refuse hashing |
| --- | --- | --- |
| 4 MB (PostgreSQL default) | 8 MB | > 95,325 |
| 16 MB (Compose default) | 32 MB | > 381,300 |
| 64 kB (floor, multiplier 1) | 64 kB | > 744 |

Live leases are bounded to one per conflict key among claimed or running
reducer rows, which is at most the reducer workers times the batch claim size
across replicas: hundreds, two to three orders of magnitude below the default
threshold. The planner's estimate of the lease set also runs below its real
size.

Two earlier shapes were rejected. The first version of this fix kept the join
but selected live leases once into the materialized CTE; without statistics the
planner still ran that join as a nested loop, rescanning the lease CTE once per
eligible row (the `1ba58924c` column below). A second version marked each key
holding a lease with a window over one `UNION ALL` of eligible rows and leases.
It removed the no-statistics pathology, but it sorts every eligible row on every
call and spills that sort past `work_mem`; with statistics it cost 26–35ms more
than the hashed filter at 40,000 eligible rows and 70–80ms more at 100,000, on
a route dashboards poll.

`reducerConflictBlockageCTEs` has one production caller, the status summary, so
no claim or lease path changes. The live-lease CTE uses the same predicate as
the reducer claim query's `reducer_source_inflight`.

## Measurements

Performance Evidence: `EXPLAIN (ANALYZE, BUFFERS)` of the full
`activeWorkSummaryQuery` on PostgreSQL 18, run as a prepared statement with a
custom plan (what the driver does). Times are the minimum of five runs (one for
the old join without statistics): the host ran other jobs during these
measurements, so buffer counts, temp blocks, and loop counts are the stable
comparison and times are the best case of each.

Seed: N scopes, each with 20 pending reducer rows on its conflict key and one
claimed lease, of which the first L are live and the rest expired (expired
leases are themselves eligible). "No statistics" is `pg_statistic` cleared for
the three tables and `reltuples = -1` with autovacuum off, asserted before
measuring; `TRUNCATE` alone does not clear `pg_statistic`. Every cell returned
byte-identical output from all four shapes (one result hash per cell). `loops`
is the largest Actual Loops of any node scanning `fact_work_items`,
`inflight_leases`, or `eligible`.

Shapes: before is the old join; `1ba58924c` is the join to the materialized
lease CTE; window is the rejected window; after is the hashed filter.

No statistics:

| Eligible, live leases | work_mem | Before ms / buffers / loops | `1ba58924c` ms / loops | Window ms | After ms | Temp blocks (before, 1ba, window, after) |
| --- | --- | --- | --- | --- | --- | --- |
| 40k, 0 | 4MB | 13,496 / 17.77M / 42,000 | 115 / 42,000 | 129 | 144 | 2475, 2475, 2475, 2475 |
| 40k, 0 | 16MB | 5,943 / 17.77M / 42,000 | 70 / 42,000 | 110 | 77 | 0 |
| 40k, 200 | 4MB | 10,012 / 17.69M / 41,800 | 571 / 41,800 | 99 | 78 | 2475 each |
| 40k, 200 | 16MB | 8,999 / 17.69M / 41,800 | 615 / 41,800 | 101 | 70 | 0 |
| 40k, 2,000 | 4MB | 10,673 / 16.93M / 40,000 | 4,957 / 40,000 | 137 | 112 | 2475 each |
| 40k, 2,000 | 16MB | 8,865 / 16.93M / 40,000 | 4,685 / 40,000 | 136 | 112 | 0 |
| 100k, 0 | 4MB | 33,646 / 112.3M / 105,000 | 181 / 105,000 | 268 | 176 | 9814, 9814, 11130, 9814 |
| 100k, 0 | 16MB | 41,187 / 112.3M / 105,000 | 257 / 105,000 | 334 | 229 | 0 |
| 100k, 200 | 4MB | 39,433 / 112.1M / 104,800 | 2,773 / 104,800 | 449 | 268 | 9808, 9808, 11122, 9808 |
| 100k, 200 | 16MB | 36,050 / 112.1M / 104,800 | 1,404 / 104,800 | 238 | 161 | 0 |
| 100k, 2,000 | 4MB | 42,954 / 110.1M / 103,000 | 12,662 / 103,000 | 290 | 217 | 9744, 9744, 11054, 9744 |
| 100k, 2,000 | 16MB | 45,713 / 110.1M / 103,000 | 12,627 / 103,000 | 276 | 200 | 0 |

The window and the hashed filter run every scan once in every cell, and the
hashed filter's plan says `hashed SubPlan` in every cell. The window's extra
temp blocks at 100,000 rows and 4MB are its sort spilling.

Analyzed statistics:

| Eligible, live leases | work_mem | Before ms / buffers / loops | `1ba58924c` ms | Window ms | After ms | Estimated (actual) live leases |
| --- | --- | --- | --- | --- | --- | --- |
| 40k, 0 | 4MB | 67.5 / 7,766 / 1 | 67.8 | 94.1 | 67.7 | 1 (0) |
| 40k, 0 | 16MB | 61.9 / 7,766 / 1 | 61.5 | 88.1 | 63.3 | 1 (0) |
| 40k, 200 | 4MB | 191.2 / 220.8k / 41,800 | 75.6 | 98.4 | 74.3 | 10 (200) |
| 40k, 200 | 16MB | 185.8 / 220.8k / 41,800 | 71.1 | 94.1 | 69.4 | 10 (200) |
| 40k, 2,000 | 4MB | 210.3 / 286.9k / 40,000 | 120.4 | 135.6 | 115.5 | 94 (2000) |
| 40k, 2,000 | 16MB | 202.9 / 286.9k / 40,000 | 109.6 | 133.1 | 109.9 | 94 (2000) |
| 100k, 0 | 4MB | 191.8 / 19,427 / 1 | 200.3 | 270.5 | 202.2 | 1 (0) |
| 100k, 0 | 16MB | 170.8 / 19,427 / 1 | 171.0 | 250.5 | 171.7 | 1 (0) |
| 100k, 200 | 4MB | 638.5 / 547.4k / 104,800 | 254.5 | 400.8 | 206.7 | 9 (200) |
| 100k, 200 | 16MB | 647.0 / 547.4k / 104,800 | 228.4 | 315.1 | 261.2 | 9 (200) |
| 100k, 2,000 | 4MB | 476.6 / 985.1k / 103,000 | 236.2 | 295.4 | 226.2 | 98 (2000) |
| 100k, 2,000 | 16MB | 448.9 / 985.1k / 103,000 | 209.1 | 271.8 | 205.3 | 98 (2000) |

With statistics the old join still rescanned the lease index once per eligible
row whenever leases were live; the hashed filter matches `1ba58924c` and the old
join's best plans.

Stale statistics (analyzed while every lease was expired, then 200 leases made
live; 40,000 eligible, 4MB): the old join ran 625ms and `1ba58924c` 960ms, both
as a nested loop with the leases outer and `eligible` rescanned once per lease
(loops 200) while every lease node ran once. The hashed filter ran 209ms with
every scan once.

When hashing is refused: with 20,000 live leases analyzed and
`work_mem = 64kB`, `hash_mem_multiplier = 1`, the plan uses an unhashed SubPlan
that rescans the lease CTE once per eligible row (40,000 loops, about 31s and
1.9M temp blocks); at the default 8MB of hash memory the same seed hashed and
ran in 492ms. Plain `EXPLAIN` already shows `(SubPlan N)` in place of
`(hashed SubPlan N)`, so the check is cheap.

Gate seed, five runs each; with statistics present both plans read the same
buffers:

| Statistics | Before | After |
| --- | --- | --- |
| analyzed | 93–106 ms, 3,834 buffers | 93–100 ms, 3,834 buffers |
| cleared | 2,540–3,015 ms, 12,285,430 buffers | 101–106 ms, 4,638 buffers |

Route level, on the gate seed with statistics cleared: `eshu-api` built from
`c4ff6e447` against one built from a pre-rebase revision of this branch whose
Go code and SQL are identical to the shipped head, both binaries
confirmed by build revision and by the listening process's executable, ten
requests per route alternating which build goes first. Every response was 200
and every round's bodies were identical once timestamps were stripped.
`/collector-readiness` is cached for 30s, so only its first request reads the
database.

| Route | Before | After |
| --- | --- | --- |
| `/api/v0/collectors` | p50 5.160 s, max 5.627 s | p50 0.153 s, max 0.232 s |
| `/api/v0/collector-readiness`, first request | 2.435 s | 0.142 s |

Parity on the gate seed: on a copy where 100 scopes hold live leases and 50 hold
expired ones, the full summary output of the old and new queries at a fixed
timestamp is byte-identical (21 rows: 3 backlog, 10 blockage, 1 queue, 7
stage).

## Tests

- `TestReducerConflictBlockageFiltersEligibleByHashedLeaseSet` pins the shape:
  the COALESCE key on both sides, the fenced `IN` over `inflight_leases`, and no
  join, window, or `fact_work_items` read in `blocked`.
- `TestReducerConflictBlockageLeaseMixMatchesPreChangeJoin` compares the full
  `blocked` set with the pre-change join, kept verbatim as the oracle. It covers
  live running and claimed leases, expired and unset leases, a self-only lease,
  a pending row with a future `claim_until`, and a lease with no conflict key
  that blocks a sibling with no key and a sibling whose key is its scope id. The
  expected rows are named.
- `TestActiveWorkSummaryBlockageHashesLeasesOnce` requires a `hashed SubPlan`
  filter and one run of every `fact_work_items`, `eligible`, and
  `inflight_leases` scan, with 0, 50, and 200 live leases, both with no
  statistics (asserted empty) and analyzed, and with stale statistics. A canary
  subtest forces `work_mem = 64kB` and `hash_mem_multiplier = 1` over 20,000
  analyzed live leases and requires an unhashed SubPlan, so the marker check is
  proven able to fail. The pre-change query's loop count is logged, not
  asserted.

Mutation checks, each run against the live tests: restoring the join, or
dropping the `COALESCE` fence, fails the plan regression in every statistics
state (no hashed SubPlan); dropping the lease status filter, the eligible-side
COALESCE, or the `claim_until` filter fails the lease differential.

## Observability

No-Observability-Change: the fix changes the blockage CTEs inside an existing
read. The statement stays timed by `eshu_dp_status_snapshot_read_duration_seconds`
(`read="active_work_summary"`) and labeled on the `postgres.query` span
(`db.query.summary=active_work_summary`), which is where a regression of this
shape would show.
