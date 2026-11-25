-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10972: the canonical-winners identifier index for the staged-audit
-- single-scan range statement (#11131). The range diffs each recomputed
-- identifier against the stored winners in the same key interval, ordered by
-- (package_id, repository_id, subject_digest, c0, advisory_id). Without this
-- index the stored side of every range falls back to a sequential scan of
-- the winners table. The five key expressions are the stored-winner
-- projections of the keyed winners index (migration 291): after stripping
-- the `w.` qualifier they are byte-identical to
-- winnersAuditRangeStoredIdentExprs, so the range predicate and the ORDER
-- BY are index conditions and index order. No existing canonical-winners
-- index shares this leading column set (finding, priority, status,
-- severity, and repository indexes lead elsewhere), and the winners table
-- is rewritten only by the resweep/rebuild writers, so the added write
-- cost lands off the finding-ingest path. A concurrent build cannot run
-- inside a transaction block, so this stays a single statement.
--
-- Additive and idempotent. Older binaries never read the table through this
-- index; a rollback leaves it unused.
CREATE INDEX CONCURRENTLY IF NOT EXISTS supply_chain_impact_winners_canonical_ident_idx
    ON supply_chain_impact_canonical_winners (
        package_id,
        repository_id,
        subject_digest,
        (COALESCE(NULLIF(cve_id, ''), NULLIF(advisory_id, ''), '')),
        advisory_id
    );
