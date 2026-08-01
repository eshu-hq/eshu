-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Acquire the first-upgrade lock before creating any digest-v3 object. Schema
-- definitions commit independently, so taking this lock later could leave a
-- partial support store when lock_timeout rejects the fact_work_items ALTER.
-- Repeated bootstrap skips the lock once both columns and the constraint are
-- present.
DO $upgrade_lock$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM (VALUES
            ('container_image_identity_v3_required'),
            ('container_image_identity_v3_authorized_status')
        ) AS required(attname)
        WHERE NOT EXISTS (
            SELECT 1 FROM pg_attribute
            WHERE attrelid = 'fact_work_items'::regclass
              AND pg_attribute.attname = required.attname
              AND NOT attisdropped
        )
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'fact_work_items'::regclass
          AND conname = 'fact_work_items_container_image_identity_v3_status_check'
    ) THEN
        LOCK TABLE fact_work_items IN ACCESS EXCLUSIVE MODE;
    END IF;
END;
$upgrade_lock$;

DO $columns$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_attribute
        WHERE attrelid = 'fact_work_items'::regclass
          AND attname = 'container_image_identity_v3_required'
          AND NOT attisdropped
    ) THEN
        ALTER TABLE fact_work_items
            ADD COLUMN container_image_identity_v3_required
                BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_attribute
        WHERE attrelid = 'fact_work_items'::regclass
          AND attname = 'container_image_identity_v3_authorized_status'
          AND NOT attisdropped
    ) THEN
        ALTER TABLE fact_work_items
            ADD COLUMN container_image_identity_v3_authorized_status
                TEXT NOT NULL DEFAULT '';
    END IF;
END;
$columns$;

DO $constraint$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'fact_work_items'::regclass
          AND conname = 'fact_work_items_container_image_identity_v3_status_check'
    ) THEN
        ALTER TABLE fact_work_items
            ADD CONSTRAINT fact_work_items_container_image_identity_v3_status_check
            CHECK (
                NOT container_image_identity_v3_required
                OR status = container_image_identity_v3_authorized_status
            ) NOT VALID;
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'fact_work_items'::regclass
          AND conname = 'fact_work_items_container_image_identity_v3_status_check'
          AND NOT convalidated
    ) THEN
        ALTER TABLE fact_work_items
            VALIDATE CONSTRAINT fact_work_items_container_image_identity_v3_status_check;
    END IF;
END;
$constraint$;

-- The image_ref_v2 cutover marker predates the digest-v3 claim latch. Replace
-- its guard only after the v3 columns and constraint exist so its authorized
-- claimed-to-running transition advances both latches atomically. Legacy v2
-- attempts keep v3_required = FALSE and retain an empty v3 authorization.
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
        'eshu_internal.container_image_identity_marker_key_v1', TRUE
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
    INTO work_item_status, work_item_required, work_item_authorized_status
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
                    work_item_status = 'claimed'
                    AND work_item_authorized_status = 'claimed'
                )
                OR (
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
        IF work_item_status = 'claimed' THEN
            UPDATE fact_work_items AS work_item
            SET status = 'running',
                container_image_identity_v2_authorized_status = 'running',
                container_image_identity_v3_authorized_status = CASE
                    WHEN work_item.container_image_identity_v3_required THEN 'running'
                    ELSE work_item.container_image_identity_v3_authorized_status
                END
            WHERE work_item.work_item_id = NEW.activated_by_work_item_id
              AND work_item.scope_id = NEW.scope_id
              AND work_item.generation_id = NEW.generation_id
              AND work_item.stage = 'reducer'
              AND work_item.domain = 'container_image_identity'
              AND work_item.container_image_identity_claim_epoch =
                  NEW.activated_by_claim_epoch
              AND work_item.container_image_identity_v2_required
              AND work_item.status = 'claimed'
              AND work_item.container_image_identity_v2_authorized_status = 'claimed'
              AND (
                  NOT work_item.container_image_identity_v3_required
                  OR work_item.container_image_identity_v3_authorized_status = 'claimed'
              );
            GET DIAGNOSTICS work_item_count = ROW_COUNT;
            IF work_item_count <> 1 THEN
                RAISE EXCEPTION USING
                    ERRCODE = '55000',
                    MESSAGE = 'existing container image identity cutover requires the exact active claim epoch';
            END IF;
        END IF;
    ELSE
        UPDATE fact_work_items AS work_item
        SET status = 'running',
            container_image_identity_v2_required = TRUE,
            container_image_identity_v2_authorized_status = 'running',
            container_image_identity_v3_authorized_status = CASE
                WHEN work_item.container_image_identity_v3_required THEN 'running'
                ELSE work_item.container_image_identity_v3_authorized_status
            END
        WHERE work_item.work_item_id = NEW.activated_by_work_item_id
          AND work_item.scope_id = NEW.scope_id
          AND work_item.generation_id = NEW.generation_id
          AND work_item.stage = 'reducer'
          AND work_item.domain = 'container_image_identity'
          AND work_item.container_image_identity_claim_epoch = NEW.activated_by_claim_epoch
          AND work_item.status IN ('claimed', 'running')
          AND (
              NOT work_item.container_image_identity_v3_required
              OR work_item.container_image_identity_v3_authorized_status = work_item.status
          )
          AND (
                (
                    NOT work_item.container_image_identity_v2_required
                    AND work_item.container_image_identity_v2_authorized_status = ''
                )
                OR (
                    work_item.container_image_identity_v2_required
                    AND work_item.container_image_identity_v2_authorized_status =
                        work_item.status
                )
          );
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
$function$;

CREATE SEQUENCE IF NOT EXISTS container_image_identity_activation_epoch_seq;

CREATE TABLE IF NOT EXISTS container_image_identity_support_sets (
    set_id BYTEA PRIMARY KEY CHECK (octet_length(set_id) = 32),
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    content_hash BYTEA NOT NULL CHECK (octet_length(content_hash) = 32),
    support_count INTEGER NOT NULL CHECK (support_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (scope_id, content_hash),
    UNIQUE (scope_id, set_id)
);

CREATE TABLE IF NOT EXISTS container_image_identity_supports (
    set_id BYTEA NOT NULL
        REFERENCES container_image_identity_support_sets(set_id) ON DELETE CASCADE,
    digest TEXT NOT NULL CHECK (digest <> ''),
    support_id BYTEA NOT NULL CHECK (octet_length(support_id) = 32),
    image_ref TEXT NOT NULL DEFAULT '',
    repository_id TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL CHECK (
        outcome IN ('exact_digest', 'tag_resolved', 'ambiguous_tag', 'unresolved', 'stale_tag')
    ),
    identity_strength TEXT NOT NULL DEFAULT '',
    source_revision TEXT NOT NULL DEFAULT '',
    source_revision_provenance TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    canonical_writes INTEGER NOT NULL DEFAULT 0 CHECK (canonical_writes >= 0),
    source_repository_ids TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    build_provenance_repository_ids TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    base_image_for_repository_ids TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    workload_ids TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    service_ids TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    source_layers TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    evidence_fact_ids TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    missing_evidence TEXT[] NOT NULL DEFAULT '{}'::TEXT[],
    PRIMARY KEY (set_id, digest, support_id)
);

CREATE INDEX IF NOT EXISTS container_image_identity_supports_image_ref_idx
    ON container_image_identity_supports (set_id, image_ref, digest, support_id);
CREATE INDEX IF NOT EXISTS container_image_identity_supports_repository_idx
    ON container_image_identity_supports (set_id, repository_id, digest, support_id);
CREATE INDEX IF NOT EXISTS container_image_identity_supports_outcome_idx
    ON container_image_identity_supports (set_id, outcome, digest, support_id);
CREATE INDEX IF NOT EXISTS container_image_identity_supports_source_repositories_idx
    ON container_image_identity_supports USING GIN (source_repository_ids);

CREATE TABLE IF NOT EXISTS container_image_identity_scope_state (
    scope_id TEXT PRIMARY KEY REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    active_generation_id TEXT,
    activation_epoch BIGINT NOT NULL CHECK (activation_epoch > 0),
    active_set_id BYTEA,
    last_set_id BYTEA,
    last_set_hash BYTEA CHECK (last_set_hash IS NULL OR octet_length(last_set_hash) = 32),
    published_claim_epoch BIGINT NOT NULL DEFAULT 0 CHECK (published_claim_epoch >= 0),
    source_system TEXT NOT NULL DEFAULT 'unknown',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_fact_key TEXT NOT NULL DEFAULT '',
    cause TEXT NOT NULL DEFAULT '',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT '-infinity',
    ingested_at TIMESTAMPTZ NOT NULL DEFAULT '-infinity',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT container_image_identity_scope_state_active_set_fk
        FOREIGN KEY (scope_id, active_set_id)
        REFERENCES container_image_identity_support_sets(scope_id, set_id),
    CONSTRAINT container_image_identity_scope_state_last_set_fk
        FOREIGN KEY (scope_id, last_set_id)
        REFERENCES container_image_identity_support_sets(scope_id, set_id),
    CHECK ((active_set_id IS NULL) OR (active_generation_id IS NOT NULL)),
    CHECK ((last_set_id IS NULL) = (last_set_hash IS NULL))
);

CREATE INDEX IF NOT EXISTS container_image_identity_scope_state_active_set_idx
    ON container_image_identity_scope_state (active_set_id, scope_id)
    WHERE active_set_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS container_image_identity_storage_cutover (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    identity_format TEXT NOT NULL CHECK (identity_format = 'digest_v3'),
    cutover_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE OR REPLACE FUNCTION reset_container_image_identity_scope_state()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.active_generation_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND NEW.active_generation_id IS NOT DISTINCT FROM OLD.active_generation_id THEN
        RETURN NEW;
    END IF;
    INSERT INTO container_image_identity_scope_state (
        scope_id, active_generation_id, activation_epoch
    ) VALUES (
        NEW.scope_id,
        NEW.active_generation_id,
        nextval('container_image_identity_activation_epoch_seq')
    )
    ON CONFLICT (scope_id) DO UPDATE
    SET active_generation_id = EXCLUDED.active_generation_id,
        activation_epoch = EXCLUDED.activation_epoch,
        active_set_id = NULL,
        published_claim_epoch = 0,
        source_fact_key = '',
        fencing_token = 0,
        updated_at = clock_timestamp();
    RETURN NEW;
END;
$function$;

DO $trigger$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'ingestion_scopes'::regclass
          AND tgname = 'ingestion_scopes_container_image_identity_state_reset'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER ingestion_scopes_container_image_identity_state_reset
        AFTER INSERT OR UPDATE OF active_generation_id ON ingestion_scopes
        FOR EACH ROW
        EXECUTE FUNCTION reset_container_image_identity_scope_state();
    END IF;
END;
$trigger$;

INSERT INTO container_image_identity_scope_state (
    scope_id,
    active_generation_id,
    activation_epoch
)
SELECT
    scope.scope_id,
    scope.active_generation_id,
    nextval('container_image_identity_activation_epoch_seq')
FROM ingestion_scopes AS scope
WHERE NOT EXISTS (
    SELECT 1
    FROM container_image_identity_storage_cutover
    WHERE singleton
      AND identity_format = 'digest_v3'
)
ON CONFLICT (scope_id) DO NOTHING;

-- The marker advertises schema capability, not global read authority. Read
-- authority cuts over per scope when a digest-v3-capable writer installs that
-- scope's active_set_id; pre-v3 in-flight writers remain valid before then.
INSERT INTO container_image_identity_storage_cutover (singleton, identity_format)
VALUES (TRUE, 'digest_v3')
ON CONFLICT (singleton) DO UPDATE
SET identity_format = EXCLUDED.identity_format;

CREATE OR REPLACE FUNCTION reject_container_image_identity_fact_record_write()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM container_image_identity_scope_state AS state
        WHERE state.scope_id = NEW.scope_id
          AND state.active_set_id IS NOT NULL
    ) OR EXISTS (
        SELECT 1
        FROM fact_work_items AS work_item
        WHERE work_item.work_item_id = NEW.source_fact_key
          AND work_item.scope_id = NEW.scope_id
          AND work_item.generation_id = NEW.generation_id
          AND work_item.stage = 'reducer'
          AND work_item.domain = 'container_image_identity'
          AND work_item.container_image_identity_v3_required
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '55000',
            MESSAGE = 'reducer_container_image_identity fact_records writes are disabled for a digest_v3-capable scope';
    END IF;
    RETURN NEW;
END;
$function$;

DO $trigger$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgrelid = 'fact_records'::regclass
          AND tgname = 'fact_records_reject_container_image_identity_v2'
          AND NOT tgisinternal
    ) THEN
        CREATE TRIGGER fact_records_reject_container_image_identity_v2
        BEFORE INSERT OR UPDATE ON fact_records
        FOR EACH ROW
        WHEN (NEW.fact_kind = 'reducer_container_image_identity')
        EXECUTE FUNCTION reject_container_image_identity_fact_record_write();
    END IF;
END;
$trigger$;
