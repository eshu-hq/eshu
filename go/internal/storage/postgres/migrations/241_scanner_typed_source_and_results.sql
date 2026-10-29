-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9299. These rows are protected authority and result provenance. Generic
-- fact_records remain a projection; they cannot create source eligibility.
CREATE SCHEMA IF NOT EXISTS eshu_security;
CREATE TABLE IF NOT EXISTS eshu_security.role_policy_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    policy_revision BIGINT NOT NULL CHECK (policy_revision >= 0),
    database_oid OID NOT NULL,
    database_identity UUID NOT NULL,
    system_identifier TEXT NOT NULL,
    inventory_version INTEGER NOT NULL CHECK (inventory_version > 0),
    owner_role_oid OID NOT NULL,
    authority_epoch BIGINT NOT NULL,
    active BOOLEAN NOT NULL
);
CREATE TABLE IF NOT EXISTS eshu_security.role_contracts (
    contract_id TEXT PRIMARY KEY,
    policy_revision BIGINT NOT NULL,
    workload_class TEXT NOT NULL,
    connection_route TEXT NOT NULL,
    profile TEXT NOT NULL,
    session_role_oid OID NOT NULL,
    effective_role_oid OID NOT NULL,
    enabled BOOLEAN NOT NULL
);
CREATE TABLE IF NOT EXISTS eshu_security.role_acl_inventory (
    inventory_version INTEGER NOT NULL,
    profile TEXT NOT NULL,
    object_kind TEXT NOT NULL,
    object_identity TEXT NOT NULL,
    privilege_name TEXT NOT NULL,
    required BOOLEAN NOT NULL,
    PRIMARY KEY (inventory_version,profile,object_kind,object_identity,privilege_name)
);
REVOKE ALL ON SCHEMA eshu_security FROM PUBLIC;
REVOKE ALL ON eshu_security.role_policy_state,
    eshu_security.role_contracts,eshu_security.role_acl_inventory FROM PUBLIC;

CREATE SEQUENCE IF NOT EXISTS scanner_authority_epoch_seq AS BIGINT;
CREATE SEQUENCE IF NOT EXISTS scanner_running_image_observation_id_seq AS BIGINT;

CREATE TABLE IF NOT EXISTS scanner_running_image_producer_grants (
    principal_oid OID NOT NULL,
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    producer_class TEXT NOT NULL CHECK (producer_class IN
      ('kubernetes_pod','ecs_task','lambda_function')),
    observation_class TEXT NOT NULL CHECK (observation_class IN
      ('running_container','active_deployment')),
    contract_version INTEGER NOT NULL CHECK (contract_version > 0),
    grant_epoch BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (principal_oid,producer_instance_id,source_scope_id,
                 producer_class,observation_class)
);

CREATE TABLE IF NOT EXISTS scanner_running_image_scope_claims (
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    source_generation_id TEXT NOT NULL,
    principal_oid OID NOT NULL,
    collection_fence BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    lease_expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (producer_instance_id,source_scope_id)
);

CREATE TABLE IF NOT EXISTS scanner_running_image_generations (
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    source_generation_id TEXT NOT NULL,
    principal_oid OID NOT NULL,
    collection_fence BIGINT NOT NULL,
    producer_grant_epoch BIGINT NOT NULL,
    producer_class TEXT NOT NULL CHECK (producer_class IN
      ('kubernetes_pod','ecs_task','lambda_function')),
    observation_class TEXT NOT NULL CHECK (observation_class IN
      ('running_container','active_deployment')),
    status TEXT NOT NULL CHECK (status IN ('staging','failed','published')),
    observed_from TIMESTAMPTZ NOT NULL,
    observed_through TIMESTAMPTZ NOT NULL,
    consistency_class TEXT NOT NULL,
    expected_pages INTEGER,
    expected_slots INTEGER,
    manifest_sha256 TEXT,
    publication_epoch BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (producer_instance_id,source_scope_id,source_generation_id),
    CHECK (observed_through >= observed_from),
    CHECK (expected_pages IS NULL OR expected_pages >= 0),
    CHECK (expected_slots IS NULL OR expected_slots >= 0)
);

CREATE TABLE IF NOT EXISTS scanner_running_image_page_receipts (
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    source_generation_id TEXT NOT NULL,
    page_index INTEGER NOT NULL CHECK (page_index >= 0),
    request_sha256 TEXT NOT NULL,
    start_token_sha256 TEXT NOT NULL,
    next_token_sha256 TEXT NOT NULL,
    unique_slots INTEGER NOT NULL CHECK (unique_slots >= 0),
    terminal BOOLEAN NOT NULL,
    PRIMARY KEY (producer_instance_id,source_scope_id,source_generation_id,page_index),
    FOREIGN KEY (producer_instance_id,source_scope_id,source_generation_id)
      REFERENCES scanner_running_image_generations
      (producer_instance_id,source_scope_id,source_generation_id)
);

CREATE TABLE IF NOT EXISTS scanner_running_image_delivery_receipts (
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    source_generation_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    request_sha256 TEXT NOT NULL,
    accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (producer_instance_id,source_scope_id,source_generation_id,delivery_id),
    FOREIGN KEY (producer_instance_id,source_scope_id,source_generation_id)
      REFERENCES scanner_running_image_generations
      (producer_instance_id,source_scope_id,source_generation_id)
);

CREATE TABLE IF NOT EXISTS scanner_running_image_observations (
    observation_id BIGINT PRIMARY KEY DEFAULT nextval('scanner_running_image_observation_id_seq'),
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    source_generation_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    object_id TEXT NOT NULL,
    slot_id TEXT NOT NULL,
    producer_class TEXT NOT NULL CHECK (producer_class IN
      ('kubernetes_pod','ecs_task','lambda_function')),
    observation_class TEXT NOT NULL CHECK (observation_class IN
      ('running_container','active_deployment')),
    observed_revision TEXT NOT NULL DEFAULT '',
    registry TEXT NOT NULL,
    repository TEXT NOT NULL,
    runtime_image_reference TEXT NOT NULL,
    runtime_digest TEXT NOT NULL,
    runtime_digest_kind TEXT NOT NULL CHECK (runtime_digest_kind IN
      ('unknown','manifest','config')),
    last_update_status TEXT NOT NULL DEFAULT '',
    payload_sha256 TEXT NOT NULL,
    UNIQUE (producer_instance_id,source_scope_id,source_generation_id,object_id,slot_id),
    FOREIGN KEY (producer_instance_id,source_scope_id,source_generation_id,delivery_id)
      REFERENCES scanner_running_image_delivery_receipts
      (producer_instance_id,source_scope_id,source_generation_id,delivery_id)
);
CREATE INDEX IF NOT EXISTS scanner_running_image_observations_generation_idx
    ON scanner_running_image_observations
    (producer_instance_id,source_scope_id,source_generation_id,observation_id);

CREATE TABLE IF NOT EXISTS scanner_running_image_active_scopes (
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    published_generation_id TEXT NOT NULL,
    activation_epoch BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    active BOOLEAN NOT NULL,
    published_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (producer_instance_id,source_scope_id),
    FOREIGN KEY (producer_instance_id,source_scope_id,published_generation_id)
      REFERENCES scanner_running_image_generations
      (producer_instance_id,source_scope_id,source_generation_id)
);

-- Each mutable ancestor has a non-reusable DB-issued incarnation. These
-- witnesses are retained across row deletion and same-key recreation.
CREATE TABLE IF NOT EXISTS scanner_authority_epochs (
    authority_kind TEXT NOT NULL,
    authority_key TEXT NOT NULL,
    authority_epoch BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    active BOOLEAN NOT NULL,
    PRIMARY KEY (authority_kind,authority_key)
);

-- The ordinary writer may update an ancestor but cannot mint or rewrite its
-- protected epoch. The trigger runs as the migration owner; runtime roles
-- receive no direct DML privilege on scanner_authority_epochs.
CREATE OR REPLACE FUNCTION scanner_bump_ancestor_epoch() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, public, pg_temp AS $$
DECLARE
    before_row JSONB := to_jsonb(OLD);
    after_row JSONB := to_jsonb(NEW);
    row_value JSONB := COALESCE(after_row,before_row);
    old_authority JSONB;
    new_authority JSONB;
    authority_kind TEXT;
    authority_key TEXT;
    is_active BOOLEAN;
BEGIN
    CASE TG_TABLE_NAME
    WHEN 'tenants' THEN
        authority_kind := 'tenant';
        authority_key := (jsonb_build_array(row_value->>'tenant_id'))::text;
        old_authority := jsonb_build_array(before_row->>'status',before_row->>'policy_revision_hash',before_row->>'tombstoned_at');
        new_authority := jsonb_build_array(after_row->>'status',after_row->>'policy_revision_hash',after_row->>'tombstoned_at');
        is_active := TG_OP <> 'DELETE' AND after_row->>'status' = 'active' AND after_row->>'tombstoned_at' IS NULL;
    WHEN 'workspaces' THEN
        authority_kind := 'workspace';
        authority_key := (jsonb_build_array(row_value->>'tenant_id',row_value->>'workspace_id'))::text;
        old_authority := jsonb_build_array(before_row->>'status',before_row->>'policy_revision_hash',before_row->>'tombstoned_at');
        new_authority := jsonb_build_array(after_row->>'status',after_row->>'policy_revision_hash',after_row->>'tombstoned_at');
        is_active := TG_OP <> 'DELETE' AND after_row->>'status' = 'active' AND after_row->>'tombstoned_at' IS NULL;
    WHEN 'tenant_scope_grants' THEN
        authority_kind := 'scope_grant';
        authority_key := (jsonb_build_array(row_value->>'tenant_id',row_value->>'workspace_id',row_value->>'scope_id',row_value->>'subject_class'))::text;
        old_authority := jsonb_build_array(before_row->>'policy_revision_hash',before_row->>'effective_at',before_row->>'expires_at',before_row->>'tombstoned_at');
        new_authority := jsonb_build_array(after_row->>'policy_revision_hash',after_row->>'effective_at',after_row->>'expires_at',after_row->>'tombstoned_at');
        is_active := TG_OP <> 'DELETE' AND after_row->>'tombstoned_at' IS NULL;
    WHEN 'collector_instances' THEN
        authority_kind := 'collector_instance';
        authority_key := (jsonb_build_array(row_value->>'instance_id'))::text;
        old_authority := jsonb_build_array(before_row->>'collector_kind',before_row->>'enabled',before_row->>'claims_enabled',before_row->'configuration');
        new_authority := jsonb_build_array(after_row->>'collector_kind',after_row->>'enabled',after_row->>'claims_enabled',after_row->'configuration');
        is_active := TG_OP <> 'DELETE' AND after_row->>'enabled' = 'true' AND after_row->>'claims_enabled' = 'true';
    WHEN 'ingestion_scopes' THEN
        authority_kind := 'advisory_scope';
        authority_key := (jsonb_build_array(row_value->>'scope_id'))::text;
        old_authority := jsonb_build_array(before_row->>'status',before_row->>'active_generation_id',before_row->>'collector_kind',before_row->>'source_system',before_row->>'source_key');
        new_authority := jsonb_build_array(after_row->>'status',after_row->>'active_generation_id',after_row->>'collector_kind',after_row->>'source_system',after_row->>'source_key');
        is_active := TG_OP <> 'DELETE' AND after_row->>'status' = 'active' AND after_row->>'active_generation_id' IS NOT NULL;
    ELSE
        RAISE EXCEPTION 'unsupported scanner authority ancestor %', TG_TABLE_NAME;
    END CASE;
    IF TG_OP = 'UPDATE' AND old_authority IS NOT DISTINCT FROM new_authority THEN
        RETURN NULL;
    END IF;
    INSERT INTO public.scanner_authority_epochs
      (authority_kind,authority_key,authority_epoch,active)
    VALUES (authority_kind,authority_key,nextval('public.scanner_authority_epoch_seq'),is_active)
    ON CONFLICT ON CONSTRAINT scanner_authority_epochs_pkey DO UPDATE
      SET authority_epoch=EXCLUDED.authority_epoch,active=EXCLUDED.active;
    RETURN NULL;
END;
$$;
REVOKE ALL ON FUNCTION scanner_bump_ancestor_epoch() FROM PUBLIC;
CREATE OR REPLACE TRIGGER scanner_tenant_authority_epoch
AFTER INSERT OR UPDATE OR DELETE ON tenants
FOR EACH ROW EXECUTE FUNCTION scanner_bump_ancestor_epoch();
CREATE OR REPLACE TRIGGER scanner_workspace_authority_epoch
AFTER INSERT OR UPDATE OR DELETE ON workspaces
FOR EACH ROW EXECUTE FUNCTION scanner_bump_ancestor_epoch();
CREATE OR REPLACE TRIGGER scanner_scope_grant_authority_epoch
AFTER INSERT OR UPDATE OR DELETE ON tenant_scope_grants
FOR EACH ROW EXECUTE FUNCTION scanner_bump_ancestor_epoch();
CREATE OR REPLACE TRIGGER scanner_registry_authority_epoch
AFTER INSERT OR UPDATE OR DELETE ON collector_instances
FOR EACH ROW EXECUTE FUNCTION scanner_bump_ancestor_epoch();
CREATE OR REPLACE TRIGGER scanner_advisory_scope_authority_epoch
AFTER INSERT OR UPDATE OR DELETE ON ingestion_scopes
FOR EACH ROW EXECUTE FUNCTION scanner_bump_ancestor_epoch();

INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'tenant',(jsonb_build_array(tenant_id))::text,
       status='active' AND tombstoned_at IS NULL FROM tenants
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'workspace',(jsonb_build_array(tenant_id,workspace_id))::text,
       status='active' AND tombstoned_at IS NULL FROM workspaces
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'scope_grant',
       (jsonb_build_array(tenant_id,workspace_id,scope_id,subject_class))::text,
       tombstoned_at IS NULL FROM tenant_scope_grants
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'collector_instance',(jsonb_build_array(instance_id))::text,
       enabled AND claims_enabled FROM collector_instances
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'advisory_scope',(jsonb_build_array(scope_id))::text,
       status='active' AND active_generation_id IS NOT NULL FROM ingestion_scopes
ON CONFLICT (authority_kind,authority_key) DO NOTHING;

ALTER TABLE scanner_image_target_sources
    ADD COLUMN IF NOT EXISTS canonical_observation_id BIGINT
      REFERENCES scanner_running_image_observations(observation_id),
    ADD COLUMN IF NOT EXISTS pinned_producer_principal_oid OID,
    ADD COLUMN IF NOT EXISTS pinned_source_activation_epoch BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_producer_grant_epoch BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_source_instance_epoch BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_tenant_epoch BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_workspace_epoch BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_scope_grant_epoch BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_registry_epoch BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_role_policy_revision BIGINT,
    ADD COLUMN IF NOT EXISTS pinned_database_identity UUID;
ALTER TABLE scanner_image_target_attempts
    ADD COLUMN IF NOT EXISTS claim_id TEXT,
    ADD COLUMN IF NOT EXISTS claim_fencing_token BIGINT,
    ADD COLUMN IF NOT EXISTS claim_owner_id TEXT,
    ADD COLUMN IF NOT EXISTS completed_authority_vector JSONB;
CREATE INDEX IF NOT EXISTS scanner_image_target_sources_observation_idx
    ON scanner_image_target_sources (canonical_observation_id,scan_scope_id);

-- A derived finding fact belongs to exactly one original publication and
-- target. Reuse points to that publication and never duplicates association.
CREATE TABLE IF NOT EXISTS scanner_image_result_publications (
    publication_id TEXT PRIMARY KEY,
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    scan_generation_id TEXT NOT NULL,
    binding_id TEXT NOT NULL REFERENCES scanner_image_target_sources(binding_id),
    work_item_id TEXT NOT NULL REFERENCES scanner_image_target_attempts(work_item_id),
    impact_scope_id TEXT NOT NULL,
    impact_generation_id TEXT NOT NULL,
    impact_fencing_token BIGINT NOT NULL,
    result_set_sha256 TEXT NOT NULL,
    advisory_set_sha256 TEXT NOT NULL,
    analysis_version TEXT NOT NULL,
    role_policy_revision BIGINT NOT NULL,
    database_identity UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('current','revoked','retracted')),
    published_at TIMESTAMPTZ NOT NULL,
    UNIQUE (impact_scope_id,impact_generation_id,
            impact_fencing_token,scan_scope_id)
);
CREATE INDEX IF NOT EXISTS scanner_image_result_publications_intent_idx
    ON scanner_image_result_publications
    (impact_scope_id,impact_generation_id,work_item_id,
     impact_fencing_token DESC);
CREATE TABLE IF NOT EXISTS scanner_image_result_fact_associations (
    finding_fact_id TEXT PRIMARY KEY,
    publication_id TEXT NOT NULL REFERENCES scanner_image_result_publications(publication_id),
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    scan_generation_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS scanner_image_result_fact_associations_target_idx
    ON scanner_image_result_fact_associations
    (scan_scope_id,tenant_id,finding_fact_id);
CREATE INDEX IF NOT EXISTS scanner_image_result_fact_associations_publication_idx
    ON scanner_image_result_fact_associations (publication_id,finding_fact_id);
CREATE TABLE IF NOT EXISTS scanner_image_result_fact_classifications (
    finding_fact_id TEXT PRIMARY KEY,
    source_class TEXT NOT NULL CHECK (source_class IN ('derived','static')),
    lifecycle TEXT NOT NULL CHECK (lifecycle IN ('active','retracted','orphan')),
    scan_scope_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS scanner_image_result_evidence_associations (
    finding_fact_id TEXT NOT NULL REFERENCES scanner_image_result_fact_associations(finding_fact_id),
    evidence_fact_id TEXT NOT NULL,
    publication_id TEXT NOT NULL REFERENCES scanner_image_result_publications(publication_id),
    scan_scope_id TEXT NOT NULL,
    scan_generation_id TEXT NOT NULL,
    evidence_class TEXT NOT NULL CHECK (evidence_class IN ('scan_intrinsic','advisory')),
    authority_kind TEXT,
    authority_id TEXT,
    authority_epoch BIGINT,
    CHECK ((evidence_class='scan_intrinsic' AND authority_kind IS NULL
            AND authority_id IS NULL AND authority_epoch IS NULL)
       OR (evidence_class='advisory' AND authority_kind IS NOT NULL
            AND authority_id IS NOT NULL AND authority_epoch IS NOT NULL)),
    PRIMARY KEY (finding_fact_id,evidence_fact_id)
);
CREATE TABLE IF NOT EXISTS scanner_image_result_advisory_dependencies (
    publication_id TEXT NOT NULL REFERENCES scanner_image_result_publications(publication_id),
    authority_kind TEXT NOT NULL CHECK (authority_kind IN ('public_registration','private_binding')),
    authority_id TEXT NOT NULL,
    authority_epoch BIGINT NOT NULL,
    advisory_scope_id TEXT NOT NULL,
    advisory_generation_id TEXT NOT NULL,
    advisory_scope_epoch BIGINT NOT NULL,
    provider_id TEXT NOT NULL,
    trusted_source_class TEXT NOT NULL,
    content_profile TEXT NOT NULL,
    PRIMARY KEY (publication_id,authority_kind,authority_id)
);
DO $scanner_evidence_dependency$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conname='scanner_image_result_evidence_dependency_fk'
                     AND conrelid='scanner_image_result_evidence_associations'::regclass) THEN
        ALTER TABLE scanner_image_result_evidence_associations
            ADD CONSTRAINT scanner_image_result_evidence_dependency_fk
            FOREIGN KEY (publication_id,authority_kind,authority_id)
            REFERENCES scanner_image_result_advisory_dependencies
            (publication_id,authority_kind,authority_id);
    END IF;
END
$scanner_evidence_dependency$;

-- Public sharing is an explicit server registration. A registration alone
-- cannot share older generations; each approved generation is bound to the
-- registration's current fingerprint and non-reused epoch.
CREATE TABLE IF NOT EXISTS scanner_advisory_public_registrations (
    registration_id TEXT PRIMARY KEY,
    collector_instance_id TEXT NOT NULL,
    advisory_scope_id TEXT NOT NULL,
    configuration_sha256 TEXT NOT NULL,
    pinned_collector_instance_epoch BIGINT NOT NULL,
    trusted_source_class TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    content_profile TEXT NOT NULL,
    allowed_fact_kinds JSONB NOT NULL,
    authority_epoch BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    active BOOLEAN NOT NULL,
    UNIQUE (collector_instance_id,advisory_scope_id)
);
CREATE TABLE IF NOT EXISTS scanner_advisory_public_generations (
    registration_id TEXT NOT NULL REFERENCES scanner_advisory_public_registrations(registration_id),
    advisory_generation_id TEXT NOT NULL,
    approved_configuration_sha256 TEXT NOT NULL,
    approved_authority_epoch BIGINT NOT NULL,
    pinned_advisory_scope_epoch BIGINT NOT NULL,
    active BOOLEAN NOT NULL,
    PRIMARY KEY (registration_id,advisory_generation_id)
);
CREATE TABLE IF NOT EXISTS scanner_advisory_private_bindings (
    advisory_binding_id TEXT PRIMARY KEY,
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    source_binding_id TEXT NOT NULL REFERENCES scanner_image_target_sources(binding_id),
    advisory_scope_id TEXT NOT NULL,
    advisory_generation_id TEXT NOT NULL,
    pinned_advisory_scope_epoch BIGINT NOT NULL,
    trusted_source_class TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    content_profile TEXT NOT NULL,
    allowed_fact_kinds JSONB NOT NULL,
    authority_epoch BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    active BOOLEAN NOT NULL,
    UNIQUE (scan_scope_id,source_binding_id,advisory_scope_id,advisory_generation_id)
);
CREATE TABLE IF NOT EXISTS scanner_image_result_reuse_receipts (
    reuse_receipt_id TEXT PRIMARY KEY,
    publication_id TEXT NOT NULL REFERENCES scanner_image_result_publications(publication_id),
    new_binding_id TEXT NOT NULL REFERENCES scanner_image_target_sources(binding_id),
    new_work_item_id TEXT NOT NULL REFERENCES scanner_image_target_attempts(work_item_id),
    new_claim_id TEXT NOT NULL,
    new_claim_fencing_token BIGINT NOT NULL,
    new_claim_owner_id TEXT NOT NULL,
    result_set_sha256 TEXT NOT NULL,
    advisory_set_sha256 TEXT NOT NULL,
    analysis_version TEXT NOT NULL,
    authority_vector JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (publication_id,new_binding_id,new_work_item_id)
);
CREATE TABLE IF NOT EXISTS scanner_image_result_suppressions (
    scan_scope_id TEXT NOT NULL REFERENCES scanner_image_targets(scan_scope_id),
    canonical_finding_key TEXT NOT NULL,
    operator_scope_id TEXT NOT NULL,
    policy_revision TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scan_scope_id,canonical_finding_key,operator_scope_id)
);

-- Migration ownership is the sole default writer. Explicit protected API
-- grants follow after its policy/role contract is installed.
REVOKE ALL ON scanner_running_image_producer_grants,
    scanner_running_image_scope_claims,scanner_running_image_generations,
    scanner_running_image_page_receipts,scanner_running_image_delivery_receipts,
    scanner_running_image_observations,scanner_running_image_active_scopes,
    scanner_authority_epochs,scanner_image_result_publications,
    scanner_image_result_fact_associations,
    scanner_image_result_fact_classifications,
    scanner_image_result_evidence_associations,
    scanner_image_result_advisory_dependencies,
    scanner_image_result_reuse_receipts,scanner_image_result_suppressions,
    scanner_advisory_public_registrations,
    scanner_advisory_public_generations,
    scanner_advisory_private_bindings
    FROM PUBLIC;
REVOKE ALL ON SEQUENCE scanner_authority_epoch_seq,
    scanner_running_image_observation_id_seq FROM PUBLIC;
