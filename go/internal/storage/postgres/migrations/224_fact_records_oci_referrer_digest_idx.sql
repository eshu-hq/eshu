-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: OCI image referrers (growth class) carry referrer_digest; the existing referrer index
-- leads on subject_digest.
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_oci_referrer_digest_idx
    ON fact_records ((payload->>'referrer_digest'), scope_id, generation_id, fact_kind, fact_id)
    WHERE fact_kind = 'oci_registry.image_referrer'
      AND is_tombstone = FALSE;
