-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9805: scanner capture for the supply-chain impact winners dirty log
-- (migration 272). Classification and association rows decide whether a
-- finding fact is a source candidate (the winners select LEFT JOINs both), so
-- an insert, a classification update or a delete of either changes a winner.
-- One statement trigger per event reads the transition table, joins the
-- finding facts, and appends their five identifier columns. An INSERT ... ON
-- CONFLICT DO NOTHING row that conflicts is absent from the transition table
-- and captures nothing.
--
-- The two tables belong to the database-global scanner authority layout
-- (migrations 241a and later), which an isolated-schema test bootstrap skips,
-- so this file is public-only (embed.go publicOnlyMigrationPaths) like the
-- scanner migrations it depends on. It follows their convention: the functions
-- run as their owner with a pinned search_path (pg_temp last) and public-qualified tables,
-- because the publish function that fires them is SECURITY DEFINER and the
-- runtime-writer login has no base-table privilege on the log.
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
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding';
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
    WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding';
    RETURN NULL;
END;
$function$;

CREATE OR REPLACE TRIGGER scanner_classifications_winners_dirty_insert
AFTER INSERT ON scanner_image_result_fact_classifications
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION supply_chain_impact_winners_capture_scanner();

CREATE OR REPLACE TRIGGER scanner_classifications_winners_dirty_update
AFTER UPDATE ON scanner_image_result_fact_classifications
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION supply_chain_impact_winners_capture_scanner();

CREATE OR REPLACE TRIGGER scanner_classifications_winners_dirty_delete
AFTER DELETE ON scanner_image_result_fact_classifications
REFERENCING OLD TABLE AS old_rows
FOR EACH STATEMENT EXECUTE FUNCTION supply_chain_impact_winners_capture_scanner_delete();

CREATE OR REPLACE TRIGGER scanner_associations_winners_dirty_insert
AFTER INSERT ON scanner_image_result_fact_associations
REFERENCING NEW TABLE AS new_rows
FOR EACH STATEMENT EXECUTE FUNCTION supply_chain_impact_winners_capture_scanner();

CREATE OR REPLACE TRIGGER scanner_associations_winners_dirty_delete
AFTER DELETE ON scanner_image_result_fact_associations
REFERENCING OLD TABLE AS old_rows
FOR EACH STATEMENT EXECUTE FUNCTION supply_chain_impact_winners_capture_scanner_delete();

-- Trigger functions: a direct call fails and a trigger fires without the caller
-- holding EXECUTE. Withdraw the default PUBLIC execute privilege as the earlier
-- SECURITY DEFINER migrations do.
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scanner_delete() FROM PUBLIC;
