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
-- DDL only: the backfill that classes pre-existing backlog lives in 204, in
-- its own file so this ALTER's ACCESS EXCLUSIVE lock is released before the
-- backfill's pass (#7961).
ALTER TABLE workflow_work_items
    ADD COLUMN IF NOT EXISTS claim_priority INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS claim_ecosystem TEXT NOT NULL DEFAULT '';
