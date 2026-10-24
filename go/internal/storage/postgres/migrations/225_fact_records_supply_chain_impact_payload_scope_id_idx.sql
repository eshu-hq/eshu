-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: ci_cd run and package consumption correlations (growth class) carry payload
-- scope_id, a repository arm, with no index leading on it.
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_supply_chain_impact_payload_scope_id_idx
    ON fact_records ((payload->>'scope_id'), scope_id, generation_id, fact_kind, fact_id)
    WHERE fact_kind IN ('reducer_package_consumption_correlation', 'reducer_ci_cd_run_correlation')
      AND is_tombstone = FALSE;
