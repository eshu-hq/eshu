# #10882 round 4 decisions and rollout notes

Status: Decided on arbiter ruling `arbiter-10882-r4` (2026-11-22). Companion to
[10882-source-backpressure.md](10882-source-backpressure.md) and
[10882-source-backpressure-fork-rulings.md](10882-source-backpressure-fork-rulings.md).
Those records hold the trigger, the actuators, the fork rulings, and the
earlier review decisions. This one holds the round 4 decisions that would push
them past the 500-line cap, and the rollout notes an operator needs before
enabling the leg.

## Enablement gate (binding)

The per-source leg ships disabled and stays disabled until measured. No
deployment sets the `ESHU_SOURCE_BACKPRESSURE_*` thresholds until the
built-binary two-source replay reports the per-claim consult cost at
`ESHU_SOURCE_BACKPRESSURE_SIGNAL_CACHE_TTL=0` and at a candidate non-zero TTL,
and this record names a recommended non-zero TTL with its staleness bound.

Why there is no TTL default in the code: a default would be an unmeasured
theory, and the proofs rely on a TTL of 0 so every consult reads the signal.
The measured cost that motivates the gate: with the TTL at 0 and the leg on,
every projector claim runs one bounded per-source probe (same-session p50 about
92 ms over about 13k claimable rows of one source at 10x replay scale, against
a harness budget of 100 ms) plus a latch read. A TTL trades that cost for
signal staleness, and the replay is what prices both.

## Rollout and rollback

Inert while the leg is off. With the thresholds unset no consult runs and no
paced release is issued, so none of the following can occur.

- **Roll every projector first.** Set the thresholds only after every projector
  and ingester is on the version that carries migration 333 and the source-paced
  retry kind. An older binary computes an item's projection claims as
  `attempt_count - admission_wait_attempts` and ignores `source_paced_waits`.
  An item paced N times and then handled by an older binary (a rollback, or a
  mixed roll with the leg on) sees N spent projection claims and dead-letters on
  its next genuine retryable failure.
- **Unset before rollback.** To roll back, unset the thresholds first and let
  the paced items drain (a released latch re-admits them within
  `max(GrantDelay, ReleaseAge)` plus jitter, see the Accounting paragraph in the
  fork rulings), then roll the binary. Rolling back with items still carrying a
  large `source_paced_waits` is the hazard above.
- **The hazard arrives sooner than it first looked.** The resume bound
  (`ReleaseAge`, Proposed 15 minutes) means a paced item is re-claimed about 4
  times an hour while its source stays slowed, and each re-claim raises
  `attempt_count` and `source_paced_waits` together. A long engagement reaches a
  large counter in a day, not a week.
- **Migration 333 takes a short lock.** It is `ALTER TABLE fact_work_items ADD
  COLUMN ... NOT NULL DEFAULT 0`: metadata-only on PostgreSQL 11 and later, but
  it takes `ACCESS EXCLUSIVE` and queues behind long `fact_work_items`
  transactions. It carries no `lock_timeout`, like the 325 precedent. Apply it
  in a quiet window.
