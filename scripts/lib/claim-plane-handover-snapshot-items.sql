-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Token high-water snapshot for the claim-plane cutover handover (#9376):
-- per-item allocator maxima. Run against the CORE database after ClaimNext
-- is frozen. Deterministic LC_ALL=C order by key: per work item the
-- allocator high-water H (current_fencing_token), the live claim, and item
-- status. The handover script stores this as items.tsv next to fences.tsv
-- and hashes both; the hash names the snapshot the seed step must cite.
-- Read-only: no locks beyond the statement snapshot.
SELECT i.work_item_id,
       i.current_fencing_token,
       COALESCE(i.current_claim_id, ''),
       i.status
  FROM workflow_work_items AS i
 ORDER BY i.work_item_id;
