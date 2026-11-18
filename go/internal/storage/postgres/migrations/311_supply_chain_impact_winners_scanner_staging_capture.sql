-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10837 direction C (hot-key coalescing): the migration-272a scanner
-- capture triggers append to the staging queue (migration 309) instead of
-- upserting the versioned dirty log. The statements read the transition
-- tables, join the finding facts, and stage every distinct five-tuple with
-- one plain INSERT; the tick's collapse restores the version math.
--
-- Public-only like 272a and 308: the two tables belong to the
-- database-global scanner authority layout, and the functions name
-- public-qualified tables with the pinned search_path 272a uses. Same
-- signatures, same SECURITY DEFINER, same PUBLIC revoke; only the INSERT
-- targets change, the ON CONFLICT arms go away, and the ORDER BY clauses
-- that fed the upserts' lock order are dropped.
CREATE OR REPLACE FUNCTION supply_chain_impact_winners_capture_scanner()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public, pg_temp AS $function$
BEGIN
    INSERT INTO public.supply_chain_impact_winners_dirty_staging
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT DISTINCT 'fact', 'scanner',
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    FROM new_rows
    JOIN public.fact_records AS fact ON fact.fact_id = new_rows.finding_fact_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding';
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION supply_chain_impact_winners_capture_scanner_delete()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public, pg_temp AS $function$
BEGIN
    INSERT INTO public.supply_chain_impact_winners_dirty_staging
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT DISTINCT 'fact', 'scanner',
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    FROM old_rows
    JOIN public.fact_records AS fact ON fact.fact_id = old_rows.finding_fact_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding';
    RETURN NULL;
END;
$function$;

REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner_delete() FROM PUBLIC;
