-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- content_file_secret_lines_state gains policy_version (#8833 ruling-3).
--
-- It records which derivation policy the side table was built under: 138 for
-- every existing row. Migration 235 replaces the derivation function and moves
-- the state to not_built only while policy_version is below 235, so a
-- re-applied 235 is a no-op. The column is its own migration because the
-- migration-immutability gate refuses an ALTER TABLE and bulk DML in one file.
-- The ALTER takes ACCESS EXCLUSIVE on a one-row table for under a millisecond;
-- the runner's lock_timeout bounds any wait.
ALTER TABLE content_file_secret_lines_state
    ADD COLUMN IF NOT EXISTS policy_version INTEGER NOT NULL DEFAULT 138;
