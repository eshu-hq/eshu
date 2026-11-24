-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 353_supply_chain_impact_winners_supports_scanner_capture.sql (#10880 PR-C1 e):
-- the scanner capture pair reads the supports table instead of the finding facts.
-- When a scanner classification or association row appears or goes, the pair
-- stages the identifiers of the finding it names, through the transition table
-- joined to the finding's row; the join now goes to the support of that finding.
-- The bodies are migration 311's with the row source swapped and nothing else.
-- Public-only like 272a, 308 and 311: the two scanner tables belong to the
-- database-global scanner authority layout, and the functions name public-qualified
-- tables with a pinned search_path. A NEW migration file per issue #7002: never edit
-- an applied migration.
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
    JOIN public.supply_chain_impact_finding_supports AS fact ON fact.fact_id = new_rows.finding_fact_id;
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
    JOIN public.supply_chain_impact_finding_supports AS fact ON fact.fact_id = old_rows.finding_fact_id;
    RETURN NULL;
END;
$function$;

REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner_delete() FROM PUBLIC;
