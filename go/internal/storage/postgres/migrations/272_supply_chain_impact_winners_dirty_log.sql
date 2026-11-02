-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9805: dirty log for the supply-chain impact winners read model. A change to
-- anything the winners select reads appends one row here, in the transaction
-- that makes the change. The winners maintainer consumes the rows with
-- DELETE ... RETURNING under its single-flight resweep lock and recomputes only
-- the keys they name (go/internal/storage/postgres/supply_chain_impact_winners_keyed.go).
--
-- Capture owners (ruling B, docs/internal/evidence/9805-winners-keyed-resweep.md):
--   * fact_records: the impact writer, in the Go transaction, filtered by the
--     content digest it already reads (fact_upsert), and the retract statement
--     itself (fact_retract). No trigger sits on that table.
--   * ingestion_scopes.active_generation_id: a row trigger (the shape migration
--     092 already uses on this column). It sees every writer of the pointer,
--     including the operator suppression scope, and any future one.
--   * ingestion_scopes delete: a BEFORE row trigger that appends the scope's
--     finding identifiers before the foreign keys cascade its facts away (the
--     retirement purge), so the leaving winners are durable at delete time.
--   * scanner_image_result_fact_classifications and
--     scanner_image_result_fact_associations: statement triggers over the
--     transition tables, because the protected publish function writes them and
--     no fact_records row. Those tables belong to the database-global scanner
--     authority layout, so the triggers live in 272a (public-only), not here.
--
-- Two row kinds:
--   fact  names a finding's key by the five columns every candidate of one
--         canonical key shares: package_id, repository_id, subject_digest, the
--         first key component c0 (cve id, else advisory id, else '') and
--         advisory_id. The probe uses the existing package-lookup index.
--   scope names an (ingestion scope, generation) pair; the consumer expands it
--         to the fact identifiers of that generation. A pointer flip appends the
--         old and new generation, because source_count is a data column and a
--         candidate leaving a key changes the winner row.
--
-- source records which capture path wrote the row. fact_upsert and fact_retract
-- are counted by their writers at capture time; the trigger sources are counted
-- when the maintainer consumes them; the audit tags the rows it queues 'audit'.
--
-- The table is an append-only queue with no unique index: writers never lock
-- each other or any existing row. It is delete-heavy, so autovacuum is set to
-- run on a fixed dead-tuple count instead of a fraction of the table.
CREATE TABLE IF NOT EXISTS supply_chain_impact_winners_dirty_log (
    seq BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('fact', 'scope')),
    source TEXT NOT NULL CHECK (source IN
        ('fact_upsert', 'fact_retract', 'activation', 'operator', 'scanner', 'audit')),
    package_id TEXT,
    repository_id TEXT,
    subject_digest TEXT,
    c0 TEXT,
    advisory_id TEXT,
    scope_id TEXT,
    generation_id TEXT,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT supply_chain_impact_winners_dirty_log_shape CHECK (
        (kind = 'fact'
            AND package_id IS NOT NULL AND repository_id IS NOT NULL
            AND subject_digest IS NOT NULL AND c0 IS NOT NULL AND advisory_id IS NOT NULL
            AND scope_id IS NULL AND generation_id IS NULL)
        OR
        (kind = 'scope'
            AND scope_id IS NOT NULL AND generation_id IS NOT NULL
            AND package_id IS NULL AND repository_id IS NULL
            AND subject_digest IS NULL AND c0 IS NULL AND advisory_id IS NULL)
    )
) WITH (
    autovacuum_vacuum_scale_factor = 0,
    autovacuum_vacuum_threshold = 1000,
    autovacuum_analyze_scale_factor = 0,
    autovacuum_analyze_threshold = 5000
);

-- Pointer capture. The function skips an UPDATE that leaves the pointer
-- unchanged (the upsert conflict arm that keeps the old value) and writes the
-- generation the pointer leaves and the one it enters. A pointer set to NULL
-- (the retirement fence, the failed-generation reconcile) captures only the old
-- generation. The operator suppression scope reports source 'operator'.
--
-- It runs as its owner (SECURITY DEFINER), so a login that moves a pointer (the
-- projector, the collector, the retirement fence, the suppression store) needs
-- no privilege on the log, and a missing grant cannot halt projection for every
-- scope. Its search_path is pinned to pg_catalog plus the one schema this
-- migration runs in, which is public in every production bootstrap; an isolated
-- test schema pins itself instead of reaching into public. The bodies name their
-- tables unqualified and resolve them only through that pinned path, set below
-- with ALTER FUNCTION ... SET search_path FROM CURRENT once the session path is
-- pg_catalog plus current_schema() plus pg_temp. pg_catalog stays first so no
-- object in the schema can shadow a built-in operator or function, and pg_temp
-- is listed last because Postgres otherwise searches the session temp schema
-- first for relations: a login that may create temporary tables could then name
-- one like the dirty log or fact_records and divert or forge the capture (the
-- SECURITY DEFINER guidance, and the pin the scanner functions use). The two
-- set_config calls are transaction-local and the last one restores the
-- caller's path.
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
    IF TG_OP = 'UPDATE' AND OLD.active_generation_id IS NOT NULL THEN
        INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id)
        VALUES ('scope', v_source, OLD.scope_id, OLD.active_generation_id);
    END IF;
    IF NEW.active_generation_id IS NOT NULL THEN
        INSERT INTO supply_chain_impact_winners_dirty_log (kind, source, scope_id, generation_id)
        VALUES ('scope', v_source, NEW.scope_id, NEW.active_generation_id);
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE TRIGGER ingestion_scopes_winners_dirty_capture
AFTER INSERT OR UPDATE OF active_generation_id ON ingestion_scopes
FOR EACH ROW EXECUTE FUNCTION supply_chain_impact_winners_capture_activation();

-- Scope delete capture. The repository retirement fences a scope (the pointer
-- goes NULL, one 'scope' row for the old generation) and purges it moments
-- later in the same request: the purge deletes the scope row, and the foreign
-- keys cascade its facts away before any keyed tick can expand that 'scope'
-- row. So a deleted scope names its leaving identifiers while its facts still
-- exist. A BEFORE row trigger runs before the cascade (the cascade is an AFTER
-- trigger of the referential action), so the findings are still readable. It
-- appends every identifier the scope's non-tombstoned findings hold, across
-- all of its generations, as 'fact' rows: a superset of what the last active
-- generation fed is harmless (a recompute of an unaffected key changes
-- nothing), and the lookup needs no read of the log. Any deleter is covered,
-- not only the retirement purge. The cost is one indexed read of the scope's
-- findings plus one log row per distinct identifier; a scope past the keyed
-- cap sends the next tick down the full path.
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
      AND fact.is_tombstone = FALSE;
    RETURN OLD;
END;
$function$;

CREATE OR REPLACE TRIGGER ingestion_scopes_winners_dirty_capture_delete
BEFORE DELETE ON ingestion_scopes
FOR EACH ROW EXECUTE FUNCTION supply_chain_impact_winners_capture_scope_delete();

SELECT set_config('search_path', 'pg_catalog, ' || quote_ident(current_schema()) || ', pg_temp', true);
ALTER FUNCTION supply_chain_impact_winners_capture_activation() SET search_path FROM CURRENT;
ALTER FUNCTION supply_chain_impact_winners_capture_scope_delete() SET search_path FROM CURRENT;

-- The capture functions are trigger functions: a direct call fails, and a trigger
-- fires without the caller holding EXECUTE. They are SECURITY DEFINER all the
-- same, so withdraw the default PUBLIC execute privilege as the earlier
-- SECURITY DEFINER migrations do.
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_activation() FROM PUBLIC;
REVOKE ALL ON FUNCTION supply_chain_impact_winners_capture_scope_delete() FROM PUBLIC;

SELECT set_config('search_path', current_setting('eshu.prev_search_path'), true);
