-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Fair-claim rotation index (#8469 step 3).
--
-- The priority-then-rotation claim discovers distinct (priority, key) pairs
-- with a loose scan and probes each pair's oldest head, so its cost scales
-- with the distinct-key count rather than the backlog. This composite serves
-- all three accesses behind the claim's equality prefix: the recursive
-- distinct-pair walk ordered by (priority, key), the per-pair head probe
-- ordered by the effective visible time, and the NULL-safe key comparison
-- (COALESCE keeps keyless rows in one rotation slot instead of dropping
-- them from the walk). CONCURRENTLY so the build does not block writes to
-- the hot table; it shares the file with no other statement because
-- CONCURRENTLY cannot run inside a transaction block.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_work_items_fair_claim_idx
    ON workflow_work_items (
        collector_kind,
        collector_instance_id,
        status,
        claim_priority,
        (COALESCE(fairness_key, '')),
        (COALESCE(visible_at, created_at)),
        created_at,
        work_item_id
    );
