-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Every protected write uses the same policy -> desired state -> collector ->
-- registration -> producer grant -> work claim -> receipt lock order.
CREATE OR REPLACE FUNCTION eshu_security.lock_public_advisory_receipt_v1(
    p_receipt_id TEXT,p_role_revision BIGINT,p_database_identity UUID)
RETURNS public.scanner_advisory_generation_receipts
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    v_hint public.scanner_advisory_generation_receipts%ROWTYPE;
    v_receipt public.scanner_advisory_generation_receipts%ROWTYPE;
    v_instance public.collector_instances%ROWTYPE;
    v_registration public.scanner_advisory_public_registrations%ROWTYPE;
    v_grant public.scanner_advisory_producer_grants%ROWTYPE;
    v_instance_epoch BIGINT;
    v_config_hash TEXT;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_role_revision,p_database_identity,'typed-source-producer');
    SELECT * INTO v_hint FROM public.scanner_advisory_generation_receipts
    WHERE receipt_id=p_receipt_id;
    IF NOT FOUND OR v_hint.producer_principal_oid<>v_principal_oid
       OR v_hint.pinned_role_policy_revision<>p_role_revision
       OR v_hint.pinned_database_identity<>p_database_identity THEN
        RAISE EXCEPTION 'public advisory receipt unavailable';
    END IF;
    PERFORM 1 FROM public.scanner_advisory_desired_policy AS desired
    WHERE desired.singleton
      AND desired.desired_revision=v_hint.pinned_desired_revision FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'public advisory desired policy changed'; END IF;
    SELECT * INTO v_instance FROM public.collector_instances AS instance
    WHERE instance.instance_id=v_hint.collector_instance_id
      AND instance.collector_kind='vulnerability_intelligence'
      AND instance.enabled AND instance.claims_enabled FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'public advisory collector inactive'; END IF;
    SELECT epoch.authority_epoch INTO v_instance_epoch
    FROM public.scanner_authority_epochs AS epoch
    WHERE epoch.authority_kind='collector_instance'
      AND epoch.authority_key=jsonb_build_array(v_hint.collector_instance_id)::text
      AND epoch.active FOR SHARE;
    v_config_hash := encode(pg_catalog.sha256(
        pg_catalog.convert_to(v_instance.configuration::text,'UTF8')),'hex');
    SELECT * INTO v_registration
    FROM public.scanner_advisory_public_registrations AS registration
    WHERE registration.registration_id=v_hint.registration_id
      AND registration.collector_instance_id=v_hint.collector_instance_id
      AND registration.advisory_scope_id=v_hint.advisory_scope_id
      AND registration.active
      AND registration.configuration_sha256=v_config_hash
      AND registration.pinned_collector_instance_epoch=v_instance_epoch
      AND registration.content_profile='supply_chain_impact_v1'
      AND registration.max_age_seconds=v_hint.pinned_max_age_seconds
    FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'public advisory registration stale'; END IF;
    SELECT * INTO v_grant FROM public.scanner_advisory_producer_grants AS allowed
    WHERE allowed.principal_oid=v_principal_oid
      AND allowed.collector_instance_id=v_hint.collector_instance_id
      AND allowed.advisory_scope_id=v_hint.advisory_scope_id
      AND allowed.active FOR SHARE;
    IF NOT FOUND THEN RAISE EXCEPTION 'public advisory producer grant stale'; END IF;
    PERFORM 1 FROM public.workflow_work_items AS item
    WHERE item.work_item_id=v_hint.work_item_id
      AND item.collector_instance_id=v_hint.collector_instance_id
      AND item.collector_kind='vulnerability_intelligence'
      AND item.scope_id=v_hint.advisory_scope_id
      AND item.status='claimed'
      AND item.current_claim_id=v_hint.claim_id
      AND item.current_fencing_token=v_hint.claim_fencing_token
      AND item.current_owner_id=v_hint.claim_owner_id
      AND item.lease_expires_at>clock_timestamp() FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'public advisory receipt claim stale'; END IF;
    SELECT * INTO v_receipt FROM public.scanner_advisory_generation_receipts
    WHERE receipt_id=p_receipt_id FOR UPDATE;
    IF NOT FOUND OR v_receipt<>v_hint
       OR v_receipt.pinned_producer_grant_epoch<>v_grant.grant_epoch
       OR v_receipt.pinned_registration_epoch<>v_registration.authority_epoch
       OR v_receipt.pinned_collector_instance_epoch<>v_instance_epoch
       OR v_receipt.pinned_configuration_sha256<>v_config_hash THEN
        RAISE EXCEPTION 'public advisory receipt authority changed';
    END IF;
    RETURN v_receipt;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.lock_public_advisory_receipt_v1(
    TEXT,BIGINT,UUID) FROM PUBLIC;

-- The caller supplies only its bounded accepted batch. Each row is matched to
-- the already-written JSONB fact under the same transaction, then preserved
-- in owner-only membership. A generic fact writer cannot create membership.
CREATE OR REPLACE FUNCTION eshu_security.record_public_advisory_facts_v1(
    p_receipt_id TEXT,p_facts JSONB,p_role_revision BIGINT,
    p_database_identity UUID)
RETURNS INTEGER LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_receipt public.scanner_advisory_generation_receipts%ROWTYPE;
    v_fact JSONB;
    v_fact_id TEXT;
    v_fact_kind TEXT;
    v_payload_hash TEXT;
    v_seen JSONB := '{}'::jsonb;
    v_count INTEGER := 0;
BEGIN
    v_receipt := eshu_security.lock_public_advisory_receipt_v1(
        p_receipt_id,p_role_revision,p_database_identity);
    IF v_receipt.status<>'staging' OR p_facts IS NULL
       OR jsonb_typeof(p_facts)<>'array'
       OR jsonb_array_length(p_facts)>500
       OR octet_length(p_facts::text)>4194304 THEN
        RAISE EXCEPTION 'public advisory accepted batch invalid';
    END IF;
    FOR v_fact IN SELECT value FROM jsonb_array_elements(p_facts) AS batch(value)
    LOOP
        v_fact_id := v_fact->>'fact_id';
        v_fact_kind := v_fact->>'fact_kind';
        IF v_fact_id IS NULL OR v_fact_id='' OR length(v_fact_id)>512
           OR v_seen ? v_fact_id OR jsonb_typeof(v_fact->'payload')<>'object'
           OR v_fact_kind IS NULL OR v_fact_kind='' THEN
            RAISE EXCEPTION 'public advisory accepted fact invalid';
        END IF;
        v_seen := v_seen||jsonb_build_object(v_fact_id,TRUE);
        PERFORM 1 FROM public.fact_records AS fact
        WHERE fact.fact_id=v_fact_id
          AND fact.scope_id=v_receipt.advisory_scope_id
          AND fact.generation_id=v_receipt.advisory_generation_id
          AND fact.fact_kind=v_fact_kind
          AND fact.payload=v_fact->'payload'
          AND fact.source_system='vulnerability_intelligence'
          AND fact.collector_kind='vulnerability_intelligence'
          AND fact.fencing_token=v_receipt.claim_fencing_token
          AND NOT fact.is_tombstone FOR SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'public advisory accepted fact did not persist';
        END IF;
        v_payload_hash := encode(pg_catalog.sha256(
            pg_catalog.convert_to((v_fact->'payload')::text,'UTF8')),'hex');
        INSERT INTO public.scanner_advisory_generation_facts
            (receipt_id,fact_id,fact_kind,payload_sha256)
        VALUES (p_receipt_id,v_fact_id,v_fact_kind,v_payload_hash)
        ON CONFLICT (receipt_id,fact_id) DO NOTHING;
        PERFORM 1 FROM public.scanner_advisory_generation_facts AS member
        WHERE member.receipt_id=p_receipt_id AND member.fact_id=v_fact_id
          AND member.fact_kind=v_fact_kind
          AND member.payload_sha256=v_payload_hash;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'public advisory fact membership conflict';
        END IF;
        v_count := v_count+1;
    END LOOP;
    RETURN v_count;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.record_public_advisory_facts_v1(
    TEXT,JSONB,BIGINT,UUID) FROM PUBLIC;

-- Complete only an exact persisted member set with one source snapshot that
-- attests provider coverage. An empty advisory set remains possible because
-- the source snapshot itself is still a required member.
CREATE OR REPLACE FUNCTION eshu_security.complete_public_advisory_generation_v1(
    p_receipt_id TEXT,p_scope TEXT,p_generation TEXT,
    p_role_revision BIGINT,p_database_identity UUID)
RETURNS BOOLEAN LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_receipt public.scanner_advisory_generation_receipts%ROWTYPE;
    v_count INTEGER;
    v_fact_count INTEGER;
    v_snapshot_count INTEGER;
    v_hash TEXT;
    v_registration public.scanner_advisory_public_registrations%ROWTYPE;
BEGIN
    v_receipt := eshu_security.lock_public_advisory_receipt_v1(
        p_receipt_id,p_role_revision,p_database_identity);
    IF v_receipt.advisory_scope_id<>p_scope
       OR v_receipt.advisory_generation_id<>p_generation THEN
        RAISE EXCEPTION 'public advisory completion identity mismatch';
    END IF;
    IF v_receipt.status IN ('complete','published') THEN RETURN TRUE; END IF;
    IF v_receipt.status<>'staging' THEN
        RAISE EXCEPTION 'public advisory completion state invalid';
    END IF;
    SELECT * INTO v_registration
    FROM public.scanner_advisory_public_registrations
    WHERE registration_id=v_receipt.registration_id;
    SELECT count(*),encode(pg_catalog.sha256(pg_catalog.convert_to(
        COALESCE(string_agg(member.fact_id||':'||member.fact_kind||':'||
            member.payload_sha256,',' ORDER BY member.fact_id),''),'UTF8')),'hex')
    INTO v_count,v_hash
    FROM public.scanner_advisory_generation_facts AS member
    WHERE member.receipt_id=p_receipt_id;
    SELECT count(*) INTO v_fact_count FROM public.fact_records AS fact
    WHERE fact.scope_id=p_scope AND fact.generation_id=p_generation;
    SELECT count(*) INTO v_snapshot_count
    FROM public.scanner_advisory_generation_facts AS member
    JOIN public.fact_records AS fact ON fact.fact_id=member.fact_id
    WHERE member.receipt_id=p_receipt_id
      AND member.fact_kind='vulnerability.source_snapshot';
    IF v_snapshot_count<>1 THEN
        RAISE EXCEPTION 'public advisory source snapshot missing or ambiguous';
    END IF;
    SELECT count(*) INTO v_snapshot_count
    FROM public.scanner_advisory_generation_facts AS member
    JOIN public.fact_records AS fact ON fact.fact_id=member.fact_id
    WHERE member.receipt_id=p_receipt_id
      AND member.fact_kind='vulnerability.source_snapshot'
      AND fact.payload->>'source'=v_registration.trusted_source_class
      AND fact.payload->>'collector_instance_id'=v_receipt.collector_instance_id
      AND fact.payload->>'complete'='true'
      AND COALESCE(fact.payload->>'warning_code','')=''
      AND COALESCE(fact.payload->>'warning_message','')='';
    IF v_count=0 OR v_count<>v_fact_count OR v_snapshot_count<>1
       OR EXISTS (
          SELECT 1 FROM public.scanner_advisory_generation_facts AS member
          LEFT JOIN public.fact_records AS fact ON fact.fact_id=member.fact_id
          WHERE member.receipt_id=p_receipt_id
            AND (fact.fact_id IS NULL OR fact.scope_id<>p_scope
              OR fact.generation_id<>p_generation
              OR fact.fact_kind<>member.fact_kind
              OR fact.source_system<>'vulnerability_intelligence'
              OR fact.collector_kind<>'vulnerability_intelligence'
              OR fact.fencing_token<>v_receipt.claim_fencing_token
              OR fact.is_tombstone
              OR encode(pg_catalog.sha256(pg_catalog.convert_to(
                     fact.payload::text,'UTF8')),'hex')<>member.payload_sha256)) THEN
        RAISE EXCEPTION 'public advisory generation incomplete or altered';
    END IF;
    UPDATE public.scanner_advisory_generation_receipts SET
        fact_count=v_count,fact_set_sha256=v_hash,status='complete',
        completed_at=NOW() WHERE receipt_id=p_receipt_id;
    RETURN TRUE;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.complete_public_advisory_generation_v1(
    TEXT,TEXT,TEXT,BIGINT,UUID) FROM PUBLIC;
