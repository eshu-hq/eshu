-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: security_alert.repository_alert (growth class) carries cve_id; the v2 cve index
-- covers the vulnerability kinds only.
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_security_alert_cve_id_idx
    ON fact_records ((payload->>'cve_id'), scope_id, generation_id, fact_kind, fact_id)
    WHERE fact_kind = 'security_alert.repository_alert'
      AND is_tombstone = FALSE;
