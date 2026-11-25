-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- 355_supply_chain_impact_winners_scope_capture_facts_and_supports.sql (#10880 PR-C1):
-- the scope-delete capture reads the finding facts AND the supports. Migration
-- 352 moved the read to the supports table for the post-fact-write world, but a
-- scope retired before its facts are backfilled stages nothing: the trigger is
-- the only capture on the purge path (the cascade hard-deletes the facts, so no
-- fact_retract mark is staged) and missed keys leave stale winners behind. The
-- retirement proofs seed finding facts directly, which is exactly the
-- not-yet-backfilled shape. Both arms project the same seven columns from the
-- fact payload verbatim (a support's payload IS the fact payload), so the UNION
-- collapses the dual-written overlap to one mark per key while covering
-- fact-only and support-only rows. The security shape is 310's/352's: SECURITY
-- DEFINER, the pinned search_path dance, a PUBLIC revoke, and no conflict
-- arbiter. The activation capture reads no fact row and is not redefined.
--
-- A NEW migration file per issue #7002: never edit an applied migration.
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
    FROM fact_records AS fact
    WHERE fact.scope_id = OLD.scope_id
      AND fact.fact_kind = 'reducer_supply_chain_impact_finding'
      AND fact.is_tombstone = FALSE
    UNION
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
