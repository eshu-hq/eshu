-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: the npm reachability file branch probes JS/TS file facts by payload repo_id. The
-- language predicate repeats the branch filter byte for byte (with the fact.
-- qualifier dropped) so the planner can prove the partial index, and moves the
-- language test out of a per-row detoast of every file of the repository.
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_jsts_reachability_repo_file_idx
    ON fact_records ((payload->>'repo_id'), scope_id, generation_id, fact_id)
    WHERE fact_kind = 'file'
      AND is_tombstone = FALSE
      AND LOWER(COALESCE(payload->'parsed_file_data'->>'language', payload->>'language', '')) IN ('javascript', 'jsx', 'typescript', 'tsx');
