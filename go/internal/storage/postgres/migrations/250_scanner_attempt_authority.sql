-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Config-digest mapping is a protected verification receipt. A mutable OCI
-- generic fact may motivate a verification but cannot itself authorize one.
CREATE TABLE IF NOT EXISTS scanner_running_image_manifest_verifications (
    verification_id TEXT PRIMARY KEY,
    observation_id BIGINT NOT NULL
      REFERENCES scanner_running_image_observations(observation_id),
    registry TEXT NOT NULL,
    repository TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    config_digest TEXT NOT NULL,
    verification_epoch BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    verified_by_principal_oid OID NOT NULL,
    verified_at TIMESTAMPTZ NOT NULL,
    UNIQUE (observation_id,manifest_digest)
);
ALTER TABLE scanner_image_target_sources
    ADD COLUMN IF NOT EXISTS manifest_verification_id TEXT
      REFERENCES scanner_running_image_manifest_verifications(verification_id),
    ADD COLUMN IF NOT EXISTS pinned_manifest_verification_epoch BIGINT;
REVOKE ALL ON scanner_running_image_manifest_verifications FROM PUBLIC;

-- The caller's authenticated session role, not a supplied instance label or
-- GUC, selects a fixed scanner-worker capability. The policy helper locks
-- policy/contract first; this function then locks exact source and ancestor
-- witnesses before the work/claim rows. A denied row raises and rolls back.
CREATE OR REPLACE FUNCTION eshu_security.authorize_scanner_attempt_v1(
    p_work_item_id TEXT,p_claim_id TEXT,p_fencing_token BIGINT,p_owner_id TEXT)
RETURNS TABLE (
    scan_scope_id TEXT,scan_generation_id TEXT,binding_id TEXT,
    work_item_id TEXT,claim_id TEXT,claim_fencing_token BIGINT,
    claim_owner_id TEXT,authority_vector JSONB)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    policy_row eshu_security.role_policy_state%ROWTYPE;
    binding_row public.scanner_image_target_sources%ROWTYPE;
    target_row public.scanner_image_targets%ROWTYPE;
    attempt_row public.scanner_image_target_attempts%ROWTYPE;
    observation_row public.scanner_running_image_observations%ROWTYPE;
    generation_row public.scanner_running_image_generations%ROWTYPE;
    active_row public.scanner_running_image_active_scopes%ROWTYPE;
    grant_row public.scanner_running_image_producer_grants%ROWTYPE;
    item_row public.workflow_work_items%ROWTYPE;
    claim_row public.workflow_claims%ROWTYPE;
    tenant_row public.tenants%ROWTYPE;
    workspace_row public.workspaces%ROWTYPE;
    scope_grant_row public.tenant_scope_grants%ROWTYPE;
    epoch_row public.scanner_authority_epochs%ROWTYPE;
    source_instance public.collector_instances%ROWTYPE;
    scanner_instance public.collector_instances%ROWTYPE;
    session_oid OID;
    source_instance_epoch BIGINT;
    scanner_instance_epoch BIGINT;
    tenant_epoch BIGINT := 0;
    workspace_epoch BIGINT := 0;
    scope_grant_epoch BIGINT := 0;
    instance_id TEXT;
    domain_count INTEGER;
    domain_hash TEXT;
BEGIN
    SELECT * INTO policy_row FROM eshu_security.role_policy_state
    WHERE singleton;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'scanner attempt role policy absent' USING ERRCODE='28000';
    END IF;
    session_oid := eshu_security.assert_protected_capability_v1(
        policy_row.policy_revision,policy_row.database_identity,'runtime-writer');
    IF NOT EXISTS (SELECT 1 FROM eshu_security.role_contracts AS contract
                   WHERE contract.policy_revision=policy_row.policy_revision
                     AND contract.session_role_oid=session_oid
                     AND contract.workload_class='scanner-worker'
                     AND contract.connection_route='writer'
                     AND contract.enabled) THEN
        RAISE EXCEPTION 'scanner attempt principal is not scanner-worker' USING ERRCODE='28000';
    END IF;
    SELECT source.* INTO binding_row
    FROM public.scanner_image_target_attempts AS attempt
    JOIN public.scanner_image_target_sources AS source
      ON source.binding_id=attempt.binding_id
    WHERE attempt.work_item_id=p_work_item_id;
    IF NOT FOUND OR binding_row.canonical_observation_id IS NULL
       OR binding_row.pinned_producer_principal_oid IS NULL THEN
        RAISE EXCEPTION 'scanner attempt has no canonical source binding' USING ERRCODE='28000';
    END IF;
    SELECT * INTO target_row FROM public.scanner_image_targets AS target
    WHERE target.scan_scope_id=binding_row.scan_scope_id;
    SELECT * INTO observation_row
    FROM public.scanner_running_image_observations
    WHERE observation_id=binding_row.canonical_observation_id;
    IF target_row.scan_scope_id IS NULL OR observation_row.observation_id IS NULL THEN
        RAISE EXCEPTION 'scanner attempt target or observation absent' USING ERRCODE='28000';
    END IF;
    SELECT * INTO grant_row
    FROM public.scanner_running_image_producer_grants AS allowed
    WHERE allowed.principal_oid=binding_row.pinned_producer_principal_oid
      AND allowed.producer_instance_id=observation_row.producer_instance_id
      AND allowed.source_scope_id=observation_row.source_scope_id
      AND allowed.producer_class=observation_row.producer_class
      AND allowed.observation_class=observation_row.observation_class
      AND allowed.grant_epoch=binding_row.pinned_producer_grant_epoch
      AND allowed.active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'scanner attempt producer grant revoked' USING ERRCODE='28000';
    END IF;
    SELECT * INTO generation_row
    FROM public.scanner_running_image_generations AS generation
    WHERE generation.producer_instance_id=observation_row.producer_instance_id
      AND generation.source_scope_id=observation_row.source_scope_id
      AND generation.source_generation_id=observation_row.source_generation_id
      AND generation.principal_oid=grant_row.principal_oid
      AND generation.producer_grant_epoch=grant_row.grant_epoch
      AND generation.status='published' FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'scanner attempt source generation not published' USING ERRCODE='28000';
    END IF;
    SELECT * INTO observation_row
    FROM public.scanner_running_image_observations AS observation
    WHERE observation.observation_id=binding_row.canonical_observation_id
      AND observation.producer_instance_id=binding_row.source_collector_instance_id
      AND observation.source_scope_id=binding_row.source_scope_id
      AND observation.source_generation_id=binding_row.source_generation_id
      AND observation.registry=binding_row.registry
      AND observation.repository=binding_row.repository
      AND observation.runtime_digest=binding_row.runtime_digest
      AND observation.runtime_digest_kind=binding_row.runtime_digest_kind
      AND observation.runtime_image_reference=binding_row.runtime_image_reference
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'scanner attempt observation binding mismatch' USING ERRCODE='28000';
    END IF;
    SELECT * INTO active_row
    FROM public.scanner_running_image_active_scopes AS active_source
    WHERE active_source.producer_instance_id=observation_row.producer_instance_id
      AND active_source.source_scope_id=observation_row.source_scope_id
      AND active_source.published_generation_id=observation_row.source_generation_id
      AND active_source.activation_epoch=binding_row.pinned_source_activation_epoch
      AND active_source.active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'scanner attempt source activation revoked' USING ERRCODE='28000';
    END IF;
    IF binding_row.runtime_digest_kind='manifest' THEN
        IF binding_row.runtime_digest<>target_row.manifest_digest THEN
            RAISE EXCEPTION 'scanner attempt manifest digest mismatch' USING ERRCODE='28000';
        END IF;
    ELSIF binding_row.runtime_digest_kind='config' THEN
        PERFORM 1
        FROM public.scanner_running_image_manifest_verifications AS verified
        WHERE verified.verification_id=binding_row.manifest_verification_id
          AND verified.verification_epoch=binding_row.pinned_manifest_verification_epoch
          AND verified.observation_id=observation_row.observation_id
          AND verified.registry=observation_row.registry
          AND verified.repository=observation_row.repository
          AND verified.config_digest=observation_row.runtime_digest
          AND verified.manifest_digest=target_row.manifest_digest FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'scanner attempt manifest mapping unverified' USING ERRCODE='28000';
        END IF;
    ELSE
        RAISE EXCEPTION 'scanner attempt runtime digest kind unsupported' USING ERRCODE='28000';
    END IF;
    IF target_row.authorization_namespace='hosted' THEN
        SELECT * INTO tenant_row FROM public.tenants
        WHERE tenant_id=target_row.tenant_id AND status='active'
          AND tombstoned_at IS NULL
          AND policy_revision_hash=binding_row.policy_revision_hash FOR SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION 'scanner attempt tenant inactive' USING ERRCODE='28000'; END IF;
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs
        WHERE authority_kind='tenant'
          AND authority_key=jsonb_build_array(target_row.tenant_id)::text
          AND active FOR SHARE;
        IF NOT FOUND OR epoch_row.authority_epoch<>binding_row.pinned_tenant_epoch THEN
            RAISE EXCEPTION 'scanner attempt tenant epoch changed' USING ERRCODE='28000';
        END IF;
        tenant_epoch := epoch_row.authority_epoch;
        SELECT * INTO workspace_row FROM public.workspaces
        WHERE tenant_id=target_row.tenant_id
          AND workspace_id=target_row.workspace_id AND status='active'
          AND tombstoned_at IS NULL
          AND policy_revision_hash=binding_row.policy_revision_hash FOR SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION 'scanner attempt workspace inactive' USING ERRCODE='28000'; END IF;
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs
        WHERE authority_kind='workspace'
          AND authority_key=jsonb_build_array(target_row.tenant_id,
                            target_row.workspace_id)::text
          AND active FOR SHARE;
        IF NOT FOUND OR epoch_row.authority_epoch<>binding_row.pinned_workspace_epoch THEN
            RAISE EXCEPTION 'scanner attempt workspace epoch changed' USING ERRCODE='28000';
        END IF;
        workspace_epoch := epoch_row.authority_epoch;
        SELECT * INTO scope_grant_row FROM public.tenant_scope_grants
        WHERE tenant_id=target_row.tenant_id
          AND workspace_id=target_row.workspace_id
          AND scope_id=binding_row.source_scope_id
          AND subject_class=target_row.subject_class
          AND policy_revision_hash=binding_row.policy_revision_hash
          AND tombstoned_at IS NULL AND effective_at<=clock_timestamp()
          AND (expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION 'scanner attempt scope grant revoked' USING ERRCODE='28000'; END IF;
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs
        WHERE authority_kind='scope_grant'
          AND authority_key=jsonb_build_array(target_row.tenant_id,
               target_row.workspace_id,binding_row.source_scope_id,
               target_row.subject_class)::text AND active FOR SHARE;
        IF NOT FOUND OR epoch_row.authority_epoch<>binding_row.pinned_scope_grant_epoch THEN
            RAISE EXCEPTION 'scanner attempt scope grant epoch changed' USING ERRCODE='28000';
        END IF;
        scope_grant_epoch := epoch_row.authority_epoch;
    ELSIF target_row.authorization_namespace<>'local'
       OR target_row.tenant_id<>'' OR target_row.workspace_id<>''
       OR target_row.subject_class<>'' THEN
        RAISE EXCEPTION 'scanner attempt namespace invalid' USING ERRCODE='28000';
    END IF;
    FOR instance_id IN
        SELECT DISTINCT value FROM unnest(ARRAY[
            binding_row.source_collector_instance_id,
            binding_row.scanner_collector_instance_id]) AS ids(value)
        ORDER BY value
    LOOP
        IF instance_id=binding_row.source_collector_instance_id THEN
            SELECT * INTO source_instance FROM public.collector_instances
            WHERE collector_instances.instance_id=instance_id
              AND collector_kind=CASE
                    WHEN observation_row.producer_class='kubernetes_pod'
                    THEN 'kubernetes_live' ELSE 'aws' END
              AND enabled AND claims_enabled FOR SHARE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'scanner attempt source instance disabled' USING ERRCODE='28000';
            END IF;
        END IF;
        IF instance_id=binding_row.scanner_collector_instance_id THEN
            SELECT * INTO scanner_instance FROM public.collector_instances
            WHERE collector_instances.instance_id=instance_id
              AND collector_kind='scanner_worker'
              AND enabled AND claims_enabled
              AND configuration->>'derive_from_running_images'='true'
            FOR SHARE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'scanner attempt scanner instance disabled' USING ERRCODE='28000';
            END IF;
        END IF;
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs
        WHERE authority_kind='collector_instance'
          AND authority_key=jsonb_build_array(instance_id)::text
          AND active FOR SHARE;
        IF NOT FOUND THEN RAISE EXCEPTION 'scanner attempt instance epoch absent' USING ERRCODE='28000'; END IF;
        IF instance_id=binding_row.source_collector_instance_id THEN
            source_instance_epoch := epoch_row.authority_epoch;
        END IF;
        IF instance_id=binding_row.scanner_collector_instance_id THEN
            scanner_instance_epoch := epoch_row.authority_epoch;
        END IF;
    END LOOP;
    IF source_instance_epoch<>binding_row.pinned_source_instance_epoch
       OR scanner_instance_epoch<>binding_row.pinned_registry_epoch
       OR source_instance.instance_id<>observation_row.producer_instance_id THEN
        RAISE EXCEPTION 'scanner attempt collector epoch changed' USING ERRCODE='28000';
    END IF;
    SELECT count(*),min(encode(pg_catalog.sha256(pg_catalog.convert_to(
        domain.value::text,'UTF8')),'hex'))
    INTO domain_count,domain_hash
    FROM jsonb_array_elements(COALESCE(
      scanner_instance.configuration->'registry_domains','[]'::jsonb))
      AS domain(value)
    WHERE domain.value->>'authorization_domain'=target_row.authorization_domain
      AND domain.value->>'registry'=binding_row.registry;
    IF domain_count<>1 OR domain_hash<>binding_row.registry_domain_config_sha256
       OR (SELECT count(*) FROM jsonb_array_elements(COALESCE(
            scanner_instance.configuration->'registry_domains','[]'::jsonb)) AS d(value)
           WHERE d.value->>'authorization_domain'=target_row.authorization_domain)<>1 THEN
        RAISE EXCEPTION 'scanner attempt registry authority changed' USING ERRCODE='28000';
    END IF;
    IF binding_row.pinned_role_policy_revision<>policy_row.policy_revision
       OR binding_row.pinned_database_identity<>policy_row.database_identity THEN
        RAISE EXCEPTION 'scanner attempt role policy changed' USING ERRCODE='28000';
    END IF;
    SELECT * INTO target_row FROM public.scanner_image_targets AS target
    WHERE target.scan_scope_id=binding_row.scan_scope_id FOR SHARE;
    SELECT * INTO binding_row FROM public.scanner_image_target_sources AS source
    WHERE source.binding_id=binding_row.binding_id FOR SHARE;
    SELECT * INTO attempt_row FROM public.scanner_image_target_attempts AS attempt
    WHERE attempt.work_item_id=p_work_item_id
      AND attempt.binding_id=binding_row.binding_id
      AND attempt.scan_scope_id=target_row.scan_scope_id FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'scanner attempt binding changed' USING ERRCODE='28000'; END IF;
    SELECT * INTO item_row FROM public.workflow_work_items AS item
    WHERE item.work_item_id=p_work_item_id
      AND item.scope_id=target_row.scan_scope_id
      AND item.collector_kind='scanner_worker'
      AND item.collector_instance_id=binding_row.scanner_collector_instance_id
      AND item.authorization_scope_id=binding_row.source_scope_id
      AND item.tenant_id=target_row.tenant_id
      AND item.workspace_id=target_row.workspace_id
      AND item.subject_class=target_row.subject_class
      AND item.policy_revision_hash=binding_row.policy_revision_hash
      AND item.status='claimed' AND item.current_claim_id=p_claim_id
      AND item.current_fencing_token=p_fencing_token
      AND item.current_owner_id=p_owner_id
      AND item.lease_expires_at>clock_timestamp() FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'scanner attempt work claim stale' USING ERRCODE='28000'; END IF;
    SELECT * INTO claim_row FROM public.workflow_claims AS claim
    WHERE claim.claim_id=p_claim_id AND claim.work_item_id=p_work_item_id
      AND claim.fencing_token=p_fencing_token AND claim.owner_id=p_owner_id
      AND claim.status='active' AND claim.lease_expires_at>clock_timestamp()
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'scanner attempt claim stale' USING ERRCODE='28000'; END IF;
    RETURN QUERY SELECT target_row.scan_scope_id,item_row.generation_id,
                        binding_row.binding_id,item_row.work_item_id,
                        claim_row.claim_id,claim_row.fencing_token,
                        claim_row.owner_id,
                        jsonb_build_object(
                         'source_activation_epoch',active_row.activation_epoch,
                         'producer_grant_epoch',grant_row.grant_epoch,
                         'source_instance_epoch',source_instance_epoch,
                         'tenant_epoch',tenant_epoch,
                         'workspace_epoch',workspace_epoch,
                         'scope_grant_epoch',scope_grant_epoch,
                         'registry_epoch',scanner_instance_epoch,
                         'role_policy_revision',policy_row.policy_revision,
                         'database_identity',policy_row.database_identity);
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.authorize_scanner_attempt_v1(
    TEXT,TEXT,BIGINT,TEXT) FROM PUBLIC;
