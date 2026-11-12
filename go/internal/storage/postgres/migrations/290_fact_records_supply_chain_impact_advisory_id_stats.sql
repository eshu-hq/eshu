-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10408: keep the holder fan-out lookup's advisory arm on its partial
-- index at the 200-id chunk bound. The lookup matches rows on cve_id,
-- advisory_id, or the stored advisory_group_members set; each arm probes up
-- to 200 ids per chunk. PostgreSQL does not use a partial index's
-- statistics for selectivity, so every advisory id is priced at the default
-- 0.5% of the kinds' rows and scalararraysel sums those shares toward a
-- full-table estimate, at which the planner walks the table instead of the
-- advisory lookup arm (the same ANY-array pathology migration 283 records
-- for the cve_id probes; the cve_id arm of this lookup is already covered
-- by that object, which is table-wide and applies here too).
--
-- Extended statistics on the same expression cover the whole table and are
-- used: with them the advisory probe estimates monotonically and stays on
-- the advisory index at the chunk bound. This is a single-expression
-- equality whose only missing input is the expression's own ndistinct/MCV,
-- which is exactly what the object supplies.
--
-- ANALYZE populates the new statistics immediately so the first lookup
-- after bootstrap plans correctly; autovacuum keeps them current
-- afterwards. A stale object only costs plan quality on this probe, it
-- cannot corrupt rows. IF NOT EXISTS for rerun safety.
CREATE STATISTICS IF NOT EXISTS fact_records_supply_chain_impact_advisory_id_stats
    ON ((payload->>'advisory_id'))
    FROM fact_records;
ANALYZE fact_records;
