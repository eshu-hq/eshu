-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10827 (R2: Global Key-Ordered Versioned Upsert): the migration-272
-- capture triggers become key-ordered upserts. Every re-mark of a queued key
-- bumps dirty_version instead of appending a row, so the trigger paths
-- collapse exactly like the writer paths; the first mark keeps its
-- captured_at because the conflict arm updates only the version. This file
-- lands after the 305/306 unique indexes: the ON CONFLICT clauses below
-- need those indexes at every trigger firing, so replacing the functions
-- first would fail live writers between the two builds.
--
-- The shape mirrors 272 exactly: same signatures, same SECURITY DEFINER, same
-- pinned search_path dance (272's functions resolve the log in the session
-- schema, so ALTER ... SET search_path FROM CURRENT converges every schema),
-- same PUBLIC revoke. Only the INSERT statements change. As with 272, the
-- two functions below apply in every schema, so this file is not public-only
-- (the scanner pair rides 308 instead).
SELECT set_config('eshu.prev_search_path', current_setting('search_path'), true);

CREATE OR REPLACE FUNCTION supply_chain_impact_winners_capture_activation()
RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $function$
DECLARE
    v_source TEXT;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.active_generation_id IS NOT DISTINCT FROM OLD.active_generation_id THEN
        RETURN NEW;
    END IF;
    IF NEW.scope_id = 'operator:vulnerability_suppressions' THEN
        v_source := 'operator';
    ELSE
        v_source := 'activation';
    END IF;
    -- One statement over both keys, in scope-key order: two statements would
    -- take the OLD row lock before the NEW one even when NEW sorts first.
    INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id)
    SELECT 'scope', v_source, k.scope_id, k.generation_id
    FROM (
        SELECT OLD.scope_id AS scope_id, OLD.active_generation_id AS generation_id
        WHERE TG_OP = 'UPDATE' AND OLD.active_generation_id IS NOT NULL
        UNION ALL
        SELECT NEW.scope_id, NEW.active_generation_id
        WHERE NEW.active_generation_id IS NOT NULL
    ) AS k
    ORDER BY k.scope_id, k.generation_id
    ON CONFLICT (scope_id, generation_id) WHERE kind = 'scope'
    DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1;
    RETURN NEW;
END;
$function$;

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
    INSERT INTO supply_chain_impact_winners_dirty_log
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
    ORDER BY
           COALESCE(fact.payload->>'package_id', ''),
           COALESCE(fact.payload->>'repository_id', ''),
           COALESCE(fact.payload->>'subject_digest', ''),
           COALESCE(NULLIF(fact.payload->>'cve_id', ''), NULLIF(fact.payload->>'advisory_id', ''), ''),
           COALESCE(fact.payload->>'advisory_id', '')
    ON CONFLICT (package_id, repository_id, subject_digest, c0, advisory_id) WHERE kind = 'fact'
    DO UPDATE SET dirty_version = supply_chain_impact_winners_dirty_log.dirty_version + 1;
    RETURN OLD;
END;
$function$;

SELECT set_config('search_path', 'pg_catalog, ' || quote_ident(current_schema()) || ', pg_temp', true);
ALTER FUNCTION supply_chain_impact_winners_capture_activation() SET search_path FROM CURRENT;
ALTER FUNCTION supply_chain_impact_winners_capture_scope_delete() SET search_path FROM CURRENT;

REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_activation() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scope_delete() FROM PUBLIC;

SELECT set_config('search_path', current_setting('eshu.prev_search_path'), true);
