# #11117 The full winners resweep drains the marks it covers

Refs #11117. Binding arbiter ruling:
[11117-winners-resweep-ruling-2026-11-21.md](../arbiter/11117-winners-resweep-ruling-2026-11-21.md)
(2026-11-21, verified at origin/main 49da4cc774). This note covers PR 1 of the ruling (B,
acceptance criterion AC3). It also records the corrected timeline (AC1).

## Corrected timeline (AC1)

The first version of #11117 read the QA watermark of 01:25:56Z as proof that no
resweep committed between 01:25Z and 05:13Z. That reading was wrong. 01:25:56Z
is the tick-start stamp of the general reducer pod. Its full resweep started at
that time and committed about 03:12Z. Before this change, the full stamped the
pod's Go clock from tick start. So a full that ran for about 1 h 47 min
committed a watermark 1 h 47 min old. A resweep did commit in the window. The
watermark was stale for two reasons: the full stamped its start time, and keyed
ticks could not advance past dirty rows queued before the full until they
drained.

The QA figures (6,388 to 6,635 s per full, two back-to-back fulls per roll) are
the arbiter's readings of
`eshu_dp_supply_chain_impact_winners_resweep_duration_seconds_sum{path="full"}`.
This note did not re-measure them.

## What changed

- `ResweepWinners` takes no stamp. Under the resweep lock, in one transaction,
  it reads S from the DB clock (`clock_timestamp()`, the same domain as
  `captured_at`). It then deletes every dirty-log row and staged mark the delete
  can see, and then runs the rebuild. The keyed tick dropped its caller stamp
  for the same reason in #10706.
- The log drain keeps the keyed drains' lock-then-delete shape: rows are locked
  in the global key order with `FOR UPDATE`, then deleted by seq. It also keeps
  their per-source tally and the NVD trip tally. The staging drain deletes the
  visible staged rows and counts each as one mark, the sum the collapse would
  have folded. Drained trigger-source marks reach `capture_rows_total` the same
  way a keyed tick reports them.
- The rebuild statement stamps
  `GREATEST(previous, LEAST(S, min(captured_at) of the residual log and staging - 1us))`.
  The residual is read inside the rebuild statement, so the cap covers every
  mark the rebuild snapshot sees. The keyed full fallback runs the same
  statement. It already passed S, or the horizon over the residual it read
  after its settle. The in-statement cap also covers marks that commit between
  that read and the rebuild snapshot, and the GREATEST stops the fallback from
  moving the watermark backward.
- The wake (#10876) carries the committed watermark, not the stamp input.

Why the delete runs before the rebuild: a writer stages its mark in the
transaction that makes the change. Every mark the delete sees therefore names a
change that is visible to the later rebuild snapshot under READ COMMITTED. A
delete after the rebuild would drop marks of changes committed between the two
snapshots. Those changes would then be lost.

The order needs a fresh snapshot for each statement. The full therefore pins
READ COMMITTED as its first statement. It does not rely on the session or
database default: under REPEATABLE READ the whole transaction would read the
claim's snapshot, the rebuild would miss marks committed after the drain, and
S would stamp over them.

Edge cases, each with a live proof in
`go/internal/storage/postgres/supply_chain_impact_winners_full_drain_live_test.go`:

- Marks captured before the full are gone after it. The next keyed tick is idle
  and moves the watermark forward (`...FullDrainsPreStampRowsLive`).
- A mark that executes before S and commits after the full stays queued. The
  next keyed tick applies it, and parity with a fresh recompute holds
  (`...FullLeavesLateCommitMarkQueuedLive`). This is a guard: it also passes
  on the code before this change, which never touched the queues. It fails a
  future drain that deletes rows it cannot see.
- A failed rebuild rolls the drain back and every mark is restored. The retry
  drains them (`...FullRollbackRestoresDrainedRowsLive`).
- A mark that commits after the drain but before the rebuild snapshot stays
  queued, and the watermark is its `captured_at` minus 1 us
  (`...FullStampsUnderResidualLive`). The same holds with the database
  default at REPEATABLE READ (`...FullPinsReadCommittedLive`, red before the
  pin).
- A residual mark older than the stored watermark leaves the watermark where
  it was, on the full and on the keyed over-cap fallback
  (`...FullNeverRegressesTheWatermarkLive`; removing the GREATEST turns both
  subtests red).

## Concurrency

Conflict domain: the dirty log, the staging queue and the winners table, under
the existing resweep advisory lock (`pg_try_advisory_xact_lock`) plus the
single-owner partition lease (lease TTL 60 s, interval 30 s, one partition).
The other writers of the log are the tick collapse and the scope convert, and
both run under the same lock. Capture owners only INSERT into staging and take
no row locks. So the staging delete waits on nothing, and an uncommitted staged
row is invisible to it and stays queued. The log drain takes row locks in the
same total key order as the keyed drains. No new lock order or retry is added.
The change has no worker-count, batch-size, or serialization component.

## Performance evidence

Performance Evidence: theory gate run before any production code, on the
disposable Postgres 18.6 container (`pg-11082`) against the real migrated
schema. The corpus was 55,000 fact log rows (half CVE advisories), 5,000 scope
log rows and 5,000 staged rows. The proposed drain statements ran under
`EXPLAIN (ANALYZE, BUFFERS)` inside `BEGIN`/`ROLLBACK`:

| Statement | Rows | Execution time |
| --- | --- | --- |
| Log drain (key-ordered `LockRows` over a sort, delete by seq, tallies) | 60,001 | 263.8 ms |
| Staging drain (delete, tallies) | 5,000 | 5.5 ms |
| Residual min over both queues | 0 | 1.0 ms |
| Total wall, three statements | 65,001 | 272.6 ms |

The target was under 5 s at about 60k rows, and the drain finished well under
it. The key-order sort spilled 5.2 MB to disk at the default `work_mem`. That
is acceptable at this size. A 10x backlog would spill more, but it would still
be one sort inside the existing full transaction. Rollback restored all 65,001
rows. Host load1 was 25 to 35 during the run, so treat the times as an upper
bound. The drain adds well under 1 s to a full that takes 6,388 to 6,635 s on
QA, and it removes the keyed backlog the full left behind.

The theory-gate shim (a throwaway live test that ran the three statements
above) is not committed. The QA-scale drain is not measured (NOT_CHECKED): QA
queues about 60k log rows and about 190k staged rows. A linear extrapolation
from this run (263.8 ms per 60k log rows, 5.5 ms per 5k staged rows) gives about
0.5 s, against the 5 s target.

Observability Evidence: no new instrument. The full path now reports `path`,
`identifiers` (drained marks) and `remaining` (marks that committed after the
drain) on the existing `supply chain impact winners resweep committed` log
line. It counts drained trigger-source marks on
`eshu_dp_supply_chain_impact_winners_capture_rows_total{source}`. It feeds the
committed watermark, not the Go clock, to
`eshu_dp_supply_chain_impact_winners_watermark_age_seconds`. AC4 signals land
with PR 2.

## Not covered here

The contract version and the skip of the startup full (A, AC2 and AC4) are
PR 2. Bounding every full to short transactions is #11119. Keyed-tick headroom
is #11120. AC5 is the live QA proof after the release.
