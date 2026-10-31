-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Reducer impact publication authorize gate. The owner-only capability
-- assert cannot run as the reducer's runtime-writer, so authorization runs
-- here as SECURITY DEFINER, following authorize_static_impact_public_v1
-- (#9299). It locks and rechecks the
-- complete static and derived witness set before the caller's
-- conflict-domain lock. The companion persist lives in 269. Both take the
-- same canonical set JSON the Go adapter marshals: static_findings
-- (shaped for the static authorizer) and target_publications (one entry
-- per derived target with its witness, finding payloads, and advisory
-- dependencies).
INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
VALUES
    (1,'runtime-writer','function',
     'eshu_security.authorize_impact_publication_v1(text,text,bigint,jsonb,bigint,uuid)',
     'EXECUTE',TRUE)
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO NOTHING;

-- authorize_impact_publication_v1 locks and validates the complete static
-- and derived witness set. Every SHARE lock is retained by the caller's
-- transaction through publication commit or rollback, so a concurrent
-- revoke cannot slip between this check and the finding write.
CREATE OR REPLACE FUNCTION eshu_security.authorize_impact_publication_v1(
    p_impact_scope TEXT,p_impact_generation TEXT,p_impact_fence BIGINT,
    p_set JSONB,p_role_revision BIGINT,p_database_identity UUID)
RETURNS VOID LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_target JSONB;
BEGIN
    IF p_impact_scope IS NULL OR p_impact_scope=''
       OR p_impact_generation IS NULL OR p_impact_generation=''
       OR p_impact_fence<=0 OR p_set IS NULL
       OR jsonb_typeof(p_set)<>'object'
       OR jsonb_typeof(COALESCE(p_set->'static_findings','[]'))<>'array'
       OR jsonb_typeof(COALESCE(p_set->'target_publications','[]'))<>'array' THEN
        RAISE EXCEPTION 'impact publication input invalid';
    END IF;
    PERFORM eshu_security.authorize_static_impact_public_v1(
        p_impact_scope,p_impact_generation,p_impact_fence,
        COALESCE(p_set->'static_findings','[]'::jsonb),
        p_role_revision,p_database_identity);
    FOR v_target IN
        SELECT value FROM jsonb_array_elements(
            COALESCE(p_set->'target_publications','[]'::jsonb)) AS target(value)
        ORDER BY target.value->>'scan_target_id',target.value->>'binding_id'
    LOOP
        PERFORM eshu_security.authorize_impact_target_publication_v1(
            p_impact_scope,p_impact_generation,p_impact_fence,v_target,
            p_role_revision,p_database_identity);
    END LOOP;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.authorize_impact_publication_v1(
    TEXT,TEXT,BIGINT,JSONB,BIGINT,UUID) FROM PUBLIC;

-- authorize_impact_target_publication_v1 rechecks one derived target
-- witness: the binding, target, completed attempt, and scan receipt still
-- agree, and every pinned source/advisory authority row is still current.
-- It mirrors authorize_scanner_attempt_v1 without the work-claim tail: by
-- reducer time the scanner claim is long released, so the completed
-- receipt plus its authority vector is the standing proof instead.
CREATE OR REPLACE FUNCTION eshu_security.authorize_impact_target_publication_v1(
    p_impact_scope TEXT,p_impact_generation TEXT,p_impact_fence BIGINT,
    p_target JSONB,p_role_revision BIGINT,p_database_identity UUID)
RETURNS VOID LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_binding public.scanner_image_target_sources%ROWTYPE;
    v_target public.scanner_image_targets%ROWTYPE;
    v_attempt public.scanner_image_target_attempts%ROWTYPE;
    v_result public.scanner_image_scan_results%ROWTYPE;
    v_generation public.scanner_running_image_generations%ROWTYPE;
    v_activation public.scanner_running_image_active_scopes%ROWTYPE;
    v_grant public.scanner_running_image_producer_grants%ROWTYPE;
    v_observation public.scanner_running_image_observations%ROWTYPE;
    v_policy eshu_security.role_policy_state%ROWTYPE;
    v_dependency JSONB;
    v_evidence JSONB;
    v_advisory RECORD;
    v_expected_vector JSONB;
BEGIN
    IF p_target->>'scan_target_id' IS NULL OR p_target->>'binding_id' IS NULL
       OR p_target->>'work_item_id' IS NULL OR p_target->>'claim_id' IS NULL
       OR p_target->>'claim_owner_id' IS NULL
       OR (p_target->>'claim_fencing_token')::BIGINT<=0
       OR p_target->>'scan_generation_id' IS NULL
       OR p_target->>'analysis_version' IS NULL THEN
        RAISE EXCEPTION 'impact target publication witness invalid';
    END IF;
    SELECT * INTO v_binding FROM public.scanner_image_target_sources AS source
    WHERE source.binding_id=p_target->>'binding_id' FOR SHARE;
    IF NOT FOUND OR v_binding.scan_scope_id<>p_target->>'scan_target_id' THEN
        RAISE EXCEPTION 'impact publication source binding changed';
    END IF;
    SELECT * INTO v_target FROM public.scanner_image_targets AS target
    WHERE target.scan_scope_id=p_target->>'scan_target_id' FOR SHARE;
    IF NOT FOUND OR v_target.authorization_namespace<>'hosted'
       OR v_target.tenant_id<>COALESCE(p_target->>'tenant_id','')
       OR v_target.workspace_id<>COALESCE(p_target->>'workspace_id','') THEN
        RAISE EXCEPTION 'impact publication scan target revoked';
    END IF;
    SELECT * INTO v_attempt FROM public.scanner_image_target_attempts AS attempt
    WHERE attempt.work_item_id=p_target->>'work_item_id'
      AND attempt.binding_id=v_binding.binding_id
      AND attempt.scan_scope_id=v_target.scan_scope_id FOR SHARE;
    IF NOT FOUND OR v_attempt.status<>'completed'
       OR v_attempt.claim_id<>p_target->>'claim_id'
       OR v_attempt.claim_fencing_token<>(p_target->>'claim_fencing_token')::BIGINT
       OR v_attempt.claim_owner_id<>p_target->>'claim_owner_id' THEN
        RAISE EXCEPTION 'impact publication scan attempt changed';
    END IF;
    v_expected_vector := jsonb_build_object(
        'source_activation_epoch',v_binding.pinned_source_activation_epoch,
        'producer_grant_epoch',v_binding.pinned_producer_grant_epoch,
        'source_instance_epoch',v_binding.pinned_source_instance_epoch,
        'tenant_epoch',v_binding.pinned_tenant_epoch,
        'workspace_epoch',v_binding.pinned_workspace_epoch,
        'scope_grant_epoch',v_binding.pinned_scope_grant_epoch,
        'registry_epoch',v_binding.pinned_registry_epoch,
        'role_policy_revision',v_binding.pinned_role_policy_revision,
        'database_identity',v_binding.pinned_database_identity);
    SELECT * INTO v_result FROM public.scanner_image_scan_results AS result
    WHERE result.work_item_id=v_attempt.work_item_id
      AND result.binding_id=v_binding.binding_id
      AND result.scan_scope_id=v_target.scan_scope_id
      AND result.scan_generation_id=p_target->>'scan_generation_id'
      AND result.claim_id=v_attempt.claim_id
      AND result.claim_fencing_token=v_attempt.claim_fencing_token
      AND result.claim_owner_id=v_attempt.claim_owner_id
      AND result.completed_authority_vector=v_expected_vector FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication scan receipt changed';
    END IF;
    SELECT * INTO v_grant FROM public.scanner_running_image_producer_grants AS allowed
    WHERE allowed.principal_oid=v_binding.pinned_producer_principal_oid
      AND allowed.producer_instance_id=v_binding.source_collector_instance_id
      AND allowed.source_scope_id=v_binding.source_scope_id
      AND allowed.grant_epoch=v_binding.pinned_producer_grant_epoch
      AND allowed.active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication producer grant revoked';
    END IF;
    SELECT * INTO v_generation FROM public.scanner_running_image_generations AS generation
    WHERE generation.producer_instance_id=v_binding.source_collector_instance_id
      AND generation.source_scope_id=v_binding.source_scope_id
      AND generation.source_generation_id=v_binding.source_generation_id
      AND generation.status='published'
      AND generation.producer_grant_epoch=v_grant.grant_epoch FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication source generation changed';
    END IF;
    SELECT * INTO v_observation FROM public.scanner_running_image_observations AS observation
    WHERE observation.observation_id=v_binding.canonical_observation_id
      AND observation.producer_instance_id=v_binding.source_collector_instance_id
      AND observation.source_scope_id=v_binding.source_scope_id
      AND observation.source_generation_id=v_binding.source_generation_id
      AND observation.registry=v_binding.registry
      AND observation.repository=v_binding.repository
      AND observation.runtime_digest=v_binding.runtime_digest
      AND observation.runtime_digest_kind=v_binding.runtime_digest_kind
      AND observation.runtime_image_reference=v_binding.runtime_image_reference
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication observation binding mismatch';
    END IF;
    SELECT * INTO v_activation FROM public.scanner_running_image_active_scopes AS active_source
    WHERE active_source.producer_instance_id=v_observation.producer_instance_id
      AND active_source.source_scope_id=v_observation.source_scope_id
      AND active_source.published_generation_id=v_observation.source_generation_id
      AND active_source.activation_epoch=v_binding.pinned_source_activation_epoch
      AND active_source.active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication source activation revoked';
    END IF;
    IF v_binding.runtime_digest_kind='manifest' THEN
        IF v_binding.runtime_digest<>v_target.manifest_digest THEN
            RAISE EXCEPTION 'impact publication manifest digest mismatch';
        END IF;
    ELSIF v_binding.runtime_digest_kind='config' THEN
        PERFORM 1 FROM public.scanner_running_image_manifest_verifications AS verified
        WHERE verified.verification_id=v_binding.manifest_verification_id
          AND verified.verification_epoch=v_binding.pinned_manifest_verification_epoch
          AND verified.observation_id=v_observation.observation_id
          AND verified.manifest_digest=v_target.manifest_digest
          AND verified.config_digest=v_binding.runtime_digest FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact publication manifest mapping unverified';
        END IF;
    ELSE
        RAISE EXCEPTION 'impact publication runtime digest kind unsupported';
    END IF;
    -- A revoked or rotated epoch joins to nothing, so the live set must
    -- match all five pins. Tenant, workspace, and scope-grant rows are
    -- rechecked active below: an epoch bump always accompanies a status
    -- change, and the loader admitted only active rows.
    IF (SELECT count(*) FROM (VALUES
            ('collector_instance',jsonb_build_array(v_binding.source_collector_instance_id)::text,
             v_binding.pinned_source_instance_epoch),
            ('collector_instance',jsonb_build_array(v_binding.scanner_collector_instance_id)::text,
             v_binding.pinned_registry_epoch),
            ('tenant',jsonb_build_array(v_target.tenant_id)::text,
             v_binding.pinned_tenant_epoch),
            ('workspace',jsonb_build_array(v_target.tenant_id,v_target.workspace_id)::text,
             v_binding.pinned_workspace_epoch),
            ('scope_grant',jsonb_build_array(v_target.tenant_id,v_target.workspace_id,
             v_binding.source_scope_id,v_target.subject_class)::text,
             v_binding.pinned_scope_grant_epoch)
        ) AS pinned(authority_kind,authority_key,pinned_epoch)
        JOIN public.scanner_authority_epochs AS epoch
          ON epoch.authority_kind=pinned.authority_kind
         AND epoch.authority_key=pinned.authority_key
         AND epoch.authority_epoch=pinned.pinned_epoch
         AND epoch.active) <> 5 THEN
        RAISE EXCEPTION 'impact publication authority epoch changed';
    END IF;
    PERFORM 1 FROM public.collector_instances AS source_instance
    WHERE source_instance.instance_id=v_binding.source_collector_instance_id
      AND source_instance.enabled AND source_instance.claims_enabled FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication source instance disabled';
    END IF;
    PERFORM 1 FROM public.tenants AS tenant
    WHERE tenant.tenant_id=v_target.tenant_id AND tenant.status='active'
      AND tenant.tombstoned_at IS NULL
      AND tenant.policy_revision_hash=v_binding.policy_revision_hash FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication tenant inactive';
    END IF;
    PERFORM 1 FROM public.workspaces AS workspace
    WHERE workspace.tenant_id=v_target.tenant_id
      AND workspace.workspace_id=v_target.workspace_id AND workspace.status='active'
      AND workspace.tombstoned_at IS NULL
      AND workspace.policy_revision_hash=v_binding.policy_revision_hash FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication workspace inactive';
    END IF;
    PERFORM 1 FROM public.tenant_scope_grants AS source_grant
    WHERE source_grant.tenant_id=v_target.tenant_id
      AND source_grant.workspace_id=v_target.workspace_id
      AND source_grant.scope_id=v_binding.source_scope_id
      AND source_grant.subject_class=v_target.subject_class
      AND source_grant.policy_revision_hash=v_binding.policy_revision_hash
      AND source_grant.tombstoned_at IS NULL
      AND source_grant.effective_at<=clock_timestamp()
      AND (source_grant.expires_at IS NULL
           OR source_grant.expires_at>clock_timestamp()) FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication scope grant revoked';
    END IF;
    PERFORM 1 FROM public.collector_instances AS scanner
    WHERE scanner.instance_id=v_binding.scanner_collector_instance_id
      AND scanner.collector_kind='scanner_worker'
      AND scanner.enabled AND scanner.claims_enabled
      AND scanner.configuration->>'derive_from_running_images'='true'
      AND 1=(SELECT count(*) FROM jsonb_array_elements(COALESCE(
               scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
             WHERE domain.value->>'authorization_domain'=v_target.authorization_domain)
      AND EXISTS (
        SELECT 1 FROM jsonb_array_elements(COALESCE(
                 scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
        WHERE domain.value->>'authorization_domain'=v_target.authorization_domain
          AND domain.value->>'registry'=v_binding.registry
          AND encode(pg_catalog.sha256(pg_catalog.convert_to(
                  domain.value::text,'UTF8')),'hex')=v_binding.registry_domain_config_sha256)
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication scanner authority changed';
    END IF;
    IF v_binding.pinned_role_policy_revision<>p_role_revision THEN
        RAISE EXCEPTION 'impact publication role policy changed';
    END IF;
    SELECT * INTO v_policy FROM eshu_security.role_policy_state AS policy
    WHERE policy.singleton AND policy.active
      AND policy.policy_revision=p_role_revision
      AND policy.database_identity=p_database_identity
      AND policy.database_oid=(SELECT oid FROM pg_database WHERE datname=current_database())
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact publication role policy is not current';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM public.scanner_image_scan_fact_associations AS member
        JOIN public.fact_records AS fact ON fact.fact_id=member.fact_id
         AND fact.fact_kind='scanner_worker.analysis'
         AND fact.schema_version=p_target->>'analysis_version'
         AND NOT fact.is_tombstone
        WHERE member.result_receipt_id=v_result.result_receipt_id
          AND member.scan_scope_id=v_target.scan_scope_id
          AND member.scan_generation_id=v_result.scan_generation_id) THEN
        RAISE EXCEPTION 'impact publication analysis version changed';
    END IF;
    FOR v_dependency IN
        SELECT value FROM jsonb_array_elements(
            COALESCE(p_target->'authority_dependencies','[]'::jsonb)) AS dependency(value)
        ORDER BY dependency.value->>'kind',dependency.value->>'id'
    LOOP
        IF v_dependency->>'kind'='public_registration' THEN
            PERFORM eshu_security.authorize_impact_public_dependency_v1(
                v_dependency,p_role_revision,p_database_identity);
        ELSIF v_dependency->>'kind'='private_binding' THEN
            PERFORM 1 FROM public.scanner_advisory_private_bindings AS binding
            WHERE binding.advisory_binding_id=v_dependency->>'id'
              AND binding.scan_scope_id=v_target.scan_scope_id
              AND binding.advisory_scope_id=v_dependency->>'advisory_scope_id'
              AND binding.advisory_generation_id=v_dependency->>'advisory_generation_id'
              AND binding.authority_epoch=(v_dependency->>'authority_epoch')::BIGINT
              AND binding.pinned_advisory_scope_epoch=(v_dependency->>'advisory_scope_epoch')::BIGINT
              AND binding.trusted_source_class=v_dependency->>'trusted_source_class'
              AND binding.provider_id=v_dependency->>'provider_id'
              AND binding.content_profile=v_dependency->>'content_profile'
              AND binding.active FOR SHARE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'impact publication private advisory revoked';
            END IF;
        ELSE
            RAISE EXCEPTION 'impact publication advisory authority unknown';
        END IF;
    END LOOP;
    -- Every association must point at a finding this publication carries;
    -- otherwise an evidence fact could ride along unattributed.
    IF EXISTS (
        SELECT 1 FROM jsonb_array_elements(
            COALESCE(p_target->'evidence_associations','[]'::jsonb)) AS evidence(value)
        WHERE NOT EXISTS (
          SELECT 1 FROM jsonb_array_elements(
              COALESCE(p_target->'finding_payloads','[]'::jsonb)) AS finding(value)
          WHERE finding.value->>'fact_id'=evidence.value->>'finding_fact_id')) THEN
        RAISE EXCEPTION 'impact publication evidence without finding';
    END IF;
    FOR v_advisory IN
        SELECT DISTINCT evidence.value->>'evidence_fact_id' AS evidence_fact_id,
               evidence.value->>'authority_kind' AS authority_kind,
               evidence.value->>'authority_id' AS authority_id,
               (evidence.value->>'authority_epoch')::BIGINT AS authority_epoch
        FROM jsonb_array_elements(
            COALESCE(p_target->'evidence_associations','[]'::jsonb)) AS evidence(value)
        WHERE evidence.value->>'evidence_class'='advisory'
        ORDER BY 1
    LOOP
        PERFORM eshu_security.authorize_impact_advisory_evidence_v1(
            p_target,jsonb_build_object(
                'evidence_fact_id',v_advisory.evidence_fact_id,
                'authority_kind',v_advisory.authority_kind,
                'authority_id',v_advisory.authority_id,
                'authority_epoch',v_advisory.authority_epoch),
            p_role_revision,p_database_identity);
    END LOOP;
    -- Intrinsic evidence must still be receipt-associated under the live
    -- fact row: a witness cannot smuggle an unassociated fact into a
    -- target result.
    FOR v_evidence IN
        SELECT value FROM jsonb_array_elements(
            COALESCE(p_target->'intrinsic_evidence_fact_ids','[]'::jsonb)) AS intrinsic(value)
        ORDER BY intrinsic.value
    LOOP
        PERFORM 1 FROM public.scanner_image_scan_fact_associations AS member
        JOIN public.fact_records AS fact
          ON fact.fact_id=member.fact_id
         AND fact.fencing_token=member.fencing_token
         AND encode(pg_catalog.sha256(pg_catalog.convert_to(
                 fact.payload::text,'UTF8')),'hex')=member.payload_sha256
        WHERE member.result_receipt_id=v_result.result_receipt_id
          AND member.fact_id=(v_evidence.value #>> '{}')
          AND fact.scope_id=v_target.scan_scope_id
          AND fact.generation_id=v_result.scan_generation_id
          AND ((fact.fact_kind IN ('scanner_worker.analysis','scanner_worker.warning')
                AND fact.source_system='scanner_worker')
               OR (fact.fact_kind IN ('vulnerability.os_package','vulnerability.warning')
                   AND fact.source_system='scanner_worker'
                   AND fact.collector_kind='os_package_vulnerability'))
          AND NOT fact.is_tombstone
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact publication intrinsic evidence changed';
        END IF;
    END LOOP;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.authorize_impact_target_publication_v1(
    TEXT,TEXT,BIGINT,JSONB,BIGINT,UUID) FROM PUBLIC;

-- authorize_impact_public_dependency_v1 rechecks one public advisory
-- registration at the exact generation a derived publication cites. It is
-- the per-dependency half of authorize_static_impact_public_v1's
-- per-evidence loop, keyed by registration and generation instead of by
-- evidence fact.
CREATE OR REPLACE FUNCTION eshu_security.authorize_impact_public_dependency_v1(
    p_dependency JSONB,p_role_revision BIGINT,p_database_identity UUID)
RETURNS VOID LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_registration public.scanner_advisory_public_registrations%ROWTYPE;
    v_collector public.collector_instances%ROWTYPE;
    v_collector_epoch BIGINT;
    v_approval public.scanner_advisory_public_generations%ROWTYPE;
    v_activation public.scanner_advisory_public_activations%ROWTYPE;
    v_grant public.scanner_advisory_producer_grants%ROWTYPE;
BEGIN
    IF p_dependency->>'id' IS NULL OR p_dependency->>'advisory_scope_id' IS NULL
       OR p_dependency->>'advisory_generation_id' IS NULL
       OR (p_dependency->>'authority_epoch')::BIGINT<=0
       OR (p_dependency->>'advisory_scope_epoch')::BIGINT<=0
       OR p_dependency->>'content_profile' IS NULL
       OR p_dependency->>'provider_id' IS NULL
       OR p_dependency->>'trusted_source_class' IS NULL THEN
        RAISE EXCEPTION 'impact public advisory dependency invalid';
    END IF;
    SELECT * INTO v_registration
    FROM public.scanner_advisory_public_registrations AS registration
    WHERE registration.registration_id=p_dependency->>'id'
      AND registration.advisory_scope_id=p_dependency->>'advisory_scope_id'
      AND registration.active
      AND registration.authority_epoch=(p_dependency->>'authority_epoch')::BIGINT
      AND registration.content_profile=p_dependency->>'content_profile'
      AND registration.provider_id=p_dependency->>'provider_id'
      AND registration.trusted_source_class=p_dependency->>'trusted_source_class'
      AND registration.max_age_seconds BETWEEN 1 AND 2592000
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact public advisory registration revoked';
    END IF;
    SELECT * INTO v_collector FROM public.collector_instances AS instance
    WHERE instance.instance_id=v_registration.collector_instance_id
      AND instance.collector_kind='vulnerability_intelligence'
      AND instance.enabled AND instance.claims_enabled FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact public advisory collector inactive';
    END IF;
    SELECT epoch.authority_epoch INTO v_collector_epoch
    FROM public.scanner_authority_epochs AS epoch
    WHERE epoch.authority_kind='collector_instance'
      AND epoch.authority_key=jsonb_build_array(v_registration.collector_instance_id)::text
      AND epoch.active FOR SHARE;
    IF v_collector_epoch IS NULL
       OR v_collector_epoch<>v_registration.pinned_collector_instance_epoch
       OR v_registration.configuration_sha256<>encode(pg_catalog.sha256(
           pg_catalog.convert_to(v_collector.configuration::text,'UTF8')),'hex') THEN
        RAISE EXCEPTION 'impact public advisory collector changed';
    END IF;
    SELECT * INTO v_approval
    FROM public.scanner_advisory_public_generations AS approved
    WHERE approved.registration_id=v_registration.registration_id
      AND approved.advisory_generation_id=p_dependency->>'advisory_generation_id'
      AND approved.active
      AND approved.approved_authority_epoch=v_registration.authority_epoch
      AND approved.approved_configuration_sha256=v_registration.configuration_sha256
      AND approved.approved_max_age_seconds=v_registration.max_age_seconds
      AND approved.expires_at>clock_timestamp()
      AND approved.pinned_role_policy_revision=p_role_revision
      AND approved.pinned_database_identity=p_database_identity
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact public advisory approval expired';
    END IF;
    SELECT * INTO v_activation
    FROM public.scanner_advisory_public_activations AS activation
    WHERE activation.collector_instance_id=v_registration.collector_instance_id
      AND activation.advisory_scope_id=v_registration.advisory_scope_id
      AND activation.active_generation_id=v_approval.advisory_generation_id
      AND activation.activation_epoch=(p_dependency->>'advisory_scope_epoch')::BIGINT
      AND activation.active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact public advisory activation revoked';
    END IF;
    SELECT * INTO v_grant
    FROM public.scanner_advisory_producer_grants AS allowed
    WHERE allowed.principal_oid=v_approval.producer_principal_oid
      AND allowed.collector_instance_id=v_registration.collector_instance_id
      AND allowed.advisory_scope_id=v_registration.advisory_scope_id
      AND allowed.active
      AND allowed.grant_epoch=v_approval.producer_grant_epoch
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact public advisory producer grant revoked';
    END IF;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.authorize_impact_public_dependency_v1(
    JSONB,BIGINT,UUID) FROM PUBLIC;

-- authorize_impact_advisory_evidence_v1 binds one derived advisory fact to
-- the exact approved generation member the publication's dependency cites,
-- then rechecks the live fact row. Private bindings carry no generation
-- receipts, so a private fact is checked for live membership, scope, kind,
-- and payload continuity only; the loader never emits private advisory
-- evidence (it fail-closes the target instead), keeping this the dormant
-- half of the check.
CREATE OR REPLACE FUNCTION eshu_security.authorize_impact_advisory_evidence_v1(
    p_target JSONB,p_evidence JSONB,p_role_revision BIGINT,p_database_identity UUID)
RETURNS VOID LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_dependency JSONB;
    v_approval public.scanner_advisory_public_generations%ROWTYPE;
    v_registration public.scanner_advisory_public_registrations%ROWTYPE;
    v_member public.scanner_advisory_generation_facts%ROWTYPE;
BEGIN
    IF p_evidence->>'evidence_fact_id' IS NULL
       OR p_evidence->>'authority_kind' IS NULL
       OR p_evidence->>'authority_id' IS NULL
       OR (p_evidence->>'authority_epoch')::BIGINT<=0 THEN
        RAISE EXCEPTION 'impact advisory evidence invalid';
    END IF;
    SELECT dependency.value INTO v_dependency
    FROM jsonb_array_elements(
        COALESCE(p_target->'authority_dependencies','[]'::jsonb)) AS dependency(value)
    WHERE dependency.value->>'kind'=p_evidence->>'authority_kind'
      AND dependency.value->>'id'=p_evidence->>'authority_id'
      AND (dependency.value->>'authority_epoch')::BIGINT=(p_evidence->>'authority_epoch')::BIGINT;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'impact advisory evidence has no publication authority';
    END IF;
    IF p_evidence->>'authority_kind'='public_registration' THEN
        SELECT * INTO v_approval
        FROM public.scanner_advisory_public_generations AS approved
        WHERE approved.registration_id=v_dependency->>'id'
          AND approved.advisory_generation_id=v_dependency->>'advisory_generation_id'
          AND approved.active FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact advisory evidence generation retired';
        END IF;
        SELECT * INTO v_registration
        FROM public.scanner_advisory_public_registrations AS registration
        WHERE registration.registration_id=v_approval.registration_id
          AND registration.active FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact advisory evidence registration revoked';
        END IF;
        SELECT * INTO v_member
        FROM public.scanner_advisory_generation_facts AS member
        WHERE member.receipt_id=v_approval.generation_receipt_id
          AND member.fact_id=p_evidence->>'evidence_fact_id'
          AND v_registration.allowed_fact_kinds ? member.fact_kind
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact advisory evidence fact unapproved';
        END IF;
        PERFORM 1 FROM public.fact_records AS fact
        WHERE fact.fact_id=v_member.fact_id
          AND fact.scope_id=v_registration.advisory_scope_id
          AND fact.generation_id=v_approval.advisory_generation_id
          AND fact.fact_kind=v_member.fact_kind
          AND fact.source_system='vulnerability_intelligence'
          AND fact.collector_kind='vulnerability_intelligence'
          AND NOT fact.is_tombstone
          AND encode(pg_catalog.sha256(pg_catalog.convert_to(
                  fact.payload::text,'UTF8')),'hex')=v_member.payload_sha256
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact advisory evidence fact changed';
        END IF;
    ELSIF p_evidence->>'authority_kind'='private_binding' THEN
        PERFORM 1 FROM public.fact_records AS fact
        JOIN public.scanner_advisory_private_bindings AS binding
          ON binding.advisory_binding_id=v_dependency->>'id'
        WHERE fact.fact_id=p_evidence->>'evidence_fact_id'
          AND fact.scope_id=binding.advisory_scope_id
          AND fact.generation_id=binding.advisory_generation_id
          AND binding.allowed_fact_kinds ? fact.fact_kind
          AND NOT fact.is_tombstone
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact private advisory evidence changed';
        END IF;
    ELSE
        RAISE EXCEPTION 'impact advisory evidence authority unknown';
    END IF;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.authorize_impact_advisory_evidence_v1(
    JSONB,JSONB,BIGINT,UUID) FROM PUBLIC;
