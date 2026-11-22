-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10882: the projector source-paced wait counter for fact_work_items.
--
-- source_paced_waits counts how many times a projector work item released
-- its claim because the batch's source was latched slowed by the per-source
-- reducer backpressure leg (the wait outlasted its bound) rather than
-- because of a genuine projection failure or a database-protection
-- admission timeout. ProjectorQueue.Fail retries these without dead-lettering
-- and refunds them from the projection budget (projection claims are
-- attempt_count - admission_wait_attempts - source_paced_waits). They get a
-- counter of their own instead of sharing admission_wait_attempts so a
-- long engagement cannot spend the finite admission budget a later genuine
-- admission timeout owns: pacing must defer a slowed source's facts, never
-- mint admission_timeout dead letters (#10882 AC4). attempt_count stays a
-- pure monotonic claim fence.
--
-- NOT NULL DEFAULT 0 is metadata-only on PostgreSQL 11+ (fast default, no
-- table rewrite, no backfill): existing rows read 0, exactly the
-- never-paced state. IF NOT EXISTS converges databases that converge out
-- of order.
ALTER TABLE IF EXISTS fact_work_items
    ADD COLUMN IF NOT EXISTS source_paced_waits BIGINT NOT NULL DEFAULT 0;
