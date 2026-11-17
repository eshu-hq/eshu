# NVD baseline trip latch: the durable latch contract (#10768)

Moved out of [10728-baseline-pacing.md](10728-baseline-pacing.md) section 5
under #10816, unchanged, because that document reached the 500-line Markdown
cap. The pacing design keeps a pointer under the same heading. Parent: the
trip-line wiring in that document's section 5; the pace that reads this latch
is its section 4. Refs #10728. Part of #9209.


The trip latch is a row per baseline run in
`vulnerability_nvd_baseline_trip_latches` (migration 298): every field of
`NVDBaselineTripState`, `sampled_at` (the last sample folded in), and a
`revision` that counts applied writes. Code: `NVDBaselineTripLatchStore` in
`go/internal/storage/postgres/vulnerability/nvd_baseline_trip_latch.go` and
the fold in `go/internal/coordinator/vulnerability/nvd_baseline_trip_latch.go`
(`LoadNVDBaselineTripLatch`, `ObserveNVDBaselineTrip`,
`CloseNVDBaselineTrip`).

- Guarded write. One statement per write, never read-then-write. The update
  applies only when `revision` still equals the value the writer loaded and
  `sampled_at` is not older than the stored one; zero rows affected is a
  skipped stale write, not an error. The first write is an
  `INSERT ... ON CONFLICT DO NOTHING` for a writer that saw no row. A
  `sampled_at` compare alone is not enough: the latch is a read-modify-write
  fold, so a writer that loaded state at T and folds the T+2h sample would
  overwrite another writer's T+1h fold, and an hourly sample could resurrect
  a trip an operator `Close` just cleared (Close records no sample). The
  revision closes both; the time predicate refuses a replayed older sample
  even when the revision matches.
- Fail closed. `LoadNVDBaselineTripLatch` returns `Open` for an unreadable
  store, a nil store, a blank run id, and a run with no row yet. A missing row
  holds planning until the first hourly sample writes it, so a restart that
  lost the row never reads as "never tripped". A run that already committed
  baseline chunks (non-empty `ReadNVDBaselineCheckpoints`) whose row is
  missing folds its first sample onto an open trip that needs six fresh quiet
  hours, never onto a clean slate. The error return and the open verdict are
  both set, so a caller that drops the error still holds.
- Gaps and replays. Samples stand for hours. A sample more than 90 minutes
  after the stored one (`NVDBaselineTripMaxSampleGap`: one hour plus 30
  minutes of tick jitter) is a missed hour: the hot streak and the quiet
  streak restart and re-enable eligibility is withdrawn before the sample is
  folded, and an open trip stays open. A sample less than 45 minutes after the
  stored one (`NVDBaselineTripMinSampleSpacing`) or at or before it is
  refused without a write, so a re-sample after a restart cannot score one
  hour twice toward re-enable. The thresholds are this change's decision, not
  the ruling's; they are constants.
- Clock bound. A sample time more than `NVDBaselineTripMaxSampleSkew` (2
  minutes) past the `now` passed to `ObserveNVDBaselineTrip` is refused with
  the `future_sample` outcome, an error, an open verdict, and no store read or
  write. Without the bound, one future-dated sample is persisted as
  `sampled_at` and every later real sample reads as stale until real time
  catches up, so the detector goes deaf. The bound is only as good as
  the independence of `now`: it is normally the database clock, and stamping
  the sample from that same clock is also safe, but a replica that stamps the
  sample and passes its own local clock as `now` would pass its own check
  after a clock step. A zero `now` is an error. A row already poisoned (stored sample
  time past `now` plus the allowance) is reported as `stored_ahead` with an
  error and no write instead of answering stale forever; the recovery is to
  delete the row, which re-folds onto an open trip through `runStarted` and so
  needs six fresh quiet hours.
- Who owns the hourly loop. The coordinator process that plans baseline
  chunks (the vulnerability-intelligence scheduler lane, wired by #10746)
  owns the hourly sample-and-save loop: it already holds the database and the
  planner, so the load and the fold sit in one process. The window collector
  and the commit path never write the latch. Interleaving does not need a lease:
  with several coordinator replicas each running the loop, the revision guard
  lets exactly one write per revision apply, and a second replica's sample
  inside 45 minutes is refused as too soon. Clock steps are a separate
  hazard the revision guard does not cover; the clock bound above covers them
  only when `now` is the database clock. A lease or single-flight remains an
  optimization against duplicate probe work (one aggregate query per hour),
  and #10746 may add one.
- Contract #10746 must keep. (1) Load the latch at the start of every plan
  tick, not once per process, and set the whole `NVDBaselineTripLoad` on
  `IntelligencePlanRequest.NVDBaselineTrip`; any error holds. A request that
  never carries a load has the zero value, which the planner holds with the
  closed reason `baseline_trip_unloaded` (an open, absent, or unreadable
  latch holds as `baseline_trip_open`), so skipping the load or dropping its
  error fails closed instead of planning into the load that tripped. (2) Start the hourly loop
  independently of planning: a new run has no row, so planning holds until the
  first sample lands, and a loop gated on planning would never start. (3) Pass
  `runStarted` from a non-empty checkpoint read. (4) Pass the database clock (one `SELECT now()` per tick) as
  `now` (stamping the sample from that same database clock is also safe;
  a replica's local clock for both is not); record `future_sample` and
  `stored_ahead` so a skewed replica or a poisoned row is visible to an
  operator. (5) Route the operator re-enable through
  `CloseNVDBaselineTrip`; on `ErrNVDBaselineTripLatchConflict` retry. (6)
  Record `RecordNVDBaselineTripLatch` with each returned outcome and the trip
  transitions from the `Before` and `After` states of each observation. (7)
  Hold planning when `stored_ahead` is recorded. `LoadNVDBaselineTripLatch`
  takes no clock, so the planner reads a poisoned row as an ordinary loaded
  latch and `ObserveNVDBaselineTrip` is the only place the condition is seen;
  the loop must stop planning for that run until the row is deleted, or the
  lane would keep planning off a latch that can no longer count. Load's
  signature does not change.
- Conservative semantics, by design. (a) A hot sample that loses its write to
  an operator `Close` is dropped, not retried, so the next sample lands about
  two hours after the stored time, reads as a missed hour, and restarts the
  hot and quiet streaks; a hot hour can be forgotten, never invented. (b) With several replicas each
  sampling, the 45 minute minimum spacing admits samples closer than an hour
  when ticks jitter, so quiet hours toward re-enable can accrue slightly faster
  than wall-clock hours (at worst one sample per 45 minutes). The re-enable
  count is still a count of distinct sample times at least 45 minutes apart,
  and the operator `Close` stays the final decision.
- Pace streak (#10814, migration 302). The row carries `soft_quiet_streak`, the
  count the adaptive chunk interval `max(floor, 30 min / 2^streak)` reads (pace
  ruling decision 8, and the 2026-11-17 arbiter ruling on the fold).
  `NVDBaselineTripState.Observe` folds it: while the trip is shut, a sample with
  CVE-keyed inflow at or under 7,500 an hour (half the trip rate, inclusive) and
  an oldest dirty age at or under 96 hours adds one. Any other sample sets it to
  0, and so does every sample that leaves the trip open (the opening sample is
  hot anyway; a quiet sample on an open trip does not count). A missed hour (the
  gap rule above) and a lost-row synthesized trip restart it at 0; an operator
  `Close` returns the zero latch, so a re-enabled lane restarts at 30 minutes and
  earns each halving again. The fold does not cap it: the planner that reads it
  (`nvdBaselinePaceInterval`) saturates the exponent at six halvings, so a long
  quiet stretch cannot overflow and a hand-edited negative value reads as zero.
  The streak is written by the same guarded statement as every other field
  (`soft_quiet_streak = $8`, revision `$9`), so it needs no lock of its own.

  | Delivery | Streak after |
  | --- | --- |
  | Applied sample, quiet, trip shut | previous + 1 |
  | Applied sample, not quiet, or the trip open | 0 |
  | Replay at or before the stored sample time (`stale`) | unchanged, no write |
  | Second tick inside 45 minutes (`too_soon`) | unchanged, no write |
  | Two samplers loaded one revision (`lost_race`) | the winner's single increment; the loser writes nothing and is not retried |
  | Next hourly sample after a lost race | folds onto the winner's row, one more |
  | Write failed, or applied with the acknowledgement lost | stored streak unchanged, or already moved once; the retry is `stale` and changes nothing, or folds once |
  | Sample after more than 90 minutes | reset to 0, then the sample counts as 1 if quiet |
  | Pre-`Close` sampler writes after `Close` | refused on the moved revision |

  No sample is ever folded twice and no increment is lost: the fold is pure on
  the loaded state, the write is a single revision compare-and-swap, and a
  dropped write is not retried within the hour. Migration 302 is
  `ADD COLUMN IF NOT EXISTS soft_quiet_streak INTEGER NOT NULL DEFAULT 0 CHECK
  (soft_quiet_streak >= 0)` on a table with one row per baseline run: a constant
  default adds no rewrite (the live test pins the relation's file node across
  the ALTER over 2,000 rows), old rows read 0, and the CHECK scan is momentary.
  A pre-302 binary never names the column and its inserts take the default, so
  the binary can roll back alone. Downgrade caveat: a pre-302
  binary that samples moves `sampled_at` and leaves the streak untouched, so
  after a downgrade and a re-upgrade the first quiet sample can resume a stale
  streak. Roll back before the floor is lowered, or `Close` the latch after
  re-upgrading.
- Staleness the planner reads (#10814). A shut latch proves nothing once its
  sampler has stopped, so the planner holds an incomplete run as
  `baseline_trip_stale` when `sampled_at` is older than 90 minutes
  (`NVDBaselineTripMaxSampleGap`, the gap the fold already treats as a missed
  hour; exactly 90 minutes is still fresh) or was never set. The check sits
  after the complete verdict, since a finished run has nothing to plan, and
  before the pace is read, so a dead sampler never paces from a streak nothing
  can refresh. Planning resumes on its own once a sample applies. One reading
  goes past the ruling's wording: a sample dated more than
  `NVDBaselineTripMaxSampleSkew` (2 minutes) ahead of the planner's `now` also
  reads stale, the bound the writer refuses at, so a stepped clock or a
  poisoned row cannot read fresh for ever.
- #10746 observability notes. The recorders in this change count outcomes but
  log nothing. #10746 must (a) emit a WARN log on `read_failed` and
  `write_failed` (the counter alone does not say which run or which error),
  and (b) carry the latch outcome on the plan span next to the closed hold
  reason, so an operator reading one plan span can tell an open trip from an
  unreadable latch. The hold reasons `baseline_trip_open` and
  `baseline_trip_unloaded` already ride the plan span through
  `appendNVDBaselineTarget` (`nvd_baseline_hold_reason`), as does
  `baseline_trip_stale`, and every hold also counts on
  `eshu_dp_nvd_baseline_plan_holds_total{reason}` (#10814); only the latch
  outcome attribute is left for #10746.
- Rollback. The migration only creates a new table nothing references, so
  the schema rollback is `DROP TABLE vulnerability_nvd_baseline_trip_latches`.
  A binary rollback is safe on its own: the older binary never reads the
  table and runs with the in-memory latch as before. A newer binary that loses
  the table fails closed (every load errors, planning holds), so a premature
  drop stops the lane and corrupts nothing. The migration number was
  re-checked at merge time. Main topped out at 297
  (`297_vulnerability_nvd_baseline_checkpoints.sql`) when this branch took 298.
  Sibling lane #10767 added 299 and edits the same golden files; main reached
  298 when this lane merged, and #10767 was rebased over it, taking 299. The
  second lane's rebase recomputed, rather than hand-resolved,
  `goldenBootstrapDefinitionsDigest` and `goldenBootstrapDefinitionsCount` in
  `go/internal/storage/postgres/migrations/embed_invariant_test.go` (the count
  is 325 with both 298 and 299), appended to
  `migration_checksum_manifest_test.go`, and extended
  `orderedBootstrapDefinitionNamesLatest` in
  `go/internal/storage/postgres/schema_order_latest_test.go` keeping 298 before
  299.

No-Regression Evidence: #10814 adds one INTEGER to the latch row and one
operand to its two guarded statements. The conflict domain is the keyed latch
row, written about once an hour per baseline run by the sampler (no worker or
lease setting changes; several replicas still serialize on the revision). On
PostgreSQL 18.6 over a scratch table of 50 rows, 20,000 keyed updates took
1.09 to 1.11 s without the column and 1.25 to 1.58 s with it (three runs each,
the with-column run second), about 55 us against 60 to 80 us an update, which
at one write an hour is not a cost. Migration 302 over 2,000 populated rows
left the relation's file node unchanged, so it is not a rewrite.
`TestNVDBaselineTripLatchStreakRacingSamplersIncrementOncePerHourLive` ran 16
racing samplers over six simulated hourly rounds (about 3 seconds of wall time):
1 applied per round, no error, no deadlock.

Observability Evidence: no new instrument. The streak is a column on the latch
row and rides `NVDBaselineTripObservation.Before` and `After`
(`SoftQuietStreak`), which the #10746 recorder and log lines read.
`eshu_dp_nvd_baseline_trip_latch_total{outcome}` already counts the outcomes the
streak's replay matrix is keyed on: `applied`, `applied_after_gap`, `stale`,
`too_soon`, `lost_race`, `write_failed`. The planner slice (#10814) adds
`eshu_dp_nvd_baseline_pace_interval_seconds` for the interval the planner
applied and counts `baseline_trip_stale` on
`eshu_dp_nvd_baseline_plan_holds_total{reason}`; the inflow gauge named in the
ruling is not built.
