-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Claim-path order index (#8469 step 1, no behavior change).
--
-- The claim candidate SELECT orders by COALESCE(visible_at, created_at),
-- created_at, work_item_id, but workflow_work_items_claimable_idx leads with
-- the plain visible_at column, so no index can serve the sort: every claim
-- scans all eligible rows and top-N sorts them (measured 7+ ms at 50,100
-- pending rows on a quiet box, growing with backlog size). This expression
-- index matches the ORDER BY exactly behind the three equality columns, so
-- the candidate becomes a first-row index serve (measured 0.03-0.05 ms,
-- flat in backlog size).
--
-- The visibility disjunct (visible_at IS NULL OR visible_at <= $3) stays a
-- residual filter: an OR cannot become a range scan on the expression, so the
-- index serves the sort, not the filter. The LIMIT 1 early stop is what keeps
-- the claim flat, and that holds for the all-eligible backlog; rows with a
-- future visible_at interleaved ahead of eligible ones are skipped per row.
-- CONCURRENTLY so the build does not block writes to the hot table; it
-- shares the file with no other statement because CONCURRENTLY cannot run
-- inside a transaction block.
CREATE INDEX CONCURRENTLY IF NOT EXISTS workflow_work_items_claim_order_idx
    ON workflow_work_items (
        collector_kind,
        collector_instance_id,
        status,
        (COALESCE(visible_at, created_at)),
        created_at,
        work_item_id
    );
