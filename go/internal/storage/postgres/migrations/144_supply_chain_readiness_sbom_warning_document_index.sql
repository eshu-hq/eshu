-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7088: keep SBOM warning matching by document_id across active scopes.
-- The document collector can reuse one document_id in multiple scopes, so
-- a scope/generation equality join would change warning truth. This partial
-- index lets the existing document-id join probe only matching warning facts.
-- On a disposable PostgreSQL 18 shim with 513 target documents and 513
-- unrelated active warnings, the exact custom/generic query fell from
-- 830.324/818.126 ms to 1.973/3.791 ms; the cross-scope result stayed one.
-- These are synthetic plans, not deployed endpoint p95 evidence.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_sbom_warning_document_id_target_reason_idx
    ON fact_records ((payload->>'document_id'))
    WHERE fact_kind = 'sbom.warning'
      AND is_tombstone = FALSE
      AND payload->>'reason' IN ('unsupported_field', 'malformed_document');
