-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10671: the projector admission-wait counter for fact_work_items.
--
-- admission_wait_attempts counts how many times a projector work item failed
-- with a backpressure admission-timeout (reducer queue above the admission
-- marks) as opposed to a genuine projection failure. ProjectorQueue.Fail
-- retries admission-timeout failures against a dedicated admission budget
-- (default 48 waits) instead of the shared projection attempt budget, so a
-- chunk whose queue stays above the mark for hours (chunk-2/chunk-3 died
-- after 3x5min waits on the shared budget of 3) retries instead of
-- dead-lettering, while the budget stays finite per #10485 (no unbounded
-- worker parking). The refund rule (projection budget measured as
-- attempt_count - admission_wait_attempts) keeps attempt_count a pure
-- monotonic claim fence.
--
-- NOT NULL DEFAULT 0 is metadata-only on PostgreSQL 11+ (fast default, no
-- table rewrite, no backfill): existing rows read 0, which is exactly the
-- never-waited state. IF NOT EXISTS converges databases that converge
-- out of order.
ALTER TABLE IF EXISTS fact_work_items
    ADD COLUMN IF NOT EXISTS admission_wait_attempts BIGINT NOT NULL DEFAULT 0;
