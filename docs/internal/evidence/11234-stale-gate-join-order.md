# #11234 The reducer claim's stale gate no longer loops over every scope

Refs #11234. Every figure below is a local fixture figure. Nothing here
claims a QA gain; the post-deploy reading decides that. The acceptance
matrix, the pin, the differential and the replay ran on 2026-11-22 at a
tree whose claim statements, readiness and generation-filter fragments and
migrations are identical to this head (`git diff` over those files is
empty), so the measured text is this text.

The arbiter rulings of 2026-11-22 set what follows:

- The fix (E2): the gate first lists the pending/retrying reducer rows not
  on their scope's active generation id, by one join of the work rows to
  `ingestion_scopes`. Each such row then looks up its active generation by
  a per-row lateral of two primary-key probes that carries the #10863 `ON`
  text verbatim, so no CTE is joined to another CTE.
- The reproduction is the loop count, not buffers: the fixture must show
  the planner's estimate of 1 and the per-scope index loops QA showed.
- Acceptance, per cell, six warm runs, minimum: time never higher than the
  pre-#11234 text; with fresh statistics (production's state) buffers at
  most one third of it; without statistics at most 1.25 times, because
  there the old text is either catastrophic or cheap in buffers and slow in
  CPU, so time ranks the two texts. The matrix runs on the full bootstrap
  schema with the production index set.
- Equivalence rests on a both-ways differential of every gate statement on
  one snapshot, hermetic body guards, and a replica and primary plan read
  of the E2 text before merge.

The operator-gated proofs below run when `ESHU_REDUCER_CLAIM_BENCH_DSN`, or
failing that `ESHU_POSTGRES_DSN`, names a disposable PostgreSQL; they skip
when neither is set. They never touch QA.

Performance Evidence: on the QA-shaped fixture over the full bootstrap
schema (12,236 active scopes, 13,366 older generations, 10,000
pending/retrying reducer rows, PostgreSQL 18.6, six warm runs per cell,
minimum), the stale-gate chain falls from 47,354 to 620 buffers and from
45.4 to 4.6 ms with no stale row and fresh statistics; the per-scope loop
of 12,238 index probes is gone (0 with no stale row, 200 with 200). All
fourteen acceptance cells pass their bars (table below), including the
no-statistics cells where the old text took 3.1 to 13.2 s. Above 2,000
stale rows the gain narrows: at 10,000 the chain reads 64 percent of the
old text's buffers with the filter NULL.

Observability Evidence: no new signal. The claim is diagnosed with the
existing `eshu_dp_queue_claim_duration_seconds` histogram (per lane,
`queue="reducer"`) and `pg_stat_statements` for the batch claim statement
(its share of total time and its buffers per call against the 442,132
measured before the change). The funnel census
(`eshu_dp_reducer_batch_claim_candidates`,
`eshu_dp_reducer_batch_claim_representatives` and
`eshu_dp_reducer_batch_claim_drops_total`, see
[reducer-batch-claim-funnel.md](../../../go/internal/storage/postgres/reducer-batch-claim-funnel.md))
composes the same gate and counts the same rows.

## Problem

The batch claim, the single claim and the funnel census all compose
`reducerClaimStaleGateCTEs()`. Its #10863 text built
`claim_active_generations` for every scope and joined it to the candidate
rows. Postgres estimates that `(scope_id, generation_id)` join between
`ingestion_scopes` and `scope_generations` at 1 row (#6794 records the
same misestimate). With "1 row" the planner drove a nested loop from it:
for each of QA's 12,236 active scopes, an index probe of
`scope_generations` for older generations, then a probe of
`fact_work_items` for pending/retrying rows on each.

QA replica, deployed commit, cut of the batch claim through `candidate`:

- the chain took 210,728 of the cut's 251,521 buffers and 8.6 of 9.8 s;
- it found 0 rows;
- the primary planned the same loop for both lanes.

The 12,307 heap fetches on the older-generation index-only scan (its
visibility map is not set on QA) made each probe dearer. They are context
for why the shape was fragile, not a separate fix.

## Reproduction

The fixture reproduced the loop exactly: estimate 1 against 12,236 actual,
12,236 index searches of `scope_generations_scope_generation_idx`, 13,366
of `fact_work_items_scope_generation_idx`, 0 rows. Buffers were 50,588 to
77,705 against QA's 210,728, because QA's per-probe cost is about twice
the fixture's (bloat and a cold cache). The arbiter accepted the shape,
estimate and loop counts as the reproduction and withdrew the buffer bar.

## What changed

`stale_gate_rows` (MATERIALIZED) lists the pending/retrying reducer rows
whose generation is not their scope's active generation id, by one join of
the work rows to `ingestion_scopes`. It runs before any probe of
`scope_generations`, so with no stale row (the normal state) it is empty
and nothing below it does per-row work.

Each such row looks up its active generation through a
`CROSS JOIN LATERAL (... OFFSET 0)` of two primary-key probes that carries
the #10863 `ON` text verbatim and keeps the alias `scope`. `OFFSET 0`
keeps the planner from pulling the subquery up. The hermetic body guard
pins its presence, but nothing live shows it is needed: without it the
pin stays green at fresh statistics, and the no-statistics 2,000-stale
cells read the same buffers in the same time (68,250 / 45.3 ms and
26,000 / 15.0 ms against 68,250 / 46.2 and 26,000 / 15.5 with it). The
proven plan fence is `MATERIALIZED` (PostgreSQL 18 docs, section 7.8.3).
The lookup yields no row
when the scope has no valid active generation row, which drops the row
exactly as the old inner join did.

The body of `stale_reducer_candidates` is the #10863 text with two lines
replaced: its `FROM` line and its `scope` join. `claim_active_generations`
is gone; only the gate read it. The locking and update text is untouched.

## Candidates measured

Plain QA-shape fixture, three runs each, minimum, chain cut derived from
the constants:

| Text | 0 stale | 200 stale | Index loops | Hash and merge joins off |
|---|---|---|---|---|
| shipped | 69,687 buf / 69 ms | 70,038 / 71 ms | 13,366 | 89k / 86 ms |
| (a) work rows first, predicates verbatim | 51,829 / 46 ms | 52,839 / 51 ms | 12,236+ | 277 ms |
| (b) #6794 form, `LATERAL ... LIMIT 1` per scope | 47,637 / 58 ms | 48,213 / 59 ms | 12,236 | 11.2 s |
| E, `claim_active_generations` per stale scope | 2,767 / 6.4 ms | 4,230 / 12.5 ms | 1 / 200 | 16 ms |

(a) keeps the loop because the estimate stays 1. (b) is itself one probe
per scope. E passed, but without statistics it joined two CTEs and rescanned
one per stale row (about 450 ms at 2,000 stale rows). The shipped text E2
replaces that join with the per-row lookup and keeps E's cost elsewhere.

## Acceptance matrix

`TestStaleGate11234AcceptanceLive`, full bootstrap schema (production
index set), six warm runs per cell, minimum, host load 15 to 30 (20 to 48
for the 10,000 cells). Fourteen cells, three bars: time never higher than
the pre-#11234 text in every cell; with fresh statistics, buffers at most
one third up to 2,000 stale rows and no higher than the old text's at
10,000; without statistics, at most 1.25 times. All fourteen cells pass.
The figures are the gate chain cut (`staleGate11234ChainCut`), not the
whole statement.

| Stats | Stale | Filter | Pre-#11234 buf / ms | Shipped buf / ms | `ingestion_scopes` loops / buf |
|---|---|---|---|---|---|
| fresh | 0 | NULL | 47,354 / 45.4 | 620 / 4.6 | 1 / 200 |
| fresh | 0 | set | 47,354 / 45.6 | 895 / 2.9 | 1 / 200 |
| fresh | 200 | NULL | 48,354 / 46.3 | 2,666 / 10.0 | 200 / 800 |
| fresh | 200 | set | 47,704 / 45.6 | 1,409 / 3.6 | 50 / 350 |
| fresh | 2,000 | NULL | 57,354 / 55.9 | 17,143 / 26.0 | 2,000 / 6,200 |
| fresh | 2,000 | set | 50,854 / 47.6 | 6,037 / 8.6 | 500 / 1,700 |
| fresh | 10,000 | NULL | 80,771 / 89.8 | 51,962 / 73.9 | 9,000 / 27,200 |
| fresh | 10,000 | set | 75,270 / 76.5 | 15,711 / 24.3 | 2,500 / 7,700 |
| none | 0 | NULL | 66,099 / 10,795 | 40,459 / 23.1 | 10,000 / 30,000 |
| none | 0 | set | 18,599 / 3,102 | 17,959 / 9.1 | 2,500 / 7,500 |
| none | 200 | NULL | 67,580 / 11,033 | 43,240 / 25.7 | 10,200 / 31,200 |
| none | 200 | set | 18,955 / 3,159 | 18,765 / 10.2 | 2,550 / 7,800 |
| none | 2,000 | NULL | 80,890 / 13,236 | 68,250 / 46.2 | 12,000 / 42,000 |
| none | 2,000 | set | 22,140 / 3,762 | 26,000 / 15.5 | 3,000 / 10,500 |

Above 2,000 stale rows the gain narrows. The gate does a fixed amount of
work per stale row (two primary-key lookups, about 4 buffers and 0.006 ms
per row on this fixture), where the old text paid a large fixed cost and
little per row. With fresh statistics the chain reads at most one third of
the old text's buffers up to 2,000 stale rows. At 10,000, which is every
pending row of the fixture, it reads 51,962 against 80,771 buffers (64
percent) in 73.9 against 89.8 ms with the filter NULL, and 15,711 against
75,270 (21 percent) in 24.3 against 76.5 ms with the filter set. The
one-third bar is asserted up to 2,000; at 10,000 the test asserts only
that time and buffers are no higher than the old text's. The ratio crosses
one third somewhere between 2,000 and 10,000 stale rows with the filter
NULL; the point was not measured. Nothing is measured above 10,000. A
straight line through the 2,000 and 10,000 cells would reach the old
text's time near 19,000 stale rows; that is an extrapolation, not a
result. QA's stale set at the last reading was about 14 rows.

The two 10,000 rows ran three times on fresh seeds, on the committed gate
text (the same SQL as this head; only comments differ). Two seeds gave the
figures above (the other of the two: 51,962 / 73.6 ms against 80,716 /
88.9, and 15,711 / 23.3 against 75,215 / 75.3). One seed's ANALYZE sample led
both texts to a different plan: 70,460 / 88.9 ms against 106,818 / 112.1
with the filter NULL (66 percent), and 20,710 / 27.8 against 100,818 /
97.8 with it set (21 percent). Every seed passes all three bars. The
10,000 cells also hold the pin's bounds: index loops at most 9,000 against
a bound of 10,064, and no rescans.

`ingestion_scopes` shows 9,000 lookups for 10,000 stale rows (2,000 for
2,000) because of the seed, not a plan change. The plan is the same at
both sizes, with no `Memoize` node. Stale rows rotate over eight domains,
so 1,250 of the 10,000 are Crossplane rows. The gate keeps a stale
Crossplane row only while a newer pending carrier exists in its scope, and
the live rows that supply carriers sit on scopes 1 to 2,000. So the 1,000
Crossplane stale rows on scopes above 2,000 drop at the hash join's filter
before the lookup (`Rows Removed by Join Filter: 1000`), which leaves
9,000. At 2,000 stale rows every stale row is on scopes 1 to 2,000, so
none drop.

"Filter set" is the vulnerability lane's two domains. The
`ingestion_scopes` probes are logged, not pinned: with statistics the work
rows hash-join to one scan of the table; without, the planner probes it
once per live row. QA's `ingestion_scopes` is bloated (13,064 buffers for
12,976 rows), so one scan there costs more than here, but it is bounded by
the table, against the 210,728 of the loop.

The claim-benchmark schema the first pin used lacks several production
indexes. On it the pre-#11234 text happened to plan cheaply without
statistics (3,163 buffers), which hid its no-statistics cliff. The
acceptance matrix, the differential and the pin's RED and GREEN halves run
on the full bootstrap schema; the benchmark-schema pin stays as a fast
guard.

## Pin

`TestStaleGate11234NoPerScopeLoopLive` (the reproduction's loop count,
made a pin): index loops on `fact_work_items` and `scope_generations` at most the
non-active row count plus 64, and no nested loop whose inner side rescans
a CTE or table more than 1,000 times, with hash and merge joins on and off
and the filter NULL and set. RED on the shipped #10863 text at 6931873148
(the pin's commit, before the fix):
13,366 loops with no stale row, 12,236 with 200. GREEN on this change on
both schemas: at most 0 loops with no stale row and 200 with 200, no
rescans. On the full schema the same run asserts that the frozen
pre-#11234 chain still breaches the pin (12,238 loops).

## Equivalence

- `TestStaleGate11234DifferentialLive` runs the pre-#11234 and shipped
  forms of the stale set, the batch claim's hand-out, the single claim's
  hand-out and the funnel counts on one snapshot (claims rolled back), with
  0 and 200 stale rows and the filter NULL and set. EXCEPT is empty both
  ways in every case.
- Its edge rows: stale pending and retrying; readiness phases under
  neither generation (held), only under the active one, and under its own;
  the fan-out-debt spare and a plain supply-chain row; a sole stale
  Crossplane row and one with a newer pending carrier; the equal
  `ingested_at` tie broken both ways; a row on a newer generation; a scope
  with no active generation; an active id naming another scope's
  generation; two stale rows on two older generations; a stale row outside
  the filter; a row whose generation belongs to another scope. The test
  checks every edge row's verdict, so the seeds exercise each branch. The
  foreign keys of `fact_work_items` forbid a row without an
  `ingestion_scopes` row or a generation row.
- Hermetic guards: the body is the base body with exactly the two lines
  replaced, and the lookup carries the base `ON` text; `stale_gate_rows`
  selects every column the body reads through `stale`, the payload
  included; no claim text names `claim_active_generations`. Each has a
  seeded mutant that must fail.

## Concurrency

The gate is read-only and stays inside each claim statement, one snapshot.
Both remaining CTEs are MATERIALIZED, as the old ones were, so EvalPlanQual
re-runs neither form; the two lock arms re-read the same named
`stale_reducer_candidates` set. `TestStaleGate11234LeavesEveryOtherByteUnchanged`
pins every byte of the three statements outside the gate to the base text
(sha256, with seeded edits of `SKIP LOCKED` and `RETURNING`). The contention
gate suite, the lock-recheck EvalPlanQual tests, the sweep lock-order
tests, the gate-precompute storm, duplicate-delivery and dead-letter tests,
and the supersede and #11081 suites pass unchanged on this change.

## Claim replay

`TestStaleGate11234ClaimReplayLive` on the full bootstrap schema, the
test binary built at the base (662083f413 plus the harness files) and at
this change, same seed, five rolled-back EXPLAINs of the whole batch claim
and 40 `ClaimBatch(4)` calls through the production path:

| Build | Stale | Filter | Batch claim EXPLAIN median | `ClaimBatch(4)` p50 / p95 |
|---|---|---|---|---|
| base | 0 | NULL | 106.3 ms / 48,378 buf | 102.9 / 109.2 ms |
| base | 0 | set | 62.5 ms / 48,665 buf | |
| base | 200 | NULL | 107.2 ms / 49,397 buf | 145.9 / 178.8 ms |
| base | 200 | set | 64.5 ms / 49,037 buf | |
| change | 0 | NULL | 64.4 ms / 1,645 buf | 72.9 / 81.5 ms |
| change | 0 | set | 19.9 ms / 2,206 buf | |
| change | 200 | NULL | 67.8 ms / 3,709 buf | 65.8 / 72.1 ms |
| change | 200 | set | 21.3 ms / 2,742 buf | |

The whole claim's buffers fall by 96 to 97 percent on this seed. Its
remaining time is outside the gate (`base` through the ranking windows and
the lock arms), and with the filter set the claim already ran in about a
third of the time. Host load 15 to 45 during both runs; the base run's
200-stale p95 includes a load spike.

## QA plan read

QA plan read of the E2 text (replica ANALYZE, primary plain EXPLAIN,
2026-11-22 17:35Z): it matches the fixture. The gate chain read 12.3
percent of the before buffers (212,330 to 26,123), and the plans have the
same shape on the primary and the replica. The read does not establish
same-snapshot candidate equality; that rests on the differential.

## Open items, not in this change

- The Crossplane carrier `EXISTS` inherited from #10863: without
  statistics it walks every reducer row through
  `fact_work_items_reducer_conflict_claim_idx` per probe (17.3M buffers in
  the old text at no statistics and 2,000 stale rows on the psql fixture,
  1.56M in this one). Recorded on #11234.
- The vulnerability lane: this change removes the stale chain, about 35
  percent of that lane's cut; `base` through `reps` (about half) remains.
  Recorded on #11234.
- The standalone stale-generation sweep is untouched.
- Growth above 10,000 stale rows is unmeasured. Recorded on #11234.

## Deploy gates

Per lane, before and after: claim seconds per second, seconds per claim,
rows per claim, executions per second by outcome, workers active, and the
batch claim's pooled buffers per call against 442,132. After deploy, read
the count of non-active pending or retrying reducer rows (the
`staleGate11234NonActiveRows` query) next to the claim's buffers per call,
so a large stale set is visible when it happens. Read the deferred
rate after deploy: a faster claim hands deferring rows out faster. A
statement whose share of total time passes 5 percent returns to the
arbiter.

NOT_CHECKED: a QA reading of this text after deploy (only the pre-merge
plan read above); the built reducer binary
(`go/cmd/reducer`) against this seed, as opposed to the package test
binary on the production claim path.
