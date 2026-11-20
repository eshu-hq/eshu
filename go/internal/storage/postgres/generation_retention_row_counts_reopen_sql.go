// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// generationRetentionRowCountsReopenLegs counts the reopen fingerprint and
// epoch sidecars, one UNION ALL leg per table, each a cascade grandchild
// through fact_work_items by work_item_id (the #8300 shape). It lives here
// instead of beside the other legs so generation_retention_row_counts_sql.go
// stays under the 500-line file cap; the legs concatenate into
// generationRetentionRowCountsQuery byte-identically (the query guard pins
// the exact strings). A new reopen sidecar adds its leg here, its guard
// strings beside the others, and its zero row in the migrated-schema census.
const generationRetentionRowCountsReopenLegs = `UNION ALL
SELECT candidate.generation_id, 'deployable_unit_reopen_fingerprints' AS table_name, COUNT(fp.work_item_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_work_items AS work
  ON work.scope_id = generation.scope_id
 AND work.generation_id = candidate.generation_id
LEFT JOIN deployable_unit_reopen_fingerprints AS fp
  ON fp.work_item_id = work.work_item_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'aws_drift_reopen_fingerprints' AS table_name, COUNT(fp.work_item_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_work_items AS work
  ON work.scope_id = generation.scope_id
 AND work.generation_id = candidate.generation_id
LEFT JOIN aws_drift_reopen_fingerprints AS fp
  ON fp.work_item_id = work.work_item_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'container_identity_reopen_epochs' AS table_name, COUNT(fp.work_item_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_work_items AS work
  ON work.scope_id = generation.scope_id
 AND work.generation_id = candidate.generation_id
LEFT JOIN container_identity_reopen_epochs AS fp
  ON fp.work_item_id = work.work_item_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'workload_materialization_reopen_fingerprints' AS table_name, COUNT(fp.work_item_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_work_items AS work
  ON work.scope_id = generation.scope_id
 AND work.generation_id = candidate.generation_id
LEFT JOIN workload_materialization_reopen_fingerprints AS fp
  ON fp.work_item_id = work.work_item_id
GROUP BY candidate.generation_id
UNION ALL
SELECT candidate.generation_id, 'service_catalog_correlation_reopen_fingerprints' AS table_name, COUNT(fp.work_item_id) AS row_count
FROM generation_retention_row_counts AS candidate
LEFT JOIN scope_generations AS generation
  ON generation.generation_id = candidate.generation_id
LEFT JOIN fact_work_items AS work
  ON work.scope_id = generation.scope_id
 AND work.generation_id = candidate.generation_id
LEFT JOIN service_catalog_correlation_reopen_fingerprints AS fp
  ON fp.work_item_id = work.work_item_id
GROUP BY candidate.generation_id`
