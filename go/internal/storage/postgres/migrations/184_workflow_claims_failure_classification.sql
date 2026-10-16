-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7926: a terminal provider failure (for example Jira HTTP 400 on the
-- search endpoint) collapsed to "jira provider failure: terminal" in
-- workflow_claims.failure_class/failure_message, so the next 400 was
-- indistinguishable in the database. This adds the bounded structured
-- classification alongside the message: the sanitized operation token, the
-- provider HTTP status code, and the terminal flag. Claim handling only
-- writes sanitized [a-z0-9_] operation tokens, numeric statuses, and the
-- route truth for the flag -- never URLs, response bodies, or credentials.
--
-- Nullable and without defaults, so ADD COLUMN is a catalog-only change with
-- no table rewrite; its ACCESS EXCLUSIVE lock is held only for that catalog
-- update and bounded by the runner's lock_timeout and retry (schema.go), as
-- in migrations 148 and 149. No index (operators filter terminal rows by the
-- existing status and family indexes, then read these columns), no backfill
-- (a legacy row cannot say which operation or status failed it; NULL reads
-- as unknown), and no CHECK. Keep this the only statement in the file.
ALTER TABLE workflow_claims
    ADD COLUMN IF NOT EXISTS failure_operation TEXT NULL,
    ADD COLUMN IF NOT EXISTS failure_status_code INTEGER NULL,
    ADD COLUMN IF NOT EXISTS failure_terminal BOOLEAN NULL;
