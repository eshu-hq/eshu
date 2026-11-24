-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 352_supply_chain_impact_winners_supports_scope_capture.sql (#10880 PR-C1 e):
-- the scope-delete capture reads the supports table instead of the finding
-- facts. When a scope row is deleted it stages the identifiers of the findings
-- the scope held, so the winners maintainer recomputes those keys; PR-C stops
-- writing the finding facts, so the identifiers must come from the supports.
-- The body is migration 310's with the row source swapped and nothing else (the
-- table, and the kind and tombstone predicates a support makes implicit); the
-- security shape is 310's: SECURITY DEFINER, the pinned search_path dance, a
-- PUBLIC revoke, and no conflict arbiter. The activation capture reads no fact
-- row and is not redefined.
--
-- Order: the trigger is BEFORE DELETE on ingestion_scopes, so it fires before the
-- foreign key cascade removes the scope's generations and their supports; the
-- supports are still there when it reads them. That order is a reading of the
-- trigger and cascade rules, proved by the live scope-delete capture test, which
-- has not run for this migration. A NEW migration file per issue #7002: never edit
-- an applied migration.
SELECT set_config('eshu.prev_search_path', current_setting('search_path'), true);

CREATE OR REPLACE FUNCTION supply_chain_impact_winners_capture_scope_delete()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $function$
DECLARE
    v_source TEXT;
BEGIN
    IF OLD.scope_id = 'operator:vulnerability_suppressions' THEN
        v_source := 'operator';
    ELSE
        v_source := 'activation';
    END IF;
    INSERT INTO supply_chain_impact_winners_dirty_staging
        (kind, source, package_id, repository_id, subject_digest, c0, advisory_id)
    SELECT DISTINCT 'fact', v_source,
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    FROM supply_chain_impact_finding_supports AS fact
    WHERE fact.scope_id = OLD.scope_id;
    RETURN OLD;
END;
$function$;

SELECT set_config('search_path', 'pg_catalog, ' || quote_ident(current_schema()) || ', pg_temp', true);
ALTER FUNCTION supply_chain_impact_winners_capture_scope_delete() SET search_path FROM CURRENT;

REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scope_delete() FROM PUBLIC;

SELECT set_config('search_path', current_setting('eshu.prev_search_path'), true);
