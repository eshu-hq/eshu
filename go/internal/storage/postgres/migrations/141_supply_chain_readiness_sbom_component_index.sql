-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7088: second half of the image-scoped SBOM join. Keep this concurrent
-- index isolated: the migration runner sends each file as one statement, and
-- PostgreSQL rejects multiple concurrent index statements in one execution.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_sbom_component_document_id_idx
    ON fact_records (
        (payload->>'document_id'),
        scope_id,
        generation_id
    )
    WHERE fact_kind = 'sbom.component'
      AND is_tombstone = FALSE;
