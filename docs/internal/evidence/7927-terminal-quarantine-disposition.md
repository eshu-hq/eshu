# #7927 Terminal Quarantine Disposition

One failed generation and one historical projector dead-letter item stay in
terminal quarantine. They are one full Git collection attempt whose generation
never activated; the scope still uses an older active full generation. This
note is the named baseline exception: completeness accounting treats these two
rows as known-quarantined, not as pending recovery, until the reopen proof
below exists.

## Why quarantine, not replay

Replay is mechanically unblocked but it is a promotion, not a retry. The
replay path returns the failed generation's items to `pending` and clears
their markers; the subsequent claim-to-ack cycle then reactivates the
generation (`TestReplayedFailedGenerationWritesAndActivatesLive`),
displacing the older active full generation the scope still uses. The dead
letter (2026-09-22) predates the `projection_write_started_at` marker
(migration 149, 2026-09-30), so a pre-marker partial canonical writer is
invisible to the `UncoveredProjectionWriters` probe, which requires the
marker (`go/internal/storage/postgres/generation_projected_commit.go`).
Existing replay tests cover no graph writes, and none reactivates a
failed generation (Ack) while an older active full generation exists
(`TestReplayAfterFullRecordsLatestWriteStartLive` interleaves the trees
without that reactivation).
Replaying would risk the good generation to rescue a stale one, and the
affected repository reads `archived=true`, which explains why no successor
generation exists.

## Quarantine stays visible

- Per-generation `dead_letter` counts in the generation lifecycle
  (`go/internal/storage/postgres/generation_lifecycle_sql.go`).
- `RecoveryStore.CountDeadLetterBacklog` counts terminal rows matching the
  replay filter (`go/internal/storage/postgres/recovery.go`), which fences
  only superseded projector generations, so this failed-generation row
  stays counted.
- Poison liveness matches newest-generation dead letters with auto-retry
  opt-in only
  (`go/internal/storage/postgres/poison/liveness_sql.go`).

## Reopen proof (all NOT_CHECKED from the drive host)

- R1: semantic completeness of both same-commit snapshots.
- R2: live deployed state — current dead_letter/failed counts, the failed
  generation marker value, poison auto-retry env, deployed-vs-main parity.
- R3: proof the September attempts left no partial canonical graph overlay.

Recorded from the 2026-10-17 arbiter ruling on #7927 (quarantine over
replay), following the 2026-10-10 owner investigation linked on the issue.
Clearing failure markers is not recovery proof.
