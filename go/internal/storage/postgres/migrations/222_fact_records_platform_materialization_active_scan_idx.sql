-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: reducer_platform_materialization (bounded class) is read by one full-filter
-- statement; no index named the kind.
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_platform_materialization_active_scan_idx
    ON fact_records (scope_id, generation_id, fact_id)
    WHERE fact_kind = 'reducer_platform_materialization'
      AND is_tombstone = FALSE;
