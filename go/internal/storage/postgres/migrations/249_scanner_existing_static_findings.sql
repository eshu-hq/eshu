-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The typed producer has not shipped before this migration. Refuse the
-- static backfill if an earlier branch already admitted derived targets;
-- treating an unclassified derived finding as static would cross boundaries.
DO $scanner_static_backfill_guard$
BEGIN
    IF EXISTS (SELECT 1 FROM scanner_image_targets LIMIT 1) THEN
        RAISE EXCEPTION 'scanner static finding backfill requires no derived targets';
    END IF;
END
$scanner_static_backfill_guard$;

INSERT INTO scanner_image_result_fact_classifications
    (finding_fact_id,source_class,lifecycle,scan_scope_id)
SELECT fact_id,'static',
       CASE WHEN is_tombstone THEN 'retracted' ELSE 'active' END,''
FROM fact_records
WHERE fact_kind='reducer_supply_chain_impact_finding'
ON CONFLICT (finding_fact_id) DO NOTHING;
