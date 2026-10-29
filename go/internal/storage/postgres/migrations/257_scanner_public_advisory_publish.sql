-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Publication is a separate short transaction after the fact/receipt batch
-- commits. The protected activation pointer, not ingestion_scopes, determines
-- which approved public generation is current.
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS approved_observed_at TIMESTAMPTZ;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS approved_max_age_seconds INTEGER;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS pinned_role_policy_revision BIGINT;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS pinned_database_identity UUID;

CREATE OR REPLACE FUNCTION eshu_security.publish_public_advisory_generation_v1(
    p_scope TEXT,p_generation TEXT,p_work_item TEXT,p_claim TEXT,
    p_fence BIGINT,p_owner TEXT,p_role_revision BIGINT,
    p_database_identity UUID)
RETURNS BOOLEAN LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_receipt_id TEXT;
    v_receipt public.scanner_advisory_generation_receipts%ROWTYPE;
    v_activation public.scanner_advisory_public_activations%ROWTYPE;
    v_registration public.scanner_advisory_public_registrations%ROWTYPE;
    v_fact_count INTEGER;
    v_fact_hash TEXT;
    v_new_epoch BIGINT;
    v_prior public.scanner_advisory_public_generations%ROWTYPE;
    v_observed_at TIMESTAMPTZ;
    v_expires_at TIMESTAMPTZ;
BEGIN
    -- This lookup is only a locator. The helper locks and rechecks all
    -- source authority and the exact claim before this function mutates.
    SELECT receipt_id INTO v_receipt_id
    FROM public.scanner_advisory_generation_receipts AS receipt
    WHERE receipt.advisory_scope_id=p_scope
      AND receipt.advisory_generation_id=p_generation
      AND receipt.work_item_id=p_work_item
      AND receipt.claim_id=p_claim
      AND receipt.claim_fencing_token=p_fence
      AND receipt.claim_owner_id=p_owner;
    IF NOT FOUND THEN RETURN FALSE; END IF;
    v_receipt := eshu_security.lock_public_advisory_receipt_v1(
        v_receipt_id,p_role_revision,p_database_identity);
    IF v_receipt.status NOT IN ('complete','published') THEN RETURN FALSE; END IF;
    SELECT * INTO v_registration
    FROM public.scanner_advisory_public_registrations AS registration
    WHERE registration.registration_id=v_receipt.registration_id;
    SELECT count(*),encode(pg_catalog.sha256(pg_catalog.convert_to(
       COALESCE(string_agg(member.fact_id||':'||member.fact_kind||':'||
             member.payload_sha256,',' ORDER BY member.fact_id),''),'UTF8')),'hex')
    INTO v_fact_count,v_fact_hash
    FROM public.scanner_advisory_generation_facts AS member
    JOIN public.fact_records AS fact
      ON fact.fact_id=member.fact_id
     AND fact.scope_id=v_receipt.advisory_scope_id
     AND fact.generation_id=v_receipt.advisory_generation_id
     AND fact.fact_kind=member.fact_kind
     AND fact.source_system='vulnerability_intelligence'
     AND fact.collector_kind='vulnerability_intelligence'
     AND fact.fencing_token=v_receipt.claim_fencing_token
     AND NOT fact.is_tombstone
     AND encode(pg_catalog.sha256(pg_catalog.convert_to(
           fact.payload::text,'UTF8')),'hex')=member.payload_sha256
    WHERE member.receipt_id=v_receipt.receipt_id;
    IF v_fact_count<>v_receipt.fact_count
       OR v_fact_hash<>v_receipt.fact_set_sha256 THEN
        RAISE EXCEPTION 'public advisory staged fact set changed';
    END IF;
    SELECT generation.observed_at INTO v_observed_at
    FROM public.scope_generations AS generation
    WHERE generation.generation_id=p_generation
      AND generation.scope_id=p_scope;
    IF NOT FOUND OR v_observed_at>clock_timestamp()+INTERVAL '5 minutes' THEN
        RAISE EXCEPTION 'public advisory observed time invalid';
    END IF;
    v_expires_at := v_observed_at
        + make_interval(secs=>v_receipt.pinned_max_age_seconds);
    IF v_expires_at<=clock_timestamp() THEN
        RAISE EXCEPTION 'public advisory source snapshot expired';
    END IF;
    INSERT INTO public.scanner_advisory_public_activations
        (collector_instance_id,advisory_scope_id,active_generation_id,
         activation_epoch,active)
    VALUES (v_receipt.collector_instance_id,p_scope,'',
            nextval('public.scanner_authority_epoch_seq'),FALSE)
    ON CONFLICT ON CONSTRAINT scanner_advisory_public_activations_pkey
    DO NOTHING;
    SELECT * INTO v_activation
    FROM public.scanner_advisory_public_activations AS activation
    WHERE activation.collector_instance_id=v_receipt.collector_instance_id
      AND activation.advisory_scope_id=p_scope FOR UPDATE;
    IF v_receipt.status='published' THEN
        RETURN v_activation.active
           AND v_activation.active_generation_id=p_generation
           AND EXISTS (
             SELECT 1 FROM public.scanner_advisory_public_generations AS approved
             WHERE approved.registration_id=v_registration.registration_id
               AND approved.advisory_generation_id=p_generation
               AND approved.generation_receipt_id=v_receipt.receipt_id
               AND approved.active
               AND approved.protected_activation_epoch=v_activation.activation_epoch
               AND approved.fact_set_sha256=v_receipt.fact_set_sha256
               AND approved.approved_max_age_seconds=v_receipt.pinned_max_age_seconds
               AND approved.pinned_role_policy_revision=p_role_revision
               AND approved.pinned_database_identity=p_database_identity
               AND approved.expires_at>clock_timestamp());
    END IF;
    SELECT * INTO v_prior
    FROM public.scanner_advisory_public_generations AS approved
    WHERE approved.registration_id=v_registration.registration_id
      AND approved.advisory_generation_id=p_generation FOR UPDATE;
    IF FOUND THEN
        -- An old approved generation cannot be revived by a later claim or
        -- a new registration with the same config hash.
        RAISE EXCEPTION 'public advisory generation already approved';
    END IF;
    v_new_epoch := nextval('public.scanner_authority_epoch_seq');
    UPDATE public.scanner_advisory_public_generations AS approved
    SET active=FALSE
    WHERE approved.registration_id=v_registration.registration_id
      AND approved.active;
    INSERT INTO public.scanner_advisory_public_generations
        (registration_id,advisory_generation_id,
         approved_configuration_sha256,approved_authority_epoch,
         pinned_advisory_scope_epoch,active,generation_receipt_id,
         producer_principal_oid,producer_grant_epoch,
         pinned_collector_instance_epoch,protected_activation_epoch,
         fact_set_sha256,approved_observed_at,expires_at,
         approved_max_age_seconds,pinned_role_policy_revision,
         pinned_database_identity)
    VALUES (v_registration.registration_id,p_generation,
            v_receipt.pinned_configuration_sha256,
            v_receipt.pinned_registration_epoch,v_new_epoch,TRUE,
            v_receipt.receipt_id,v_receipt.producer_principal_oid,
            v_receipt.pinned_producer_grant_epoch,
            v_receipt.pinned_collector_instance_epoch,v_new_epoch,
            v_receipt.fact_set_sha256,v_observed_at,v_expires_at,
            v_receipt.pinned_max_age_seconds,p_role_revision,
            p_database_identity);
    UPDATE public.scanner_advisory_public_activations SET
        active_generation_id=p_generation,activation_epoch=v_new_epoch,
        active=TRUE
    WHERE collector_instance_id=v_receipt.collector_instance_id
      AND advisory_scope_id=p_scope;
    UPDATE public.scanner_advisory_generation_receipts SET
        status='published',published_at=NOW()
    WHERE receipt_id=v_receipt.receipt_id;
    RETURN TRUE;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.publish_public_advisory_generation_v1(
    TEXT,TEXT,TEXT,TEXT,BIGINT,TEXT,BIGINT,UUID) FROM PUBLIC;
