-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7926: work-item mirror of migration 184. workflow_work_items carries the
-- last failure per item (last_failure_class/last_failure_message) so
-- operators can read terminal classification without joining workflow_claims;
-- this adds the same bounded operation token, HTTP status code, and terminal
-- flag there. FailClaimTerminal and FailClaimRetryable write both rows in one
-- fenced statement; CompleteClaim and ReleaseClaim clear all six columns so
-- stale classification never survives completion or release.
--
-- Nullable and without defaults, so ADD COLUMN is a catalog-only change with
-- no table rewrite; its ACCESS EXCLUSIVE lock is held only for that catalog
-- update and bounded by the runner's lock_timeout and retry (schema.go), as
-- in migrations 148 and 149. No index, no backfill (NULL reads as unknown),
-- and no CHECK. Keep this the only statement in the file.
ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS last_failure_operation TEXT NULL,
    ADD COLUMN IF NOT EXISTS last_failure_status_code INTEGER NULL,
    ADD COLUMN IF NOT EXISTS last_failure_terminal BOOLEAN NULL;
