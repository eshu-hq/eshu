-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #11221: how many succeeded reducer work items ran again (a retry, attempt_count > 1, or a
-- reopen, reopened_at set), per domain, overall and in the last seven days. Read-only, aggregate
-- output only; run on the READ REPLICA in a session with default_transaction_read_only=on.
-- A re-saved item is a succeeded work item that ran more than once: attempt_count > 1 (a retry) or reopened_at set
-- (a reopen; the column keeps only the last reopen, so an item may have been reopened more than once). The rows are
-- filtered to the nine domains in the IN list below (the reducer domains that write fact_records and are named in
-- the record, plus supply_chain_impact, which the record excludes), all with stage = 'reducer'.
-- A reducer fact id includes the generation, so a same-id update needs the same intent to run
-- again for the same generation. Summary recorded on issue #11221.
SELECT domain,
       count(*) FILTER (WHERE status = 'succeeded') AS succeeded_items,
       count(*) FILTER (WHERE status = 'succeeded' AND (attempt_count > 1 OR reopened_at IS NOT NULL)) AS resaved_items,
       count(*) FILTER (WHERE status = 'succeeded' AND updated_at > now() - interval '7 days') AS succeeded_updated_7d,
       count(*) FILTER (WHERE status = 'succeeded' AND updated_at > now() - interval '7 days'
                          AND (attempt_count > 1 OR reopened_at IS NOT NULL)) AS resaved_7d
FROM fact_work_items
WHERE stage = 'reducer'
  AND domain IN ('eshu_search_document', 'code_drifted', 'workload_identity', 'platform_infra_materialization',
                 'aws_cloud_runtime_drift', 'container_image_identity', 'service_catalog_correlation',
                 'supply_chain_impact', 'observability_coverage_correlation')
GROUP BY 1
ORDER BY 2 DESC;
