-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- A producer principal is granted exact collector/scope pairs by the schema
-- owner. A generic runtime writer cannot assert producer identity in payload.
CREATE TABLE IF NOT EXISTS scanner_advisory_producer_grants (
    principal_oid OID NOT NULL,
    collector_instance_id TEXT NOT NULL,
    advisory_scope_id TEXT NOT NULL,
    grant_epoch BIGINT NOT NULL DEFAULT nextval('scanner_authority_epoch_seq'),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (principal_oid,collector_instance_id,advisory_scope_id)
);
CREATE TABLE IF NOT EXISTS scanner_advisory_generation_receipts (
    receipt_id TEXT PRIMARY KEY,
    registration_id TEXT NOT NULL
      REFERENCES scanner_advisory_public_registrations(registration_id),
    collector_instance_id TEXT NOT NULL,
    advisory_scope_id TEXT NOT NULL,
    advisory_generation_id TEXT NOT NULL,
    work_item_id TEXT NOT NULL REFERENCES workflow_work_items(work_item_id),
    claim_id TEXT NOT NULL,
    claim_fencing_token BIGINT NOT NULL CHECK (claim_fencing_token > 0),
    claim_owner_id TEXT NOT NULL,
    producer_principal_oid OID NOT NULL,
    pinned_producer_grant_epoch BIGINT NOT NULL,
    pinned_registration_epoch BIGINT NOT NULL,
    pinned_collector_instance_epoch BIGINT NOT NULL,
    pinned_configuration_sha256 TEXT NOT NULL,
    pinned_desired_revision BIGINT NOT NULL,
    pinned_max_age_seconds INTEGER NOT NULL CHECK
      (pinned_max_age_seconds BETWEEN 1 AND 2592000),
    pinned_role_policy_revision BIGINT NOT NULL,
    pinned_database_identity UUID NOT NULL,
    fact_set_sha256 TEXT,
    fact_count INTEGER,
    status TEXT NOT NULL CHECK (status IN ('staging','complete','published')),
    staged_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    UNIQUE (collector_instance_id,advisory_scope_id,advisory_generation_id),
    CHECK ((status='staging' AND fact_set_sha256 IS NULL AND fact_count IS NULL)
       OR (status IN ('complete','published') AND fact_set_sha256 IS NOT NULL
           AND fact_count IS NOT NULL AND fact_count>=0))
);
CREATE TABLE IF NOT EXISTS scanner_advisory_generation_facts (
    receipt_id TEXT NOT NULL
      REFERENCES scanner_advisory_generation_receipts(receipt_id),
    fact_id TEXT NOT NULL,
    fact_kind TEXT NOT NULL,
    payload_sha256 TEXT NOT NULL,
    PRIMARY KEY (receipt_id,fact_id),
    UNIQUE (fact_id)
);
CREATE INDEX IF NOT EXISTS scanner_advisory_generation_facts_kind_idx
    ON scanner_advisory_generation_facts(receipt_id,fact_kind,fact_id);
CREATE TABLE IF NOT EXISTS scanner_advisory_public_activations (
    collector_instance_id TEXT NOT NULL,
    advisory_scope_id TEXT NOT NULL,
    active_generation_id TEXT NOT NULL,
    activation_epoch BIGINT NOT NULL,
    active BOOLEAN NOT NULL,
    PRIMARY KEY (collector_instance_id,advisory_scope_id)
);
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS generation_receipt_id TEXT
      REFERENCES scanner_advisory_generation_receipts(receipt_id);
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS producer_principal_oid OID;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS producer_grant_epoch BIGINT;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS pinned_collector_instance_epoch BIGINT;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS protected_activation_epoch BIGINT;
ALTER TABLE scanner_advisory_public_generations
    ADD COLUMN IF NOT EXISTS fact_set_sha256 TEXT;
REVOKE ALL ON scanner_advisory_producer_grants,
    scanner_advisory_generation_receipts,scanner_advisory_generation_facts,
    scanner_advisory_public_activations FROM PUBLIC;

-- Called before claim heartbeat and before any fact upsert. The claimed work
-- is checked after the policy, desired state, collector, registration and
-- producer grant locks. This fixes one global lock order for stage/reconcile.
CREATE OR REPLACE FUNCTION eshu_security.stage_public_advisory_generation_v1(
    p_scope TEXT,p_generation TEXT,p_work_item TEXT,p_claim TEXT,
    p_fence BIGINT,p_owner TEXT,p_role_revision BIGINT,
    p_database_identity UUID)
RETURNS TEXT LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    v_instance_id TEXT;
    v_policy public.scanner_advisory_desired_policy%ROWTYPE;
    v_instance public.collector_instances%ROWTYPE;
    v_registration public.scanner_advisory_public_registrations%ROWTYPE;
    v_grant public.scanner_advisory_producer_grants%ROWTYPE;
    v_collector_epoch BIGINT;
    v_config_hash TEXT;
    v_receipt_id TEXT;
    v_prior public.scanner_advisory_generation_receipts%ROWTYPE;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_role_revision,p_database_identity,'typed-source-producer');
    IF p_scope IS NULL OR p_scope='' OR p_generation IS NULL
       OR p_generation='' OR p_work_item IS NULL OR p_work_item=''
       OR p_claim IS NULL OR p_claim='' OR p_owner IS NULL OR p_owner=''
       OR p_fence<=0 THEN
        RAISE EXCEPTION 'public advisory stage input invalid';
    END IF;
    SELECT * INTO v_policy FROM public.scanner_advisory_desired_policy
    WHERE singleton FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'public advisory desired policy absent';
    END IF;
    -- The first read is a locator only; the work-item identity is rechecked
    -- under its row lock after all source authority locks are held.
    SELECT item.collector_instance_id INTO v_instance_id
    FROM public.workflow_work_items AS item
    WHERE item.work_item_id=p_work_item;
    IF v_instance_id IS NULL THEN
        RAISE EXCEPTION 'public advisory work item absent';
    END IF;
    SELECT * INTO v_instance FROM public.collector_instances AS instance
    WHERE instance.instance_id=v_instance_id
      AND instance.collector_kind='vulnerability_intelligence'
      AND instance.enabled AND instance.claims_enabled FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'public advisory producer instance inactive';
    END IF;
    SELECT epoch.authority_epoch INTO v_collector_epoch
    FROM public.scanner_authority_epochs AS epoch
    WHERE epoch.authority_kind='collector_instance'
      AND epoch.authority_key=jsonb_build_array(v_instance_id)::text
      AND epoch.active FOR SHARE;
    v_config_hash := encode(pg_catalog.sha256(
        pg_catalog.convert_to(v_instance.configuration::text,'UTF8')),'hex');
    SELECT * INTO v_registration
    FROM public.scanner_advisory_public_registrations AS registration
    WHERE registration.collector_instance_id=v_instance_id
      AND registration.advisory_scope_id=p_scope
      AND registration.active
      AND registration.configuration_sha256=v_config_hash
      AND registration.pinned_collector_instance_epoch=v_collector_epoch
      AND registration.content_profile='supply_chain_impact_v1'
      AND registration.max_age_seconds BETWEEN 1 AND 2592000
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'public advisory registration not current';
    END IF;
    SELECT * INTO v_grant FROM public.scanner_advisory_producer_grants AS allowed
    WHERE allowed.principal_oid=v_principal_oid
      AND allowed.collector_instance_id=v_instance_id
      AND allowed.advisory_scope_id=p_scope AND allowed.active FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'public advisory producer grant absent';
    END IF;
    PERFORM 1 FROM public.workflow_work_items AS item
    WHERE item.work_item_id=p_work_item
      AND item.collector_instance_id=v_instance_id
      AND item.collector_kind='vulnerability_intelligence'
      AND item.scope_id=p_scope
      AND item.status='claimed'
      AND item.current_claim_id=p_claim
      AND item.current_fencing_token=p_fence
      AND item.current_owner_id=p_owner
      AND item.lease_expires_at>clock_timestamp()
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'public advisory claim stale';
    END IF;
    v_receipt_id := 'public-advisory-generation:'||encode(pg_catalog.sha256(
        pg_catalog.convert_to(jsonb_build_array(v_instance_id,p_scope,
            p_generation,p_work_item,p_claim,p_fence)::text,'UTF8')),'hex');
    SELECT * INTO v_prior FROM public.scanner_advisory_generation_receipts
    WHERE collector_instance_id=v_instance_id
      AND advisory_scope_id=p_scope
      AND advisory_generation_id=p_generation FOR UPDATE;
    IF FOUND THEN
        IF v_prior.receipt_id<>v_receipt_id
           OR v_prior.claim_owner_id<>p_owner
           OR v_prior.producer_principal_oid<>v_principal_oid
           OR v_prior.pinned_registration_epoch<>v_registration.authority_epoch
           OR v_prior.pinned_producer_grant_epoch<>v_grant.grant_epoch
           OR v_prior.pinned_collector_instance_epoch<>v_collector_epoch
           OR v_prior.pinned_configuration_sha256<>v_config_hash
           OR v_prior.pinned_desired_revision<>v_policy.desired_revision
           OR v_prior.pinned_max_age_seconds<>v_registration.max_age_seconds
           OR v_prior.pinned_role_policy_revision<>p_role_revision
           OR v_prior.pinned_database_identity<>p_database_identity THEN
            RAISE EXCEPTION 'public advisory generation identity conflict';
        END IF;
        RETURN v_receipt_id;
    END IF;
    IF EXISTS (SELECT 1 FROM public.fact_records AS fact
               WHERE fact.scope_id=p_scope AND fact.generation_id=p_generation)
       OR EXISTS (SELECT 1 FROM public.scope_generations AS generation
                  WHERE generation.generation_id=p_generation) THEN
        RAISE EXCEPTION 'public advisory generation was not staged before facts';
    END IF;
    INSERT INTO public.scanner_advisory_generation_receipts
        (receipt_id,registration_id,collector_instance_id,advisory_scope_id,
         advisory_generation_id,work_item_id,claim_id,claim_fencing_token,
         claim_owner_id,producer_principal_oid,pinned_producer_grant_epoch,
         pinned_registration_epoch,pinned_collector_instance_epoch,
         pinned_configuration_sha256,pinned_desired_revision,
         pinned_max_age_seconds,pinned_role_policy_revision,
         pinned_database_identity,status)
    VALUES (v_receipt_id,v_registration.registration_id,v_instance_id,p_scope,
            p_generation,p_work_item,p_claim,p_fence,p_owner,v_principal_oid,
            v_grant.grant_epoch,v_registration.authority_epoch,
            v_collector_epoch,v_config_hash,v_policy.desired_revision,
            v_registration.max_age_seconds,p_role_revision,
            p_database_identity,'staging');
    RETURN v_receipt_id;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.stage_public_advisory_generation_v1(
    TEXT,TEXT,TEXT,TEXT,BIGINT,TEXT,BIGINT,UUID) FROM PUBLIC;
