// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// generationRetentionRowCountsSupportsArm counts
// supply_chain_impact_finding_supports, the #10880 PR-B1 per-support table.
// Its generation_id foreign key cascades from scope_generations, so the arm
// is generation-only like the fact_work_items arm (#10525): it charges exactly
// the rows the final scope_generations delete removes, and it probes the
// (generation_id) btree migration 336 adds for the cascade, so the cost
// follows the batch, not the table. The retention key index check refuses the
// cycle while that index is unusable. It lives in its own file because the
// composed row-counts query must stay under the 500-line file cap; the
// cascade-coverage test reads the composed constant's value, so the split
// changes nothing it checks.
const generationRetentionRowCountsSupportsArm = `
UNION ALL
SELECT candidate.generation_id, 'supply_chain_impact_finding_supports' AS table_name, COUNT(support.fact_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN supply_chain_impact_finding_supports AS support
  ON support.generation_id = candidate.generation_id
GROUP BY candidate.generation_id
`
