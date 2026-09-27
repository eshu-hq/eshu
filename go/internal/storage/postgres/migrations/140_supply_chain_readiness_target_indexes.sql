-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7088: resolve image-scoped package identities through the owning SBOM
-- document. Component payloads carry document_id, not subject_digest. This
-- partial index begins the active-generation join. A disposable PostgreSQL
-- 18.6 shim returned identical 15 component rows; EXPLAIN ANALYZE fell from
-- 2362.449 ms to 0.297 ms on 6000 documents and 18000 components.
-- Deployment-scale p95 remains a separate proof.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_sbom_document_subject_digest_idx
    ON fact_records (
        (payload->>'subject_digest'),
        scope_id,
        generation_id,
        (payload->>'document_id')
    )
    WHERE fact_kind = 'sbom.document'
      AND is_tombstone = FALSE;
