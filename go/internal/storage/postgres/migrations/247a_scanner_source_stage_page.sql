-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- A bounded page transaction accepts a typed receipt, immutable observations
-- and projection outbox together. Conflicting values fail the generation
-- closed without committing any row from the rejected page.
CREATE OR REPLACE FUNCTION eshu_security.stage_running_image_page_v1(
    p_instance TEXT,p_scope TEXT,p_generation TEXT,p_fence BIGINT,
    p_delivery TEXT,p_page_index INTEGER,p_start_token_sha256 TEXT,
    p_next_token_sha256 TEXT,p_terminal BOOLEAN,p_observations JSONB,
    p_policy_revision BIGINT,p_database_identity UUID)
RETURNS JSONB LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    grant_row public.scanner_running_image_producer_grants%ROWTYPE;
    claim_row public.scanner_running_image_scope_claims%ROWTYPE;
    generation_row public.scanner_running_image_generations%ROWTYPE;
    accepted_request_sha256 TEXT;
    prior_request_sha256 TEXT;
    prior_payload_sha256 TEXT;
    payload_sha256 TEXT;
    object_slot_key TEXT;
    seen_slots JSONB := '{}'::jsonb;
    unique_slots INTEGER := 0;
    observation JSONB;
    full_repository TEXT;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_policy_revision,p_database_identity,'typed-source-producer');
    IF p_instance IS NULL OR p_instance='' OR p_scope IS NULL OR p_scope=''
       OR p_generation IS NULL OR p_generation=''
       OR p_delivery IS NULL OR p_delivery='' OR length(p_delivery)>4096
       OR p_fence<=0 OR p_page_index NOT BETWEEN 0 AND 100000
       OR p_start_token_sha256 IS NULL OR p_next_token_sha256 IS NULL
       OR p_terminal IS NULL OR p_observations IS NULL
       OR jsonb_typeof(p_observations)<>'array'
       OR jsonb_array_length(p_observations)>256
       OR octet_length(p_observations::text)>1048576 THEN
        RAISE EXCEPTION 'running-image page input invalid';
    END IF;
    accepted_request_sha256 := encode(pg_catalog.sha256(pg_catalog.convert_to(
        jsonb_build_array(p_page_index,p_start_token_sha256,
                          p_next_token_sha256,p_terminal,p_observations)::text,
        'UTF8')),'hex');
    -- An accepted receipt is idempotent even after publication or retirement.
    SELECT receipt.request_sha256 INTO prior_request_sha256
    FROM public.scanner_running_image_delivery_receipts AS receipt
    JOIN public.scanner_running_image_generations AS prior_generation
      ON prior_generation.producer_instance_id=receipt.producer_instance_id
     AND prior_generation.source_scope_id=receipt.source_scope_id
     AND prior_generation.source_generation_id=receipt.source_generation_id
    WHERE receipt.producer_instance_id=p_instance
      AND receipt.source_scope_id=p_scope
      AND receipt.source_generation_id=p_generation
      AND receipt.delivery_id=p_delivery
      AND prior_generation.principal_oid=v_principal_oid;
    IF FOUND THEN
        IF prior_request_sha256<>accepted_request_sha256 THEN
            RAISE EXCEPTION 'running-image delivery ID reused with different page';
        END IF;
        RETURN jsonb_build_object('status','duplicate',
                                  'request_sha256',accepted_request_sha256);
    END IF;
    SELECT * INTO grant_row
    FROM public.scanner_running_image_producer_grants AS allowed
    WHERE allowed.principal_oid=v_principal_oid
      AND allowed.producer_instance_id=p_instance
      AND allowed.source_scope_id=p_scope
      AND allowed.producer_class=(
          SELECT generation.producer_class
          FROM public.scanner_running_image_generations AS generation
          WHERE generation.producer_instance_id=p_instance
            AND generation.source_scope_id=p_scope
            AND generation.source_generation_id=p_generation)
      AND allowed.observation_class=(
          SELECT generation.observation_class
          FROM public.scanner_running_image_generations AS generation
          WHERE generation.producer_instance_id=p_instance
            AND generation.source_scope_id=p_scope
            AND generation.source_generation_id=p_generation)
      AND allowed.contract_version=1 AND allowed.active
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'running-image producer grant absent';
    END IF;
    SELECT * INTO claim_row
    FROM public.scanner_running_image_scope_claims AS claim
    WHERE claim.producer_instance_id=p_instance
      AND claim.source_scope_id=p_scope
      AND claim.source_generation_id=p_generation
      AND claim.principal_oid=v_principal_oid
      AND claim.collection_fence=p_fence
      AND claim.lease_expires_at>clock_timestamp()
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'running-image collection claim stale';
    END IF;
    SELECT * INTO generation_row
    FROM public.scanner_running_image_generations AS generation
    WHERE generation.producer_instance_id=p_instance
      AND generation.source_scope_id=p_scope
      AND generation.source_generation_id=p_generation
      AND generation.principal_oid=v_principal_oid
      AND generation.collection_fence=p_fence
      AND generation.producer_grant_epoch=grant_row.grant_epoch
      AND generation.producer_class=grant_row.producer_class
      AND generation.observation_class=grant_row.observation_class
      AND generation.status='staging'
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'running-image generation stale';
    END IF;
    -- A concurrent retry may have committed while this writer waited for the
    -- per-generation lock. Recheck the receipt after acquiring that lock.
    SELECT receipt.request_sha256 INTO prior_request_sha256
    FROM public.scanner_running_image_delivery_receipts AS receipt
    WHERE receipt.producer_instance_id=p_instance
      AND receipt.source_scope_id=p_scope
      AND receipt.source_generation_id=p_generation
      AND receipt.delivery_id=p_delivery;
    IF FOUND THEN
        IF prior_request_sha256<>accepted_request_sha256 THEN
            RAISE EXCEPTION 'running-image delivery ID reused with different page';
        END IF;
        RETURN jsonb_build_object('status','duplicate',
                                  'request_sha256',accepted_request_sha256);
    END IF;
    SELECT page.request_sha256 INTO prior_request_sha256
    FROM public.scanner_running_image_page_receipts AS page
    WHERE page.producer_instance_id=p_instance
      AND page.source_scope_id=p_scope
      AND page.source_generation_id=p_generation
      AND page.page_index=p_page_index;
    IF FOUND AND prior_request_sha256<>accepted_request_sha256 THEN
        UPDATE public.scanner_running_image_generations
        SET status='failed'
        WHERE producer_instance_id=p_instance AND source_scope_id=p_scope
          AND source_generation_id=p_generation;
        RETURN jsonb_build_object('status','conflict',
                                  'request_sha256',accepted_request_sha256);
    END IF;
    FOR observation IN SELECT value FROM jsonb_array_elements(p_observations)
    LOOP
        IF jsonb_typeof(observation)<>'object'
           OR EXISTS (SELECT 1 FROM jsonb_object_keys(observation) AS field(name)
                      WHERE field.name NOT IN
                        ('object_id','slot_id','producer_class','observation_class',
                         'observed_revision','registry','repository',
                         'runtime_image_reference','runtime_digest',
                         'runtime_digest_kind','last_update_status'))
           OR observation->>'object_id' IS NULL
           OR observation->>'slot_id' IS NULL
           OR observation->>'registry' IS NULL
           OR observation->>'repository' IS NULL
           OR observation->>'runtime_image_reference' IS NULL
           OR observation->>'runtime_digest' IS NULL
           OR observation->>'producer_class'<>grant_row.producer_class
           OR observation->>'observation_class'<>grant_row.observation_class
           OR observation->>'runtime_digest_kind' NOT IN ('unknown','manifest','config')
           OR observation->>'runtime_digest' !~ '^sha256:[0-9a-f]{64}$'
           OR length(observation->>'object_id') NOT BETWEEN 1 AND 4096
           OR length(observation->>'slot_id') NOT BETWEEN 1 AND 4096
           OR length(observation->>'registry') NOT BETWEEN 1 AND 4096
           OR length(observation->>'repository') NOT BETWEEN 1 AND 4096
           OR length(observation->>'runtime_image_reference') NOT BETWEEN 1 AND 4096
           OR (observation->>'object_id') ~ '[[:space:][:cntrl:]]'
           OR (observation->>'slot_id') ~ '[[:space:][:cntrl:]]'
           OR (observation->>'registry') ~ '[/@?#[:space:][:cntrl:]]'
           OR (observation->>'repository') ~ '[@?#[:space:][:cntrl:]]'
           OR (observation->>'runtime_image_reference') ~ '[?#[:space:][:cntrl:]]'
           OR (grant_row.producer_class='lambda_function'
               AND COALESCE(observation->>'observed_revision','')='') THEN
            RAISE EXCEPTION 'running-image observation invalid';
        END IF;
        full_repository := observation->>'registry' || '/' ||
                           observation->>'repository';
        IF left(observation->>'runtime_image_reference',length(full_repository)+1)
           NOT IN (full_repository||':',full_repository||'@') THEN
            RAISE EXCEPTION 'running-image observation locator mismatch';
        END IF;
        object_slot_key := jsonb_build_array(observation->>'object_id',
                                             observation->>'slot_id')::text;
        payload_sha256 := encode(pg_catalog.sha256(pg_catalog.convert_to(
            observation::text,'UTF8')),'hex');
        IF seen_slots ? object_slot_key THEN
            IF seen_slots->>object_slot_key<>payload_sha256 THEN
                UPDATE public.scanner_running_image_generations
                SET status='failed'
                WHERE producer_instance_id=p_instance AND source_scope_id=p_scope
                  AND source_generation_id=p_generation;
                RETURN jsonb_build_object('status','conflict',
                                          'request_sha256',accepted_request_sha256);
            END IF;
        ELSE
            unique_slots := unique_slots+1;
            seen_slots := jsonb_set(seen_slots,ARRAY[object_slot_key],
                                    to_jsonb(payload_sha256));
        END IF;
        SELECT existing.payload_sha256 INTO prior_payload_sha256
        FROM public.scanner_running_image_observations AS existing
        WHERE existing.producer_instance_id=p_instance
          AND existing.source_scope_id=p_scope
          AND existing.source_generation_id=p_generation
          AND existing.object_id=observation->>'object_id'
          AND existing.slot_id=observation->>'slot_id';
        IF FOUND AND prior_payload_sha256<>payload_sha256 THEN
            UPDATE public.scanner_running_image_generations
            SET status='failed'
            WHERE producer_instance_id=p_instance AND source_scope_id=p_scope
              AND source_generation_id=p_generation;
            RETURN jsonb_build_object('status','conflict',
                                      'request_sha256',accepted_request_sha256);
        END IF;
    END LOOP;
    INSERT INTO public.scanner_running_image_delivery_receipts
      (producer_instance_id,source_scope_id,source_generation_id,
       delivery_id,request_sha256)
    VALUES (p_instance,p_scope,p_generation,p_delivery,accepted_request_sha256);
    INSERT INTO public.scanner_running_image_page_receipts
      (producer_instance_id,source_scope_id,source_generation_id,page_index,
       request_sha256,start_token_sha256,next_token_sha256,unique_slots,terminal)
    VALUES (p_instance,p_scope,p_generation,p_page_index,
            accepted_request_sha256,p_start_token_sha256,p_next_token_sha256,
            unique_slots,p_terminal)
    ON CONFLICT (producer_instance_id,source_scope_id,source_generation_id,
                 page_index) DO NOTHING;
    FOR observation IN SELECT value FROM jsonb_array_elements(p_observations)
    LOOP
        INSERT INTO public.scanner_running_image_observations
          (producer_instance_id,source_scope_id,source_generation_id,
           delivery_id,object_id,slot_id,producer_class,observation_class,
           observed_revision,registry,repository,runtime_image_reference,
           runtime_digest,runtime_digest_kind,last_update_status,payload_sha256)
        VALUES (p_instance,p_scope,p_generation,p_delivery,
                observation->>'object_id',observation->>'slot_id',
                observation->>'producer_class',observation->>'observation_class',
                COALESCE(observation->>'observed_revision',''),
                observation->>'registry',observation->>'repository',
                observation->>'runtime_image_reference',
                observation->>'runtime_digest',
                observation->>'runtime_digest_kind',
                COALESCE(observation->>'last_update_status',''),
                encode(pg_catalog.sha256(pg_catalog.convert_to(
                    observation::text,'UTF8')),'hex'))
        ON CONFLICT (producer_instance_id,source_scope_id,source_generation_id,
                     object_id,slot_id) DO NOTHING;
    END LOOP;
    INSERT INTO public.scanner_running_image_projection_outbox
      (producer_instance_id,source_scope_id,source_generation_id,delivery_id)
    VALUES (p_instance,p_scope,p_generation,p_delivery);
    RETURN jsonb_build_object('status','accepted',
                              'request_sha256',accepted_request_sha256);
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.stage_running_image_page_v1(
    TEXT,TEXT,TEXT,BIGINT,TEXT,INTEGER,TEXT,TEXT,BOOLEAN,JSONB,BIGINT,UUID)
    FROM PUBLIC;
