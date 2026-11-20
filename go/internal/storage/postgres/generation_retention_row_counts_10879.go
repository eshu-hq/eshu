// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// generationRetentionRowCountsLeaseArm counts fact_work_item_leases, the
// #10879 lease sidecar: lease rows hang off fact_work_items by
// work_item_id, so the arm joins through the work item's (scope_id,
// generation_id) probe and then the lease PRIMARY KEY. Both probes stay
// index-only and the cost follows the batch, like the #8300 fingerprint
// arm. It lives in its own file because the composed row-counts query
// must stay under the 500-line file cap; the cascade-coverage test reads
// the composed constant's value, so the split changes nothing it checks.
const generationRetentionRowCountsLeaseArm = `
UNION ALL
SELECT candidate.generation_id, 'fact_work_item_leases' AS table_name, COUNT(lease.work_item_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_work_items AS work
  ON work.scope_id = generation.scope_id
 AND work.generation_id = candidate.generation_id
LEFT JOIN fact_work_item_leases AS lease
  ON lease.work_item_id = work.work_item_id
GROUP BY candidate.generation_id
`
