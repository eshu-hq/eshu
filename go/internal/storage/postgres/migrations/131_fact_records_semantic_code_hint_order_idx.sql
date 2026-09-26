-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7237: semantic.code_hint pages order active facts by observed_at and
-- fact_id. Without a kind-selective access path, PostgreSQL skip-scans the
-- multi-column fact_records_scope_generation_idx thousands of times even
-- when this kind is empty. The literal kind and tombstone predicates match
-- buildSemanticEvidenceSQL; populated-page performance remains to be measured.
-- Only active code-hint facts pay index maintenance. CONCURRENTLY preserves
-- writers during bootstrap; this file contains one statement so the migration
-- coordinator can execute it outside a transaction.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_semantic_code_hint_order_idx
    ON fact_records (observed_at DESC, fact_id DESC)
    WHERE fact_kind = 'semantic.code_hint'
      AND is_tombstone = FALSE;
