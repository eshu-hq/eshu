-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Drop the step-1 FIFO order index (#8469 step 3).
--
-- Migration 200 served the FIFO candidate's ORDER BY. The step-3 rewrite
-- replaces that candidate with the priority-then-rotation pick, which the
-- step-1 index cannot serve (claim_priority and the key intervene), so the
-- index is dead weight on a hot table's writes from here on. Dropped
-- CONCURRENTLY in its own file for the same transaction-block reason as
-- every other concurrent build.
DROP INDEX CONCURRENTLY IF EXISTS workflow_work_items_claim_order_idx;
