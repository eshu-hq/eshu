-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Fair-claim priority and ecosystem columns (#8469 step 2).
--
-- The owner decision normalises the fairness key to three segments and moves
-- target class out of the key into a real priority column, with a real
-- ecosystem column retiring the positional SPLIT_PART(fairness_key, ':', 4)
-- parse the registry status surface reads. Class leaves the key:
-- package_registry:<instance>:<class>:<ecosystem> becomes
-- package_registry:<instance>:<ecosystem>, and likewise for
-- vulnerability_intelligence with its trailing source segment.
--
-- Both columns are written once at enqueue and never touched by the claim
-- path, so the claim UPDATE's SET list and lock footprint do not change.
-- Backfill keeps pre-existing backlog classed: ecosystem from the old
-- fourth key segment, priority from the old third segment through the same
-- rank mapping the schedulers use (unknown classes rank last and never
-- preempt a known one).
ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS claim_priority INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS claim_ecosystem TEXT NOT NULL DEFAULT '';

UPDATE workflow_work_items
SET claim_ecosystem = NULLIF(BTRIM(SPLIT_PART(fairness_key, ':', 4)), '')
WHERE collector_kind = 'package_registry'
  AND claim_ecosystem = ''
  AND NULLIF(BTRIM(SPLIT_PART(fairness_key, ':', 4)), '') IS NOT NULL;

UPDATE workflow_work_items
SET claim_priority = CASE BTRIM(SPLIT_PART(fairness_key, ':', 3))
    WHEN 'configured_direct' THEN 0
    WHEN 'owned_package' THEN 1
    WHEN 'installed_os_package' THEN 2
    WHEN 'sbom_component' THEN 2
    WHEN 'broad' THEN 3
    ELSE 4
END
WHERE collector_kind IN ('package_registry', 'vulnerability_intelligence')
  AND claim_priority = 0
  AND fairness_key IS NOT NULL
  AND SPLIT_PART(fairness_key, ':', 3) <> ''
  AND SPLIT_PART(fairness_key, ':', 4) <> '';
