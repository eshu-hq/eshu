// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const containerImageIdentityCutoverSchemaSQL = `
DO $migration$
BEGIN
    EXECUTE $ddl$
        CREATE TABLE IF NOT EXISTS container_image_identity_cutovers (
            scope_id TEXT NOT NULL
                REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
            generation_id TEXT NOT NULL
                REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
            activated_by_work_item_id TEXT NOT NULL,
            activated_by_claim_epoch BIGINT NOT NULL
                CONSTRAINT container_image_identity_cutovers_claim_epoch_check
                CHECK (activated_by_claim_epoch > 0),
            cutover_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
            PRIMARY KEY (scope_id, generation_id)
        )
    $ddl$;

    ALTER TABLE fact_work_items
        ADD COLUMN IF NOT EXISTS
            container_image_identity_v2_required
            BOOLEAN NOT NULL DEFAULT FALSE,
        ADD COLUMN IF NOT EXISTS
            container_image_identity_claim_epoch BIGINT NOT NULL DEFAULT 0,
        ADD COLUMN IF NOT EXISTS
            container_image_identity_v2_authorized_status
            TEXT NOT NULL DEFAULT '';

    UPDATE fact_work_items
    SET container_image_identity_claim_epoch =
            GREATEST(attempt_count::BIGINT, 1)
    WHERE stage = 'reducer'
      AND domain = 'container_image_identity'
      AND container_image_identity_claim_epoch = 0
      AND status IN ('claimed', 'running', 'succeeded', 'retrying', 'dead_letter');

    ALTER TABLE container_image_identity_cutovers
        ADD COLUMN IF NOT EXISTS activated_by_work_item_id TEXT,
        ADD COLUMN IF NOT EXISTS activated_by_claim_epoch BIGINT;

    IF EXISTS (
        SELECT 1
        FROM container_image_identity_cutovers AS cutover
        LEFT JOIN fact_work_items AS work_item
          ON work_item.scope_id = cutover.scope_id
         AND work_item.generation_id = cutover.generation_id
         AND work_item.stage = 'reducer'
         AND work_item.domain = 'container_image_identity'
        GROUP BY cutover.scope_id, cutover.generation_id
        HAVING COUNT(work_item.work_item_id) <> 1
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'container image identity cutover requires exactly one reducer work item';
    END IF;

    UPDATE container_image_identity_cutovers AS cutover
    SET activated_by_work_item_id = work_item.work_item_id,
        activated_by_claim_epoch =
            work_item.container_image_identity_claim_epoch
    FROM fact_work_items AS work_item
    WHERE work_item.scope_id = cutover.scope_id
      AND work_item.generation_id = cutover.generation_id
      AND work_item.stage = 'reducer'
      AND work_item.domain = 'container_image_identity'
      AND (
          cutover.activated_by_work_item_id IS NULL
          OR cutover.activated_by_claim_epoch IS NULL
      );

    IF EXISTS (
        SELECT 1
        FROM container_image_identity_cutovers
        WHERE activated_by_work_item_id IS NULL
           OR activated_by_claim_epoch IS NULL
           OR activated_by_claim_epoch <= 0
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'container image identity cutover has invalid activation provenance';
    END IF;

    ALTER TABLE container_image_identity_cutovers
        ALTER COLUMN activated_by_work_item_id SET NOT NULL,
        ALTER COLUMN activated_by_claim_epoch SET NOT NULL;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'container_image_identity_cutovers'::regclass
          AND conname =
              'container_image_identity_cutovers_claim_epoch_check'
    ) THEN
        ALTER TABLE container_image_identity_cutovers
            ADD CONSTRAINT
                container_image_identity_cutovers_claim_epoch_check
            CHECK (activated_by_claim_epoch > 0);
    END IF;

    IF EXISTS (
        SELECT 1
        FROM fact_work_items AS work_item
        LEFT JOIN container_image_identity_cutovers AS cutover
          ON cutover.scope_id = work_item.scope_id
         AND cutover.generation_id = work_item.generation_id
        WHERE work_item.container_image_identity_v2_required
          AND cutover.scope_id IS NULL
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'container image identity queue fence has no durable cutover marker';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM container_image_identity_cutovers AS cutover
        JOIN fact_work_items AS work_item
          ON work_item.scope_id = cutover.scope_id
         AND work_item.generation_id = cutover.generation_id
         AND work_item.stage = 'reducer'
         AND work_item.domain = 'container_image_identity'
        WHERE work_item.status NOT IN ('claimed', 'running', 'succeeded')
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'container image identity cutover has invalid queue fence state';
    END IF;

    UPDATE fact_work_items AS work_item
    SET status = CASE
            WHEN work_item.status IN ('claimed', 'running') THEN 'running'
            ELSE work_item.status
        END,
        container_image_identity_v2_required = TRUE,
        container_image_identity_v2_authorized_status = CASE
            WHEN work_item.status IN ('claimed', 'running') THEN 'running'
            ELSE work_item.status
        END
    FROM container_image_identity_cutovers AS cutover
    WHERE work_item.scope_id = cutover.scope_id
      AND work_item.generation_id = cutover.generation_id
      AND work_item.stage = 'reducer'
      AND work_item.domain = 'container_image_identity'
      AND NOT work_item.container_image_identity_v2_required;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'fact_work_items'::regclass
          AND conname =
              'fact_work_items_container_image_identity_v2_status_check'
    ) THEN
        ALTER TABLE fact_work_items
            ADD CONSTRAINT
                fact_work_items_container_image_identity_v2_status_check
            CHECK (
                NOT container_image_identity_v2_required
                OR status = container_image_identity_v2_authorized_status
            );
    END IF;

    EXECUTE $ddl$
        CREATE OR REPLACE FUNCTION advance_container_image_identity_claim_epoch()
        RETURNS trigger
        LANGUAGE plpgsql
        AS $function$
        BEGIN
            NEW.container_image_identity_claim_epoch :=
                OLD.container_image_identity_claim_epoch + 1;
            IF OLD.container_image_identity_v2_required THEN
                NEW.status := 'running';
                NEW.container_image_identity_v2_authorized_status :=
                    'running';
            END IF;
            RETURN NEW;
        END;
        $function$
    $ddl$;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_work_items'::regclass
          AND tgname =
              'fact_work_items_container_image_identity_claim_epoch_advance'
          AND NOT tgisinternal
    ) THEN
        EXECUTE $ddl$
            CREATE TRIGGER fact_work_items_container_image_identity_claim_epoch_advance
            BEFORE UPDATE OF container_image_identity_claim_epoch
            ON fact_work_items
            FOR EACH ROW
            WHEN (OLD.domain = 'container_image_identity')
            EXECUTE FUNCTION advance_container_image_identity_claim_epoch()
        $ddl$;
    END IF;

    EXECUTE $ddl$
        CREATE OR REPLACE FUNCTION guard_container_image_identity_cutover_marker()
        RETURNS trigger
        LANGUAGE plpgsql
        AS $function$
        DECLARE
            cache_key TEXT;
            marker_key_state TEXT;
            marker_exists BOOLEAN;
            work_item_count INTEGER;
            work_item_required BOOLEAN;
            work_item_status TEXT;
            work_item_authorized_status TEXT;
        BEGIN
            IF current_setting('transaction_isolation') NOT IN (
                'read committed',
                'read uncommitted'
            ) THEN
                RAISE EXCEPTION
                    'legacy container image identity writes require read committed isolation during image_ref_v2 cutover';
            END IF;
            cache_key :=
                length(NEW.scope_id)::TEXT || ':' || NEW.scope_id ||
                length(NEW.generation_id)::TEXT || ':' || NEW.generation_id;
            marker_key_state := current_setting(
                'eshu_internal.container_image_identity_marker_key_v1',
                TRUE
            );
            IF COALESCE(marker_key_state, '') <> ''
                AND marker_key_state <> cache_key THEN
                RAISE EXCEPTION USING
                    ERRCODE = '55000',
                    MESSAGE = 'container image identity cutover supports one scope generation per transaction';
            END IF;
            SELECT
                status,
                container_image_identity_v2_required,
                container_image_identity_v2_authorized_status
            INTO
                work_item_status,
                work_item_required,
                work_item_authorized_status
            FROM fact_work_items
            WHERE work_item_id = NEW.activated_by_work_item_id
              AND scope_id = NEW.scope_id
              AND generation_id = NEW.generation_id
              AND stage = 'reducer'
              AND domain = 'container_image_identity'
              AND container_image_identity_claim_epoch =
                  NEW.activated_by_claim_epoch
            FOR UPDATE;
            GET DIAGNOSTICS work_item_count = ROW_COUNT;
            IF work_item_count <> 1 THEN
                RAISE EXCEPTION USING
                    ERRCODE = '55000',
                    MESSAGE = 'container image identity cutover requires the exact current claim epoch';
            END IF;
            SELECT EXISTS (
                SELECT 1
                FROM container_image_identity_cutovers
                WHERE scope_id = NEW.scope_id
                  AND generation_id = NEW.generation_id
            )
            INTO marker_exists;
            IF marker_exists THEN
                IF NOT work_item_required
                    OR NOT (
                        (
                            work_item_status = 'running'
                            AND work_item_authorized_status = 'running'
                        )
                        OR (
                            work_item_status = 'succeeded'
                            AND work_item_authorized_status = 'succeeded'
                        )
                    ) THEN
                    RAISE EXCEPTION USING
                        ERRCODE = '55000',
                        MESSAGE = 'existing container image identity cutover has invalid queue fence state';
                END IF;
            ELSE
                UPDATE fact_work_items AS work_item
                SET status = 'running',
                    container_image_identity_v2_required = TRUE,
                    container_image_identity_v2_authorized_status = 'running'
                WHERE work_item.work_item_id =
                          NEW.activated_by_work_item_id
                  AND work_item.scope_id = NEW.scope_id
                  AND work_item.generation_id = NEW.generation_id
                  AND work_item.stage = 'reducer'
                  AND work_item.domain = 'container_image_identity'
                  AND work_item.container_image_identity_claim_epoch = NEW.activated_by_claim_epoch
                  AND NOT work_item.container_image_identity_v2_required
                  AND work_item.status IN ('claimed', 'running');
                GET DIAGNOSTICS work_item_count = ROW_COUNT;
                IF work_item_count <> 1 THEN
                    RAISE EXCEPTION USING
                        ERRCODE = '55000',
                        MESSAGE = 'container image identity first cutover requires the exact active claim epoch';
                END IF;
            END IF;
            PERFORM pg_advisory_xact_lock(
                hashtextextended(
                    NEW.scope_id || E'\x1f' || NEW.generation_id,
                    5854
                )
            );
            PERFORM set_config(
                'eshu_internal.container_image_identity_marker_key_v1',
                cache_key,
                TRUE
            );
            RETURN NEW;
        END;
        $function$
    $ddl$;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'container_image_identity_cutovers'::regclass
          AND tgname = 'container_image_identity_cutover_marker_guard'
          AND NOT tgisinternal
    ) THEN
        EXECUTE $ddl$
            CREATE TRIGGER container_image_identity_cutover_marker_guard
            BEFORE INSERT ON container_image_identity_cutovers
            FOR EACH ROW
            EXECUTE FUNCTION guard_container_image_identity_cutover_marker()
        $ddl$;
    END IF;

    EXECUTE $ddl$
        CREATE OR REPLACE FUNCTION guard_legacy_container_image_identity_statement()
        RETURNS trigger
        LANGUAGE plpgsql
        AS $function$
        DECLARE
            legacy_scope_id TEXT;
            legacy_generation_id TEXT;
            legacy_key_count INTEGER;
            cutover_exists BOOLEAN;
        BEGIN
            EXECUTE format(
                'SELECT
                    min(scope_id),
                    min(generation_id),
                    count(*)
                 FROM (
                    SELECT DISTINCT scope_id, generation_id
                    FROM %I
                    WHERE fact_kind = ''reducer_container_image_identity''
                      AND COALESCE(
                          payload->>''identity_format'',
                          ''''
                      ) <> ''image_ref_v2''
                    LIMIT 2
                ) AS legacy_keys',
                TG_ARGV[0]
            )
            INTO
                legacy_scope_id,
                legacy_generation_id,
                legacy_key_count;
            IF legacy_key_count > 1 THEN
                RAISE EXCEPTION USING
                    ERRCODE = '55000',
                    MESSAGE = 'legacy container image identity writer statement spans multiple scope generations';
            END IF;
            IF legacy_key_count = 1 THEN
                IF current_setting('transaction_isolation') NOT IN (
                    'read committed',
                    'read uncommitted'
                ) THEN
                    RAISE EXCEPTION
                        'legacy container image identity writes require read committed isolation during image_ref_v2 cutover';
                END IF;
                PERFORM pg_advisory_xact_lock(
                    hashtextextended(
                        legacy_scope_id || E'\x1f' ||
                            legacy_generation_id,
                        5854
                    )
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
                USING legacy_scope_id, legacy_generation_id;
                IF cutover_exists THEN
                    RAISE EXCEPTION USING
                        ERRCODE = '55000',
                        MESSAGE = 'legacy container image identity writer is incompatible with completed image_ref_v2 cutover';
                END IF;
            END IF;
            RETURN NULL;
        END;
        $function$
    $ddl$;

    DROP TRIGGER IF EXISTS
        fact_records_legacy_container_image_identity_cutover_guard
        ON fact_records;
    DROP FUNCTION IF EXISTS guard_legacy_container_image_identity_fact() CASCADE;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_records'::regclass
          AND tgname =
              'fact_records_legacy_container_image_identity_cutover_guard_update_statement'
          AND NOT tgisinternal
    ) THEN
        EXECUTE $ddl$
            CREATE TRIGGER fact_records_legacy_container_image_identity_cutover_guard_update_statement
            AFTER UPDATE ON fact_records
            REFERENCING NEW TABLE AS updated_rows
            FOR EACH STATEMENT
            EXECUTE FUNCTION guard_legacy_container_image_identity_statement('updated_rows')
        $ddl$;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_records'::regclass
          AND tgname =
              'fact_records_legacy_container_image_identity_cutover_guard_insert_statement'
          AND NOT tgisinternal
    ) THEN
        EXECUTE $ddl$
            CREATE TRIGGER fact_records_legacy_container_image_identity_cutover_guard_insert_statement
            AFTER INSERT ON fact_records
            REFERENCING NEW TABLE AS inserted_rows
            FOR EACH STATEMENT
            EXECUTE FUNCTION guard_legacy_container_image_identity_statement('inserted_rows')
        $ddl$;
    END IF;

    DROP TRIGGER IF EXISTS
        fact_work_items_container_image_identity_ack_guard
        ON fact_work_items;
    DROP FUNCTION IF EXISTS guard_container_image_identity_ack();
END;
$migration$;

CREATE INDEX IF NOT EXISTS
    fact_records_container_image_identity_legacy_cleanup_idx
ON fact_records (scope_id, generation_id, fact_id)
WHERE fact_kind = 'reducer_container_image_identity'
  AND is_tombstone = FALSE
  AND COALESCE(payload->>'identity_format', '') <> 'image_ref_v2';
`
