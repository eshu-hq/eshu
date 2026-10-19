-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Fair-claim backlog backfill (#8469 step 2).
--
-- Classes pre-existing backlog under migration 201's columns: ecosystem from
-- the old fourth key segment, priority from the old third segment through
-- the same rank mapping the schedulers use (unknown classes rank last and
-- never preempt a known one).
--
-- Idempotent: the ecosystem UPDATE only touches rows still '' whose key
-- still carries a fourth segment, and the priority UPDATE only touches rows
-- still 0 whose key still carries a third and fourth segment, so a replay
-- of this file (an untracked database's one full replay, or a retried
-- interrupted apply) re-classes nothing already classed. Rows the schedulers
-- enqueue after 201 carry their class at write time and never match either
-- predicate.
--
-- Locking: this is a file of its own, so the runner's single simple-query
-- string holds no table-locking DDL alongside the backfill (#7961). Each
-- UPDATE takes ROW EXCLUSIVE on workflow_work_items -- readers are not
-- blocked -- plus a row lock on each row it classes. The claim path never
-- writes these columns, so concurrent claims do not contend with this
-- statement; a concurrent enqueue of a legacy-shaped row during the upgrade
-- waits on that row lock, bounded by the runner's lock_timeout and retry.
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
