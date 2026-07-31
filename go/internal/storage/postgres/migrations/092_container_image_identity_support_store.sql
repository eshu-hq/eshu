-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

CREATE SEQUENCE IF NOT EXISTS container_image_identity_activation_epoch_seq;

CREATE TABLE IF NOT EXISTS container_image_identity_support_sets (
    set_id BYTEA PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    content_hash BYTEA NOT NULL,
    support_count INTEGER NOT NULL CHECK (support_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (scope_id, content_hash),
    UNIQUE (scope_id, set_id)
);

CREATE TABLE IF NOT EXISTS container_image_identity_supports (
    set_id BYTEA NOT NULL
        REFERENCES container_image_identity_support_sets(set_id) ON DELETE CASCADE,
    digest TEXT NOT NULL CHECK (digest <> ''),
    support_id BYTEA NOT NULL CHECK (octet_length(support_id) > 0),
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

ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS container_image_identity_v3_required
        BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS container_image_identity_v3_authorized_status
        TEXT NOT NULL DEFAULT '';

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
            );
    END IF;
END;
$constraint$;

CREATE TABLE IF NOT EXISTS container_image_identity_scope_state (
    scope_id TEXT PRIMARY KEY REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    active_generation_id TEXT,
    activation_epoch BIGINT NOT NULL CHECK (activation_epoch > 0),
    active_set_id BYTEA,
    last_set_id BYTEA,
    last_set_hash BYTEA,
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
ON CONFLICT (scope_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS container_image_identity_storage_cutover (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    identity_format TEXT NOT NULL CHECK (identity_format = 'digest_v3'),
    cutover_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

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

DROP TRIGGER IF EXISTS fact_records_reject_container_image_identity_v2
    ON fact_records;
CREATE TRIGGER fact_records_reject_container_image_identity_v2
BEFORE INSERT OR UPDATE ON fact_records
FOR EACH ROW
WHEN (NEW.fact_kind = 'reducer_container_image_identity')
EXECUTE FUNCTION reject_container_image_identity_fact_record_write();
