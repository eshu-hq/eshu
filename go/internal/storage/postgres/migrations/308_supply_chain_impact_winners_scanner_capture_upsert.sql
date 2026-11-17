-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10827 (R2: Global Key-Ordered Versioned Upsert): the migration-272a
-- scanner capture triggers become key-ordered upserts. The statements read
-- the transition tables, join the finding facts, and mark every distinct
-- five-tuple; the SELECT's key order feeds the upsert in the global lock
-- order, and a re-mark bumps only dirty_version, preserving the first
-- captured_at. Lands after the 305 fact unique index, which the ON CONFLICT
-- clause below needs at every firing.
--
-- Public-only like 272a: the two tables belong to the database-global
-- scanner authority layout, and the functions name public-qualified tables
-- with the pinned search_path 272a uses. Same signatures, same SECURITY
-- DEFINER, same PUBLIC revoke; only the INSERT statements change.
CREATE OR REPLACE FUNCTION supply_chain_impact_winners_capture_scanner()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public, pg_temp AS $function$
BEGIN
    INSERT INTO public.supply_chain_impact_winners_dirty_log
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT DISTINCT 'fact', 'scanner',
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    FROM new_rows
    JOIN public.fact_records AS fact ON fact.fact_id = new_rows.finding_fact_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
    ORDER BY
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'
    DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1;
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION supply_chain_impact_winners_capture_scanner_delete()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public, pg_temp AS $function$
BEGIN
    INSERT INTO public.supply_chain_impact_winners_dirty_log
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT DISTINCT 'fact', 'scanner',
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    FROM old_rows
    JOIN public.fact_records AS fact ON fact.fact_id = old_rows.finding_fact_id
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
    ORDER BY
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'
    DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1;
    RETURN NULL;
END;
$function$;

REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner_delete() FROM PUBLIC;
