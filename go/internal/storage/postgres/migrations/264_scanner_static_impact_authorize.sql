-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The reducer calls this before its impact-owner lock or finding upsert. The
-- role policy lock is first, followed by the desired policy and sorted public
-- advisory witnesses. The same transaction retains every SHARE lock through
-- publication, so a concurrent revoke cannot slip between load and write.
CREATE OR REPLACE FUNCTION eshu_security.authorize_static_impact_public_v1(
    p_impact_scope TEXT,p_impact_generation TEXT,p_impact_fence BIGINT,
    p_findings JSONB,p_role_revision BIGINT,p_database_identity UUID)
RETURNS VOID LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    v_evidence RECORD;
    v_instance_id TEXT;
    v_collector public.collector_instances%ROWTYPE;
    v_collector_epoch BIGINT;
    v_registration public.scanner_advisory_public_registrations%ROWTYPE;
    v_grant public.scanner_advisory_producer_grants%ROWTYPE;
    v_activation public.scanner_advisory_public_activations%ROWTYPE;
    v_receipt public.scanner_advisory_generation_receipts%ROWTYPE;
    v_approval public.scanner_advisory_public_generations%ROWTYPE;
    v_member public.scanner_advisory_generation_facts%ROWTYPE;
    v_fact public.fact_records%ROWTYPE;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_role_revision,p_database_identity,'runtime-writer');
    IF v_principal_oid IS NULL OR p_impact_scope IS NULL OR p_impact_scope=''
       OR p_impact_generation IS NULL OR p_impact_generation=''
       OR p_impact_fence<=0 OR p_findings IS NULL
       OR jsonb_typeof(p_findings)<>'array'
       OR jsonb_array_length(p_findings)>50000
       OR octet_length(p_findings::text)>33554432 THEN
        RAISE EXCEPTION 'static impact authority input invalid';
    END IF;
    IF EXISTS (
       SELECT 1 FROM jsonb_array_elements(p_findings) AS finding(value)
       WHERE finding.value->>'finding_fact_id' IS NULL
          OR finding.value->>'finding_fact_id'=''
          OR jsonb_typeof(finding.value->'advisory_evidence')<>'array'
          OR jsonb_typeof(finding.value->'payload_json')<>'string'
    ) OR (SELECT count(*) FROM jsonb_array_elements(p_findings)) <>
         (SELECT count(DISTINCT value->>'finding_fact_id')
          FROM jsonb_array_elements(p_findings)) THEN
        RAISE EXCEPTION 'static impact authority finding set invalid';
    END IF;
    PERFORM 1 FROM public.scanner_advisory_desired_policy
    WHERE singleton FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'static impact desired advisory policy absent';
    END IF;
    FOR v_evidence IN
        SELECT DISTINCT
            evidence.value->>'evidence_fact_id' AS evidence_fact_id,
            evidence.value->>'registration_id' AS registration_id,
            evidence.value->>'generation_receipt_id' AS receipt_id,
            evidence.value->>'registration_epoch' AS registration_epoch,
            evidence.value->>'activation_epoch' AS activation_epoch,
            evidence.value->>'evidence_payload_sha256' AS payload_sha256
        FROM jsonb_array_elements(p_findings) AS finding(value)
        CROSS JOIN LATERAL jsonb_array_elements(
            finding.value->'advisory_evidence') AS evidence(value)
        ORDER BY registration_id,evidence_fact_id,receipt_id
    LOOP
        IF v_evidence.evidence_fact_id IS NULL OR v_evidence.evidence_fact_id=''
           OR v_evidence.registration_id IS NULL OR v_evidence.registration_id=''
           OR v_evidence.receipt_id IS NULL OR v_evidence.receipt_id=''
           OR v_evidence.registration_epoch IS NULL
           OR v_evidence.activation_epoch IS NULL
           OR v_evidence.payload_sha256 !~ '^[0-9a-f]{64}$' THEN
            RAISE EXCEPTION 'static impact advisory evidence invalid';
        END IF;
        -- This is an unlocked locator only. All authority values are
        -- rechecked after the canonical collector/epoch/registration locks.
        SELECT registration.collector_instance_id INTO v_instance_id
        FROM public.scanner_advisory_public_registrations AS registration
        WHERE registration.registration_id=v_evidence.registration_id;
        IF v_instance_id IS NULL THEN
            RAISE EXCEPTION 'static impact advisory registration absent';
        END IF;
        SELECT * INTO v_collector FROM public.collector_instances AS instance
        WHERE instance.instance_id=v_instance_id
          AND instance.collector_kind='vulnerability_intelligence'
          AND instance.enabled AND instance.claims_enabled FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory collector inactive';
        END IF;
        SELECT epoch.authority_epoch INTO v_collector_epoch
        FROM public.scanner_authority_epochs AS epoch
        WHERE epoch.authority_kind='collector_instance'
          AND epoch.authority_key=jsonb_build_array(v_instance_id)::text
          AND epoch.active FOR SHARE;
        SELECT * INTO v_registration
        FROM public.scanner_advisory_public_registrations AS registration
        WHERE registration.registration_id=v_evidence.registration_id
          AND registration.collector_instance_id=v_instance_id
          AND registration.active
          AND registration.authority_epoch=v_evidence.registration_epoch::BIGINT
          AND registration.pinned_collector_instance_epoch=v_collector_epoch
          AND registration.configuration_sha256=encode(pg_catalog.sha256(
              pg_catalog.convert_to(v_collector.configuration::text,'UTF8')),'hex')
          AND registration.content_profile='supply_chain_impact_v1'
          AND registration.max_age_seconds BETWEEN 1 AND 2592000
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory registration revoked';
        END IF;
        -- Receipt is read only as a locator before its lock. The producer
        -- grant lock remains ordered before activation and receipt locks.
        SELECT * INTO v_receipt
        FROM public.scanner_advisory_generation_receipts AS receipt
        WHERE receipt.receipt_id=v_evidence.receipt_id;
        IF NOT FOUND OR v_receipt.registration_id<>v_registration.registration_id THEN
            RAISE EXCEPTION 'static impact advisory receipt absent';
        END IF;
        SELECT * INTO v_grant
        FROM public.scanner_advisory_producer_grants AS allowed
        WHERE allowed.principal_oid=v_receipt.producer_principal_oid
          AND allowed.collector_instance_id=v_instance_id
          AND allowed.advisory_scope_id=v_registration.advisory_scope_id
          AND allowed.active
          AND allowed.grant_epoch=v_receipt.pinned_producer_grant_epoch
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory producer grant revoked';
        END IF;
        SELECT * INTO v_activation
        FROM public.scanner_advisory_public_activations AS activation
        WHERE activation.collector_instance_id=v_instance_id
          AND activation.advisory_scope_id=v_registration.advisory_scope_id
          AND activation.active
          AND activation.activation_epoch=v_evidence.activation_epoch::BIGINT
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory activation revoked';
        END IF;
        SELECT * INTO v_approval
        FROM public.scanner_advisory_public_generations AS approved
        WHERE approved.registration_id=v_registration.registration_id
          AND approved.advisory_generation_id=v_activation.active_generation_id
          AND approved.generation_receipt_id=v_evidence.receipt_id
          AND approved.active
          AND approved.protected_activation_epoch=v_activation.activation_epoch
          AND approved.approved_authority_epoch=v_registration.authority_epoch
          AND approved.approved_configuration_sha256=v_registration.configuration_sha256
          AND approved.approved_max_age_seconds=v_registration.max_age_seconds
          AND approved.expires_at>clock_timestamp()
          AND approved.pinned_role_policy_revision=p_role_revision
          AND approved.pinned_database_identity=p_database_identity
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory approval expired';
        END IF;
        SELECT * INTO v_receipt
        FROM public.scanner_advisory_generation_receipts AS receipt
        WHERE receipt.receipt_id=v_evidence.receipt_id
          AND receipt.registration_id=v_registration.registration_id
          AND receipt.collector_instance_id=v_instance_id
          AND receipt.advisory_scope_id=v_registration.advisory_scope_id
          AND receipt.advisory_generation_id=v_activation.active_generation_id
          AND receipt.status='published'
          AND receipt.pinned_registration_epoch=v_registration.authority_epoch
          AND receipt.pinned_collector_instance_epoch=v_collector_epoch
          AND receipt.pinned_configuration_sha256=v_registration.configuration_sha256
          AND receipt.pinned_producer_grant_epoch=v_grant.grant_epoch
          AND receipt.pinned_role_policy_revision=p_role_revision
          AND receipt.pinned_database_identity=p_database_identity
          AND receipt.fact_set_sha256=v_approval.fact_set_sha256
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory receipt changed';
        END IF;
        SELECT * INTO v_member
        FROM public.scanner_advisory_generation_facts AS member
        WHERE member.receipt_id=v_receipt.receipt_id
          AND member.fact_id=v_evidence.evidence_fact_id
          AND member.payload_sha256=v_evidence.payload_sha256
          AND v_registration.allowed_fact_kinds ? member.fact_kind
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory fact unapproved';
        END IF;
        SELECT * INTO v_fact FROM public.fact_records AS fact
        WHERE fact.fact_id=v_member.fact_id
          AND fact.scope_id=v_registration.advisory_scope_id
          AND fact.generation_id=v_receipt.advisory_generation_id
          AND fact.fact_kind=v_member.fact_kind
          AND fact.source_system='vulnerability_intelligence'
          AND fact.collector_kind='vulnerability_intelligence'
          AND fact.fencing_token=v_receipt.claim_fencing_token
          AND NOT fact.is_tombstone
          AND encode(pg_catalog.sha256(pg_catalog.convert_to(
              fact.payload::text,'UTF8')),'hex')=v_member.payload_sha256
        FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'static impact advisory fact changed';
        END IF;
    END LOOP;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.authorize_static_impact_public_v1(
    TEXT,TEXT,BIGINT,JSONB,BIGINT,UUID) FROM PUBLIC;
