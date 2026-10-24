-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: package_registry.package_version (growth class) carries purl with no purl-leading index.
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_package_version_purl_idx
    ON fact_records ((payload->>'purl'), scope_id, generation_id, fact_kind, fact_id)
    WHERE fact_kind = 'package_registry.package_version'
      AND is_tombstone = FALSE;
