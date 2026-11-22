-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11182: the replay retry allowance for fact_work_items.
--
-- replay_attempt_baseline records the attempt_count a projector row had when
-- an operator replay moved it from dead_letter or failed back to pending.
-- ProjectorQueue.Fail measures the projection budget as attempt_count -
-- admission_wait_attempts - replay_attempt_baseline, so a replayed row gets
-- the same three projection retries a new row gets. Replay also resets
-- admission_wait_attempts to 0, so the 48-wait admission budget starts fresh.
-- attempt_count itself stays a pure monotonic claim fence: resetting it would
-- let a stale holder of an old claim match a new claim at the same attempt
-- (lease_owner is shared by every projector worker).
--
-- The two nullable fact_replay_events columns record, per replayed row, the
-- baseline the replay set and the admission_wait_attempts the row had before
-- the replay reset it. Events written before this migration read NULL.
--
-- NOT NULL DEFAULT 0 is metadata-only on PostgreSQL 11+ (fast default, no
-- table rewrite, no backfill): existing rows read 0, which keeps today's
-- budget for every row that was never replayed. Nullable columns with no
-- default are metadata-only too. IF NOT EXISTS converges databases that
-- converge out of order.
ALTER TABLE IF EXISTS fact_work_items
    ADD COLUMN IF NOT EXISTS replay_attempt_baseline BIGINT NOT NULL DEFAULT 0;

ALTER TABLE IF EXISTS fact_replay_events
    ADD COLUMN IF NOT EXISTS replay_attempt_baseline BIGINT NULL,
    ADD COLUMN IF NOT EXISTS prior_admission_wait_attempts BIGINT NULL;
