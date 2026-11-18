-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10837 direction C (hot-key coalescing): the migration-272 capture
-- triggers append to the staging queue (migration 309) instead of upserting
-- the versioned dirty log. Every re-mark appends one staged row with its
-- own captured_at; the maintainer tick collapses each wave into the log
-- with one key-ordered GROUP BY merge-upsert, so concurrent pointer flips
-- over the same scope never wait on the log's conflict arbiter.
--
-- The shape mirrors 307 exactly: same signatures, same SECURITY DEFINER,
-- same pinned search_path dance (272's functions resolve the log in the
-- session schema, so ALTER ... SET search_path FROM CURRENT converges every
-- schema), same PUBLIC revoke. Only the INSERT targets change, and the
-- ON CONFLICT arms go away: the staging has no unique index, so there is
-- nothing to arbitrate on. SELECT DISTINCT is kept (one staged row per
-- distinct key per firing, not per finding row); the ORDER BY feeds it no
-- longer served and is dropped. As with 272 and 307, the two functions
-- below apply in every schema, so this file is not public-only (the scanner
-- pair rides 311 instead).
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
    -- One plain multi-row INSERT over both keys: no arbiter, no lock order
    -- to keep, and the tick's collapse restores the version math.
    INSERT INTO supply_chain_impact_winners_dirty_staging (kind, source, scope_id, generation_id)
    SELECT 'scope', v_source, k.scope_id, k.generation_id
    FROM (
        SELECT OLD.scope_id AS scope_id, OLD.active_generation_id AS generation_id
        WHERE TG_OP = 'UPDATE' AND OLD.active_generation_id IS NOT NULL
        UNION ALL
        SELECT NEW.scope_id, NEW.active_generation_id
        WHERE NEW.active_generation_id IS NOT NULL
    ) AS k;
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
      AND fact.is_tombstone = FALSE;
    RETURN OLD;
END;
$function$;

SELECT set_config('search_path', 'pg_catalog, ' || quote_ident(current_schema()) || ', pg_temp', true);
ALTER FUNCTION supply_chain_impact_winners_capture_activation() SET search_path FROM CURRENT;
ALTER FUNCTION supply_chain_impact_winners_capture_scope_delete() SET search_path FROM CURRENT;

REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_activation() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scope_delete() FROM PUBLIC;

SELECT set_config('search_path', current_setting('eshu.prev_search_path'), true);
