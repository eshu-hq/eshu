-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Org fan-out position of one security-alert org target generation (#8946).
--
-- The fan-out reads at most 100 allowlisted repositories per claim and stops at
-- the first repository whose read fails for a reason that is not specific to it.
-- Each generation records the repository its fan-out started at, the repository
-- the next claim starts at, and, when a failed read at the last allowlist
-- position closed a lap, that last repository. The next claim then starts after
-- a failing repository instead of at the top, reads the repositories behind it,
-- and the start settles so the committer skips unchanged generations. A
-- repository behind a failing one can stay unread, labeled
-- org_repository_fanout_stopped, until the failing repository is fixed or
-- removed. All three are NULL for the top of the allowlist, for no lap, and for
-- a generation committed before this migration.
--
-- The columns ride on the read-state row that #8943 writes in the generation's
-- own commit transaction, so the position advances only when the generation
-- commits: a rollback or an unchanged-generation skip leaves it where it was.
-- The row already cascades from scope_generations, so retention needs no arm.
ALTER TABLE security_alert_target_read_states
    ADD COLUMN IF NOT EXISTS fanout_start_repository TEXT,
    ADD COLUMN IF NOT EXISTS fanout_next_repository TEXT,
    ADD COLUMN IF NOT EXISTS fanout_lap_repository TEXT;
