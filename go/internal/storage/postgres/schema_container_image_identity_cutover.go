// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const containerImageIdentityCutoverSchemaSQL = `
CREATE TABLE IF NOT EXISTS container_image_identity_cutovers (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    cutover_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (scope_id, generation_id)
);

CREATE OR REPLACE FUNCTION guard_legacy_container_image_identity_fact()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
DECLARE
    cutover_exists BOOLEAN;
BEGIN
    IF current_setting('transaction_isolation') NOT IN (
        'read committed',
        'read uncommitted'
    ) THEN
        RAISE EXCEPTION
            'legacy container image identity writes require read committed isolation during image_ref_v2 cutover';
    END IF;
    PERFORM pg_advisory_xact_lock(
        hashtextextended(NEW.scope_id || E'\x1f' || NEW.generation_id, 5854)
    );
    EXECUTE format(
        'SELECT EXISTS (
            SELECT 1
            FROM %I.container_image_identity_cutovers AS cutover
            WHERE cutover.scope_id = $1
              AND cutover.generation_id = $2
        )',
        TG_TABLE_SCHEMA
    )
    INTO cutover_exists
    USING NEW.scope_id, NEW.generation_id;
    IF cutover_exists THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$function$;

DO $migration$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_records'::regclass
          AND tgname = 'fact_records_legacy_container_image_identity_cutover_guard'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER fact_records_legacy_container_image_identity_cutover_guard
        BEFORE INSERT ON fact_records
        FOR EACH ROW
        WHEN (
            NEW.fact_kind = 'reducer_container_image_identity'
            AND COALESCE(NEW.payload->>'identity_format', '') <> 'image_ref_v2'
        )
        EXECUTE FUNCTION guard_legacy_container_image_identity_fact();
    END IF;
END;
$migration$;
`
