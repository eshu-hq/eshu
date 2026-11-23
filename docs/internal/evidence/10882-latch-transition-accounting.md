# #10882 latch transition accounting: one transition, one count

Refs #10882. Ruling:
[arbiter-replay-go.md section (b)](../../../.claude/arbiter-replay-go.md).

## The fault

Every pacing consult reads the shared latch row, decides the state it wants,
and, when that differs from what it read, wrote the row with an unconditional
upsert and then counted the transition. Consults racing one transition all
read the same prior state, all wrote, and all counted. The reducer-source path
(`evaluateSources`) and the #8470 depth path (`advanceClaimPacingLatch`) share
the statement and share the fault. Only the process that wrote the release
lowered the active gauge, so a process that counted an engagement but did not
write the release kept the gauge at 1 for a latch the row showed released.

In a replay the projector reported `engaged_total` 2 and `active` 2 after the
row read `engaged = false`. On this branch's base, 12 racing consults across
12 pools counted 4 engagements, raised the fleet gauge to 4, and left the
last writer's `engaged_at` on the row (RED run, live test below).

## What changed

- `claimPacingLatchUpsert` writes only when the stored `engaged` bit differs
  from the desired one: `DO UPDATE ... WHERE latch.engaged IS DISTINCT FROM
  EXCLUDED.engaged`. A consult that lost the race matches nothing.
- `writeClaimPacingLatch` runs it and reads `RowsAffected`. Only a statement
  that changed the row records telemetry, so one transition adds exactly 1 to
  `eshu_dp_workflow_claim_pacing_engaged_total`, and `engaged_at` is the first
  writer's.
- The active gauge follows ownership. The process whose statement engaged the
  latch raises it and remembers the engagement (`engaged_at`). Any process
  that holds a raise takes it back when a read that began after the raise
  shows the row released or on a newer engagement. A process that never
  raised it never lowers it, so no process reads below zero. Reads that
  settle: every source consult, every `SourcePacingStates` publication, every
  depth consult.
- `updated_at` is not read by any code or query in the repository (rg over
  `go/`, `schema/`, `scripts/`). It is the time of the last real transition,
  and a no-op re-engage no longer rewrites the row. `engaged_at` alone drives
  the stuck bound, and it is unchanged by a steady-state consult.

## Lock and isolation reasoning

The statement is a single `INSERT ... ON CONFLICT DO UPDATE ... WHERE`. Under
READ COMMITTED, PostgreSQL locks the conflicting row, waits out a concurrent
writer of it, and evaluates the `WHERE` against the latest committed version
of that row, even when no version is visible to the statement's snapshot
([Read Committed](https://www.postgresql.org/docs/18/transaction-iso.html#XACT-READ-COMMITTED),
[INSERT](https://www.postgresql.org/docs/18/sql-insert.html)). So of N
statements that want the same state, the first to take the row lock flips it
and the rest see the flipped row and match nothing. A stale engage against a
row another consult already released flips it again: that is a real second
transition and is counted as one.

One row, one statement, no second lock, no lock-order question. A losing
statement still takes the row lock for the length of its implicit
transaction (microseconds); it writes no new row version.

## Proof

All on disposable PostgreSQL 18.6 (`postgres:18.6`).

- `TestSourceBackpressureLatchRaceCountsOneTransitionLive` and
  `TestWorkflowClaimPacingLatchRaceCountsOneTransitionLive`: 12 independent
  pools, one barrier. RED on base: 4 row-changing writes, `engaged_total` 4,
  gauge 4, `engaged_at` not the winner's. GREEN: 1 write, `engaged_total` 1,
  gauge 1, `engaged_at` the winner's; a steady-state round leaves `xmin` and
  `updated_at` unchanged; after release every process's gauge reads 0 and the
  row is not rewritten by the observations.
- `TestPacingLatchEngageReleaseInterleavingLive`: 12 pools, 60 rounds, engage
  and release of one row racing. Each round's row-changing writes leave the
  row on the state their parity predicts; across the run engage wins minus
  release wins equal the final bit; the fleet gauge equals it after every
  process observes. With the `WHERE` deleted it fails in round 2 (12
  row-changing writes from `engaged=false`).
- Race and repeat: `go test -race -count=50` on the three live tests at `1903c4c709` against disposable PostgreSQL 18.6: exit 0, 0 failed tests, 0 data races, 609.6 s (150 test runs). A first run of the interleaving test failed 26 of 50 in round 0 only: the row did not exist yet, so a racing release inserted a disengaged row, a change that is not a flip. A real release is only written against a row the consult read engaged, so the test now seeds the row first.

## No-Regression Evidence

Conflict domain: one `workflow_claim_pacing` row per (signal, domain); worker
and lease settings are unchanged (no worker count, batch size, or lease
change). The statement is the same single-row upsert with one added predicate
on the conflict row.

`EXPLAIN (ANALYZE, BUFFERS)` of the statement for a consult that loses the
race (row already engaged), warm run, then pgbench with 4 clients on the one
hot row (6 s each, host load average about 40 from other lanes, so read the
row counters, not the latency):

| Statement | Execution | Buffers | Rows written (pgbench) | Dead tuples |
| --- | --- | --- | --- | --- |
| before (unconditional) | 0.175 ms | shared hit 11 | 868 and 708 updates | 90 and 114 |
| after (conditional) | 0.017 ms | shared hit 3 | 0 | 0 |

The EXPLAIN pair is one sample each. pgbench latency was 27.8 and 34.0 ms
before against 31.5 and 34.5 ms after on a loaded host, equal within noise.
A genuine transition costs the same one row write as before (0.013 ms before,
0.030 ms after, one sample each, rolled back).

## Observability Evidence

No new instruments. `eshu_dp_workflow_claim_pacing_engaged_total` now counts
latch transitions, one per transition fleet-wide, and
`eshu_dp_workflow_claim_pacing_active` sums to 1 while engaged and 0 once
every raising process has read the released row. An operator at 3 AM reads
the row through the coordinator `source_pacing` status for ground truth and
the two series for the transition history; the stuck and delay series are
unchanged. Wording updated in
[claim-pacing.md](../../public/reference/telemetry/claim-pacing.md).

## Limits

- A process that raised the gauge and restarts while the latch stays engaged
  loses its raise: the gauge reads 0 for that engagement. The row stays
  correct. This was already true; the doc now says so.
- A consult that read "behind" before a release and wrote after it re-engages
  the latch from stale data. This is a real second transition, counted once,
  and the next consult releases it. It is the pre-existing staleness of a
  read-then-write consult, not changed here.
- A release won before the winning engage's process has recorded its raise
  (a window of microseconds inside one process) leaves a raise that the next
  read settles.
