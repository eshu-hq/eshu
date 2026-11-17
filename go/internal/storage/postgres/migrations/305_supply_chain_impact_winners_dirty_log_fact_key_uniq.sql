-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10827 (R2: Global Key-Ordered Versioned Upsert): the fact-kind unique
-- key of the winners dirty log. One row per pending fact recompute unit,
-- keyed by the five columns every candidate fact of one canonical key
-- shares (package_id, repository_id, subject_digest, c0, advisory_id).
-- The writer upserts (fact capture, retract, scanner triggers, audit queue)
-- target this index with ON CONFLICT ... WHERE kind = 'fact' DO UPDATE,
-- bumping only dirty_version; the settle's version recheck reads it.
-- Single statement: a concurrent build cannot run inside a transaction block.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_winners_dirty_log_fact_key_uniq
    ON supply_chain_impact_winners_dirty_log (package_id, repository_id, subject_digest, c0, advisory_id)
    WHERE kind = 'fact';
