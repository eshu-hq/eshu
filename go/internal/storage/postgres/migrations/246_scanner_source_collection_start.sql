-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

CREATE UNIQUE INDEX IF NOT EXISTS scanner_running_image_generation_global_id_key
    ON scanner_running_image_generations (source_generation_id);

-- The accepted page's projection intent is committed with its receipt and
-- canonical observations. A worker may build generic fact projections later,
-- but that repair cannot create canonical source authority.
CREATE TABLE IF NOT EXISTS scanner_running_image_projection_outbox (
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    source_generation_id TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
      CHECK (status IN ('pending','claimed','complete')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (producer_instance_id,source_scope_id,source_generation_id,delivery_id),
    FOREIGN KEY (producer_instance_id,source_scope_id,source_generation_id,delivery_id)
      REFERENCES scanner_running_image_delivery_receipts
      (producer_instance_id,source_scope_id,source_generation_id,delivery_id)
);
REVOKE ALL ON scanner_running_image_projection_outbox FROM PUBLIC;

-- New collection claims fence old STAGING writers but do not retire the last
-- successfully published generation. This transaction contains no network I/O.
CREATE OR REPLACE FUNCTION eshu_security.start_running_image_collection_v1(
    producer_instance_id TEXT, source_scope_id TEXT,
    fresh_generation_id TEXT, producer_class TEXT,
    observation_class TEXT, observed_from TIMESTAMPTZ,
    observed_through TIMESTAMPTZ, consistency_class TEXT,
    lease_seconds INTEGER, expected_policy_revision BIGINT,
    expected_database_identity UUID)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    grant_row public.scanner_running_image_producer_grants%ROWTYPE;
    fence BIGINT;
    expected_collector_kind TEXT;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        expected_policy_revision,expected_database_identity,
        'typed-source-producer');
    IF producer_instance_id IS NULL OR producer_instance_id=''
       OR source_scope_id IS NULL OR source_scope_id=''
       OR fresh_generation_id IS NULL OR fresh_generation_id=''
       OR length(fresh_generation_id)>4096
       OR observed_from IS NULL OR observed_through IS NULL
       OR observed_through<observed_from
       OR consistency_class NOT IN ('observed_interval','provider_point')
       OR lease_seconds NOT BETWEEN 1 AND 3600
       OR NOT ((producer_class IN ('kubernetes_pod','ecs_task')
                AND observation_class='running_container')
            OR (producer_class='lambda_function'
                AND observation_class='active_deployment')) THEN
        RAISE EXCEPTION 'running-image collection claim invalid';
    END IF;
    SELECT * INTO grant_row
    FROM public.scanner_running_image_producer_grants AS allowed
    WHERE allowed.principal_oid=v_principal_oid
      AND allowed.producer_instance_id=start_running_image_collection_v1.producer_instance_id
      AND allowed.source_scope_id=start_running_image_collection_v1.source_scope_id
      AND allowed.producer_class=start_running_image_collection_v1.producer_class
      AND allowed.observation_class=start_running_image_collection_v1.observation_class
      AND allowed.contract_version=1 AND allowed.active
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'running-image producer grant absent';
    END IF;
    expected_collector_kind := CASE WHEN producer_class='kubernetes_pod'
        THEN 'kubernetes_live' ELSE 'aws' END;
    PERFORM 1 FROM public.collector_instances AS instance
    WHERE instance.instance_id=producer_instance_id
      AND instance.collector_kind=expected_collector_kind
      AND instance.enabled AND instance.claims_enabled
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'running-image collector instance inactive';
    END IF;
    fence := nextval('public.scanner_authority_epoch_seq');
    INSERT INTO public.scanner_running_image_scope_claims
      (producer_instance_id,source_scope_id,source_generation_id,
       principal_oid,collection_fence,lease_expires_at)
    VALUES (producer_instance_id,source_scope_id,fresh_generation_id,
            v_principal_oid,fence,
            clock_timestamp()+make_interval(secs=>lease_seconds))
    ON CONFLICT ON CONSTRAINT scanner_running_image_scope_claims_pkey DO UPDATE
      SET source_generation_id=EXCLUDED.source_generation_id,
          principal_oid=EXCLUDED.principal_oid,
          collection_fence=EXCLUDED.collection_fence,
          lease_expires_at=EXCLUDED.lease_expires_at;
    INSERT INTO public.scanner_running_image_generations
      (producer_instance_id,source_scope_id,source_generation_id,
       principal_oid,collection_fence,producer_grant_epoch,producer_class,
       observation_class,status,observed_from,observed_through,
       consistency_class)
    VALUES (producer_instance_id,source_scope_id,fresh_generation_id,
            v_principal_oid,fence,
            grant_row.grant_epoch,producer_class,
            observation_class,
            'staging',observed_from,observed_through,consistency_class);
    RETURN fence;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.start_running_image_collection_v1(
    TEXT,TEXT,TEXT,TEXT,TEXT,TIMESTAMPTZ,TIMESTAMPTZ,TEXT,INTEGER,BIGINT,UUID)
    FROM PUBLIC;
