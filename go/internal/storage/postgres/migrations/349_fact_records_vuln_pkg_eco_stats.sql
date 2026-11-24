-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #11376: keep the package-keyed holder read's ecosystem arm on its partial
-- index at any scope count. The read filters
-- LOWER(BTRIM(payload->>'ecosystem')) = ANY($1); PostgreSQL does not use a
-- partial index's statistics for selectivity, so every spelling is priced
-- at the default and scalararraysel sums those shares, at which the
-- planner prefers a per-scope probe (or the whole-index bitmap) with the
-- ecosystem as a heap filter — the breach shape (the same ANY-array
-- pathology migrations 283 and 290 record for the cve_id and advisory_id
-- arms of the holder lookup).
--
-- Extended statistics on the same expression cover the whole table and are
-- used: with them the estimate follows the expression's own ndistinct/MCV
-- and the planner prices the eco-anchored bitmap below the probe shapes.
-- This is a single-expression equality whose only missing input is the
-- expression's own ndistinct/MCV, which is exactly what the object
-- supplies. Do NOT widen this to a multivariate object: the 283 proof
-- records that widening bought nothing while adding ANALYZE cost.
--
-- ANALYZE populates the new statistics immediately so the first lookup
-- after bootstrap plans correctly; autovacuum keeps them current
-- afterwards. A stale object only costs plan quality on this probe, it
-- cannot corrupt rows. IF NOT EXISTS for rerun safety.
CREATE STATISTICS IF NOT EXISTS fact_records_vuln_pkg_eco_stats
    ON (((LOWER(BTRIM(payload->>'ecosystem') COLLATE "C")) COLLATE "default"))
    FROM fact_records;
ANALYZE fact_records;
