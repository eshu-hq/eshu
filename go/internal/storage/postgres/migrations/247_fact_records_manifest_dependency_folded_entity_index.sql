-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9568: the supply-chain impact manifest read matches entity_name with exact
-- equality, so rows whose stored name keeps registry case (NuGet
-- Newtonsoft.Json, pypi PyYAML, rubygems and composer mixed-case rows) never
-- load: the $2 candidate set is LowerASCII-folded. The read now folds the
-- stored name with LOWER(TRIM(entity_name) COLLATE "C"), byte-identical to Go
-- packageidentity.LowerASCII (ASCII A-Z only; U+0130 and U+212A are kept, so
-- lookalike rows cannot share a key with the ASCII package, #9584). The key
-- join stays the accuracy gate; this index only serves the wider prefilter.
--
-- On a disposable PostgreSQL 18.6 fixture (146,000 fact_records, 46,000 git
-- dependency rows with the QA case mix, 4,000 active scopes, 17-name advisory
-- set, three warm runs each): the exact predicate on the old index is a
-- scope-driven nested loop at 56.5-56.9 ms returning 0 rows (the defect); the
-- folded predicate with only this index is the same nested-loop shape with
-- 44,000 index searches and no sequential scan, returning the 26,620
-- registry-cased rows; with both indexes the planner serves the full set from
-- the old index package_manager prefix plus filter at 72-81 ms, and a
-- selective name (400 rows) seeks this index in 1.2-1.7 ms versus 16-21 ms
-- through the old prefix. Fixture timings, not a deployed claim.
--
-- One statement per file is required for a concurrent build; manifest writers
-- continue during it. The partial predicate matches
-- fact_records_active_package_dependency_entity_idx, so rows outside the
-- manifest read never enter this index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_active_package_dependency_folded_entity_idx
    ON fact_records (
        (payload->'entity_metadata'->>'package_manager'),
        ((LOWER(TRIM(fact_records.payload->>'entity_name') COLLATE "C")) COLLATE "default"),
        observed_at ASC,
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'content_entity'
      AND source_system = 'git'
      AND payload->>'entity_type' = 'Variable'
      AND payload->'entity_metadata'->>'config_kind' = 'dependency';
