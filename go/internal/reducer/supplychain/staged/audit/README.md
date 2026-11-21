# stagedaudit

The staged stale-winners audit (routing `staged-audits`) is a sweep of
identifier ranges with a persisted cursor (eshu-hq/eshu#11131). It replaced the
whole-corpus tick of #10451, which deferred on its 1,000,000-fact budget once
the impact-fact corpus grew past it.

## Flow of one range (`StagedAuditor.AuditWinners`)

1. Read the cursor row on the writer (`ReadWinnersAuditSweep`, migration 335).
2. Pin one reader member. At sweep start (no cursor) run the NULL probe; a
   source finding fact with a NULL package, repository or digest defers as
   `null_key` before any range read.
3. Read one range on the member (`ReadWinnersAuditRange`): the next budget rows
   after the cursor in keyed-index order, trimmed to whole identifiers, an
   oversize identifier alone and whole, every operator fact loaded whole, the
   shipped winner body over the kept facts, and the diff against the stored
   winners in the same key interval. A statement timeout retries at half the
   budget down to one row; a one-row timeout defers as `timeout`. A '|' in a
   range identifier defers as `pipe`.
4. On the last range run the NULL probe again; a NULL there verifies the range
   but restarts the sweep without counting it complete.
5. Verify on the writer (`VerifyWinnersAuditRange`): drop candidates already
   queued (no lock), take the resweep lock only for the rest (try-lock, or
   after two misses on the same range the lock_timeout-bounded waiting
   claim), verify and queue repairs, compare-and-set the cursor. Outcomes:
   `advanced`, `completed`, `skipped`, `lock_busy`, `cursor_race`.

## Invariants

- One range is one standby statement and one snapshot; no cross-range state
  besides the persisted cursor and the process-local try-lock miss count.
- A range commits only through the cursor compare-and-set, so two instances
  advance each range at most once and the loser rolls its repairs back.
- A range is not a completed audit: only the range that completes the sweep
  reports `Partial == false`, which is what resets the audit last-success age.
- The legacy writer path and the split scan are untouched; routing selects
  this auditor only under the staged-audits mode.
