-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Authority-state denials in validate_scanner_source_admission_v1 return no
-- rows instead of raising (#9299 P2-3). The admission proof runs many
-- bindings inside one transaction with no savepoints, so an exception on one
-- binding would abort its siblings; a no-row denial keeps the transaction
-- healthy while denying exactly that binding, and Go maps no-row to
-- ErrScannerTargetNotAuthorized (a counted per-binding skip with cursor
-- advance). Caller-shape and identity-invariant denials still raise: bad
-- input, a local boundary carrying hosted fields, a hosted boundary missing
-- fields, and coincident scanner/source instances are coordinator bugs, not
-- lifecycle, and must abort the page with the cursor held.
CREATE OR REPLACE FUNCTION eshu_security.validate_scanner_source_admission_v1(
    p_observation_id BIGINT,p_scanner_instance TEXT,p_namespace TEXT,
    p_tenant TEXT,p_workspace TEXT,p_subject TEXT,p_policy_hash TEXT,
    p_auth_domain TEXT,p_manifest_digest TEXT,p_verification_id TEXT,
    p_local_scope_ids TEXT[],p_role_revision BIGINT,p_database_identity UUID)
RETURNS TABLE (
    source_instance_id TEXT,source_scope_id TEXT,source_generation_id TEXT,
    source_registry TEXT,source_repository TEXT,source_runtime_ref TEXT,
    source_runtime_digest TEXT,source_runtime_digest_kind TEXT,
    source_payload_sha256 TEXT,domain_config_sha256 TEXT,
    domain_config JSONB,producer_principal_oid OID,
    source_activation_epoch BIGINT,producer_grant_epoch BIGINT,
    source_instance_epoch BIGINT,tenant_epoch BIGINT,
    workspace_epoch BIGINT,scope_grant_epoch BIGINT,
    registry_epoch BIGINT,role_policy_revision BIGINT,
    database_identity UUID,manifest_verification_epoch BIGINT)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    observation_row public.scanner_running_image_observations%ROWTYPE;
    generation_row public.scanner_running_image_generations%ROWTYPE;
    grant_row public.scanner_running_image_producer_grants%ROWTYPE;
    active_row public.scanner_running_image_active_scopes%ROWTYPE;
    instance_row public.collector_instances%ROWTYPE;
    scanner_row public.collector_instances%ROWTYPE;
    epoch_row public.scanner_authority_epochs%ROWTYPE;
    verification_row public.scanner_running_image_manifest_verifications%ROWTYPE;
    v_source_epoch BIGINT;
    v_scanner_epoch BIGINT;
    v_tenant_epoch BIGINT := 0;
    v_workspace_epoch BIGINT := 0;
    v_scope_grant_epoch BIGINT := 0;
    v_domain_count INTEGER;
    v_domain JSONB;
    v_instance_id TEXT;
    v_verification_epoch BIGINT := 0;
BEGIN
    PERFORM eshu_security.authorize_scanner_discovery_v1(
        p_role_revision,p_database_identity);
    IF p_observation_id<=0 OR p_scanner_instance IS NULL
       OR p_scanner_instance='' OR p_auth_domain IS NULL
       OR p_auth_domain='' OR p_manifest_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_namespace NOT IN ('local','hosted') THEN
        RAISE EXCEPTION 'scanner canonical admission input invalid';
    END IF;
    -- Canonical observations are immutable once staged. This unlocked key
    -- read only chooses the exact rows to lock in the documented order.
    SELECT * INTO observation_row
    FROM public.scanner_running_image_observations AS observation
    WHERE observation.observation_id=p_observation_id;
    IF NOT FOUND THEN
        -- Authority state: the observation retired after discovery.
        RETURN;
    END IF;
    SELECT * INTO generation_row
    FROM public.scanner_running_image_generations AS generation
    WHERE generation.producer_instance_id=observation_row.producer_instance_id
      AND generation.source_scope_id=observation_row.source_scope_id
      AND generation.source_generation_id=observation_row.source_generation_id;
    IF NOT FOUND THEN
        -- Authority state: the source generation retired after discovery.
        RETURN;
    END IF;
    SELECT * INTO grant_row
    FROM public.scanner_running_image_producer_grants AS allowed
    WHERE allowed.principal_oid=generation_row.principal_oid
      AND allowed.producer_instance_id=observation_row.producer_instance_id
      AND allowed.source_scope_id=observation_row.source_scope_id
      AND allowed.producer_class=observation_row.producer_class
      AND allowed.observation_class=observation_row.observation_class
      AND allowed.grant_epoch=generation_row.producer_grant_epoch
      AND allowed.contract_version=1 AND allowed.active FOR SHARE;
    IF NOT FOUND THEN RETURN; END IF; -- Authority state: producer grant revoked.
    SELECT * INTO generation_row
    FROM public.scanner_running_image_generations AS generation
    WHERE generation.producer_instance_id=observation_row.producer_instance_id
      AND generation.source_scope_id=observation_row.source_scope_id
      AND generation.source_generation_id=observation_row.source_generation_id
      AND generation.principal_oid=grant_row.principal_oid
      AND generation.producer_grant_epoch=grant_row.grant_epoch
      AND generation.status='published' FOR SHARE;
    IF NOT FOUND THEN RETURN; END IF; -- Authority state: generation unpublished.
    SELECT * INTO observation_row
    FROM public.scanner_running_image_observations AS observation
    WHERE observation.observation_id=p_observation_id
      AND observation.producer_instance_id=generation_row.producer_instance_id
      AND observation.source_scope_id=generation_row.source_scope_id
      AND observation.source_generation_id=generation_row.source_generation_id
      AND observation.producer_class=generation_row.producer_class
      AND observation.observation_class=generation_row.observation_class
    FOR SHARE;
    IF NOT FOUND THEN RETURN; END IF; -- Authority state: observation changed under proof.
    SELECT * INTO active_row
    FROM public.scanner_running_image_active_scopes AS active_source
    WHERE active_source.producer_instance_id=observation_row.producer_instance_id
      AND active_source.source_scope_id=observation_row.source_scope_id
      AND active_source.published_generation_id=observation_row.source_generation_id
      AND active_source.active FOR SHARE;
    IF NOT FOUND THEN RETURN; END IF; -- Authority state: active scope deactivated.
    IF p_namespace='local' THEN
        IF COALESCE(p_tenant,'')<>'' OR COALESCE(p_workspace,'')<>''
           OR COALESCE(p_subject,'')<>'' OR COALESCE(p_policy_hash,'')<>'' THEN
            RAISE EXCEPTION 'scanner local boundary invalid';
        END IF;
        IF NOT observation_row.source_scope_id=ANY(p_local_scope_ids) THEN
            -- Authority state: scope dropped from the local allowlist.
            RETURN;
        END IF;
    ELSE
        IF COALESCE(p_tenant,'')='' OR COALESCE(p_workspace,'')=''
           OR COALESCE(p_subject,'')='' OR COALESCE(p_policy_hash,'')='' THEN
            RAISE EXCEPTION 'scanner hosted boundary invalid';
        END IF;
        PERFORM 1 FROM public.tenants AS tenant
        WHERE tenant.tenant_id=p_tenant AND tenant.status='active'
          AND tenant.tombstoned_at IS NULL
          AND tenant.policy_revision_hash=p_policy_hash FOR SHARE;
        IF NOT FOUND THEN RETURN; END IF; -- Authority state: tenant revoked.
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs AS epoch
        WHERE epoch.authority_kind='tenant'
          AND epoch.authority_key=jsonb_build_array(p_tenant)::text
          AND epoch.active FOR SHARE;
        IF NOT FOUND THEN RETURN; END IF; -- Authority state: tenant epoch absent.
        v_tenant_epoch:=epoch_row.authority_epoch;
        PERFORM 1 FROM public.workspaces AS workspace
        WHERE workspace.tenant_id=p_tenant AND workspace.workspace_id=p_workspace
          AND workspace.status='active' AND workspace.tombstoned_at IS NULL
          AND workspace.policy_revision_hash=p_policy_hash FOR SHARE;
        IF NOT FOUND THEN RETURN; END IF; -- Authority state: workspace revoked.
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs AS epoch
        WHERE epoch.authority_kind='workspace'
          AND epoch.authority_key=jsonb_build_array(p_tenant,p_workspace)::text
          AND epoch.active FOR SHARE;
        IF NOT FOUND THEN RETURN; END IF; -- Authority state: workspace epoch absent.
        v_workspace_epoch:=epoch_row.authority_epoch;
        PERFORM 1 FROM public.tenant_scope_grants AS source_grant
        WHERE source_grant.tenant_id=p_tenant
          AND source_grant.workspace_id=p_workspace
          AND source_grant.scope_id=observation_row.source_scope_id
          AND source_grant.subject_class=p_subject
          AND source_grant.policy_revision_hash=p_policy_hash
          AND source_grant.tombstoned_at IS NULL
          AND source_grant.effective_at<=clock_timestamp()
          AND (source_grant.expires_at IS NULL OR
               source_grant.expires_at>clock_timestamp()) FOR SHARE;
        IF NOT FOUND THEN RETURN; END IF; -- Authority state: scope grant revoked.
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs AS epoch
        WHERE epoch.authority_kind='scope_grant'
          AND epoch.authority_key=jsonb_build_array(
              p_tenant,p_workspace,observation_row.source_scope_id,p_subject)::text
          AND epoch.active FOR SHARE;
        IF NOT FOUND THEN RETURN; END IF; -- Authority state: scope grant epoch absent.
        v_scope_grant_epoch:=epoch_row.authority_epoch;
    END IF;
    IF p_scanner_instance=observation_row.producer_instance_id THEN
        RAISE EXCEPTION 'scanner and source instances cannot coincide';
    END IF;
    FOR v_instance_id IN
        SELECT DISTINCT instance_id FROM unnest(ARRAY[
            observation_row.producer_instance_id,p_scanner_instance]) AS ids(instance_id)
        ORDER BY instance_id
    LOOP
        IF v_instance_id=observation_row.producer_instance_id THEN
            SELECT * INTO instance_row FROM public.collector_instances AS source_instance
            WHERE source_instance.instance_id=v_instance_id
              AND source_instance.collector_kind=CASE
                  WHEN observation_row.producer_class='kubernetes_pod'
                  THEN 'kubernetes_live' ELSE 'aws' END
              AND source_instance.enabled AND source_instance.claims_enabled
            FOR SHARE;
            IF NOT FOUND THEN RETURN; END IF; -- Authority state: producer disabled.
        END IF;
        IF v_instance_id=p_scanner_instance THEN
            SELECT * INTO scanner_row FROM public.collector_instances AS scanner
            WHERE scanner.instance_id=v_instance_id
              AND scanner.collector_kind='scanner_worker'
              AND scanner.enabled AND scanner.claims_enabled
              AND scanner.configuration->>'derive_from_running_images'='true'
            FOR SHARE;
            IF NOT FOUND THEN RETURN; END IF; -- Authority state: scanner disabled.
        END IF;
        SELECT * INTO epoch_row FROM public.scanner_authority_epochs AS epoch
        WHERE epoch.authority_kind='collector_instance'
          AND epoch.authority_key=jsonb_build_array(v_instance_id)::text
          AND epoch.active FOR SHARE;
        IF NOT FOUND THEN RETURN; END IF; -- Authority state: instance epoch absent.
        IF v_instance_id=observation_row.producer_instance_id THEN
            v_source_epoch:=epoch_row.authority_epoch;
        END IF;
        IF v_instance_id=p_scanner_instance THEN
            v_scanner_epoch:=epoch_row.authority_epoch;
        END IF;
    END LOOP;
    SELECT count(*),min(domain.value::text)::jsonb
      INTO v_domain_count,v_domain
    FROM jsonb_array_elements(COALESCE(
         scanner_row.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
    WHERE domain.value->>'authorization_domain'=p_auth_domain
      AND domain.value->>'registry'=observation_row.registry;
    IF v_domain_count<>1 OR
       (SELECT count(*) FROM jsonb_array_elements(COALESCE(
          scanner_row.configuration->'registry_domains','[]'::jsonb)) AS peer(value)
        WHERE peer.value->>'authorization_domain'=p_auth_domain)<>1 THEN
        -- Authority state: registry domain config removed or ambiguous.
        RETURN;
    END IF;
    IF observation_row.runtime_digest_kind='manifest' THEN
        IF observation_row.runtime_digest<>p_manifest_digest
           OR COALESCE(p_verification_id,'')<>'' THEN
            -- Authority state: planner manifest claim differs from canonical.
            RETURN;
        END IF;
    ELSIF observation_row.runtime_digest_kind='config' THEN
        SELECT * INTO verification_row
        FROM public.scanner_running_image_manifest_verifications AS verified
        WHERE verified.verification_id=p_verification_id
          AND verified.observation_id=p_observation_id
          AND verified.registry=observation_row.registry
          AND verified.repository=observation_row.repository
          AND verified.config_digest=observation_row.runtime_digest
          AND verified.manifest_digest=p_manifest_digest FOR SHARE;
        IF NOT FOUND OR (SELECT count(*) FROM
             public.scanner_running_image_manifest_verifications AS peer
             WHERE peer.observation_id=p_observation_id)<>1 THEN
            -- Authority state: verification row deleted, forged, or ambiguous.
            RETURN;
        END IF;
        v_verification_epoch:=verification_row.verification_epoch;
    ELSE
        -- Authority state: unknown digest kind is legitimate observation state.
        RETURN;
    END IF;
    RETURN QUERY SELECT observation_row.producer_instance_id,
        observation_row.source_scope_id,observation_row.source_generation_id,
        observation_row.registry,observation_row.repository,
        observation_row.runtime_image_reference,observation_row.runtime_digest,
        observation_row.runtime_digest_kind,observation_row.payload_sha256,
        encode(pg_catalog.sha256(pg_catalog.convert_to(v_domain::text,'UTF8')),'hex'),
        v_domain,grant_row.principal_oid,active_row.activation_epoch,
        grant_row.grant_epoch,v_source_epoch,v_tenant_epoch,
        v_workspace_epoch,v_scope_grant_epoch,v_scanner_epoch,
        p_role_revision,p_database_identity,v_verification_epoch;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.validate_scanner_source_admission_v1(
    BIGINT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,
    TEXT[],BIGINT,UUID) FROM PUBLIC;
