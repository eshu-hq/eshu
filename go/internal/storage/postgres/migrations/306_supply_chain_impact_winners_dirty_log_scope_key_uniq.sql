-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10827 (R2: Global Key-Ordered Versioned Upsert): the scope-kind unique
-- key of the winners dirty log. One row per pending scope recompute unit,
-- keyed by the (scope_id, generation_id) pair the keyed tick expands. The
-- activation trigger upserts into it with ON CONFLICT ... WHERE kind =
-- 'scope' DO UPDATE, bumping only dirty_version. Single statement: a
-- concurrent build cannot run inside a transaction block.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_winners_dirty_log_scope_key_uniq
    ON supply_chain_impact_winners_dirty_log (scope_id, generation_id)
    WHERE kind = 'scope';
