-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9379: backfill half of the incident freshness lease migration (DML
-- half; the DDL half is migration 238).
--
-- Rows already sitting at 'claimed' when migration 238 ran predate the
-- lease entirely, so without a backfill they get claim_expires_at = NULL
-- and ReapExpiredTriggerClaims's "claim_expires_at IS NOT NULL" predicate
-- would skip them forever -- leaving exactly the already-stuck triggers
-- #9379 exists to rescue permanently stranded. Derive an expiry from
-- claimed_at plus the coordinator's default 5-minute lease so a claim that
-- is genuinely still in flight (claimed moments before this migration ran)
-- is not reclaimed out from under a live holder, while anything claimed
-- longer ago than 5 minutes is immediately eligible for the next reap
-- pass.
--
-- Idempotent: the UPDATE only touches rows still NULL, so a replay (an
-- untracked database's one full replay, or a retried interrupted apply)
-- re-derives nothing already derived. Rows claimed after 238 carry their
-- lease at claim time and never match the predicate.
--
-- Locking (#7961): this is a file of its own, so the runner's single
-- simple-query string holds no table-locking DDL alongside the backfill.
-- The UPDATE takes ROW EXCLUSIVE on incident_freshness_triggers --
-- readers are not blocked -- plus a row lock on each claimed row it
-- stamps.

UPDATE incident_freshness_triggers
SET claim_expires_at = COALESCE(claimed_at, updated_at) + INTERVAL '5 minutes'
WHERE status = 'claimed' AND claim_expires_at IS NULL;
