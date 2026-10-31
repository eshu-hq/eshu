-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

CREATE TABLE IF NOT EXISTS scanner_running_image_activation_outbox (
    producer_instance_id TEXT NOT NULL,
    source_scope_id TEXT NOT NULL,
    activation_epoch BIGINT NOT NULL,
    published_generation_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
      CHECK (status IN ('pending','claimed','complete')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (producer_instance_id,source_scope_id,activation_epoch),
    FOREIGN KEY (producer_instance_id,source_scope_id,published_generation_id)
      REFERENCES scanner_running_image_generations
      (producer_instance_id,source_scope_id,source_generation_id)
);
REVOKE ALL ON scanner_running_image_activation_outbox FROM PUBLIC;

-- A complete page manifest is sealed before activation. Partial or failed
-- collection leaves the previous published pointer unchanged. A terminal
-- empty page is required for a complete empty snapshot.
CREATE OR REPLACE FUNCTION eshu_security.publish_running_image_generation_v1(
    p_instance TEXT,p_scope TEXT,p_generation TEXT,p_fence BIGINT,
    p_expected_pages INTEGER,p_expected_slots INTEGER,
    p_manifest_sha256 TEXT,p_policy_revision BIGINT,
    p_database_identity UUID)
RETURNS BIGINT LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    grant_row public.scanner_running_image_producer_grants%ROWTYPE;
    claim_row public.scanner_running_image_scope_claims%ROWTYPE;
    generation_row public.scanner_running_image_generations%ROWTYPE;
    actual_pages INTEGER;
    actual_slots INTEGER;
    terminal_pages INTEGER;
    last_page_index INTEGER;
    actual_manifest_sha256 TEXT;
    new_epoch BIGINT;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_policy_revision,p_database_identity,'typed-source-producer');
    IF p_expected_pages NOT BETWEEN 1 AND 100001
       OR p_expected_slots NOT BETWEEN 0 AND 25600256
       OR p_manifest_sha256 !~ '^[0-9a-f]{64}$'
       OR p_fence<=0 THEN
        RAISE EXCEPTION 'running-image publication input invalid';
    END IF;
    SELECT * INTO grant_row
    FROM public.scanner_running_image_producer_grants AS allowed
    WHERE allowed.principal_oid=v_principal_oid
      AND allowed.producer_instance_id=p_instance
      AND allowed.source_scope_id=p_scope
      AND allowed.contract_version=1 AND allowed.active
      AND allowed.producer_class=(SELECT generation.producer_class
          FROM public.scanner_running_image_generations AS generation
          WHERE generation.producer_instance_id=p_instance
            AND generation.source_scope_id=p_scope
            AND generation.source_generation_id=p_generation)
      AND allowed.observation_class=(SELECT generation.observation_class
          FROM public.scanner_running_image_generations AS generation
          WHERE generation.producer_instance_id=p_instance
            AND generation.source_scope_id=p_scope
            AND generation.source_generation_id=p_generation)
    FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'running-image publication producer grant absent';
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
        RAISE EXCEPTION 'running-image publication collection claim stale';
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
        RAISE EXCEPTION 'running-image publication generation stale';
    END IF;
    PERFORM 1 FROM public.collector_instances AS instance
    WHERE instance.instance_id=p_instance
      AND instance.collector_kind=CASE WHEN grant_row.producer_class='kubernetes_pod'
          THEN 'kubernetes_live' ELSE 'aws' END
      AND instance.enabled AND instance.claims_enabled FOR SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'running-image publication collector inactive';
    END IF;
    SELECT count(*),count(*) FILTER (WHERE terminal),max(page_index),
           encode(pg_catalog.sha256(pg_catalog.convert_to(
             COALESCE(string_agg(page_index::text||':'||request_sha256,','
                                 ORDER BY page_index),''),'UTF8')),'hex')
      INTO actual_pages,terminal_pages,last_page_index,actual_manifest_sha256
    FROM public.scanner_running_image_page_receipts
    WHERE producer_instance_id=p_instance AND source_scope_id=p_scope
      AND source_generation_id=p_generation;
    SELECT count(*) INTO actual_slots
    FROM public.scanner_running_image_observations
    WHERE producer_instance_id=p_instance AND source_scope_id=p_scope
      AND source_generation_id=p_generation;
    IF actual_pages<>p_expected_pages OR actual_slots<>p_expected_slots
       OR terminal_pages<>1 OR last_page_index<>p_expected_pages-1
       OR actual_manifest_sha256<>p_manifest_sha256
       OR EXISTS (
           SELECT 1 FROM public.scanner_running_image_page_receipts AS page
           WHERE page.producer_instance_id=p_instance
             AND page.source_scope_id=p_scope
             AND page.source_generation_id=p_generation
             AND (page.page_index>=p_expected_pages
               OR (page.terminal AND page.page_index<>p_expected_pages-1)
               OR (page.page_index<p_expected_pages-1 AND NOT EXISTS (
                   SELECT 1 FROM public.scanner_running_image_page_receipts AS next_page
                   WHERE next_page.producer_instance_id=p_instance
                     AND next_page.source_scope_id=p_scope
                     AND next_page.source_generation_id=p_generation
                     AND next_page.page_index=page.page_index+1
                     AND next_page.start_token_sha256=page.next_token_sha256)))) THEN
        RAISE EXCEPTION 'running-image publication snapshot incomplete';
    END IF;
    new_epoch := nextval('public.scanner_authority_epoch_seq');
    UPDATE public.scanner_running_image_generations
    SET status='published',expected_pages=p_expected_pages,
        expected_slots=p_expected_slots,manifest_sha256=p_manifest_sha256,
        publication_epoch=new_epoch
    WHERE producer_instance_id=p_instance AND source_scope_id=p_scope
      AND source_generation_id=p_generation;
    INSERT INTO public.scanner_running_image_active_scopes
      (producer_instance_id,source_scope_id,published_generation_id,
       activation_epoch,active,published_at)
    VALUES (p_instance,p_scope,p_generation,new_epoch,TRUE,clock_timestamp())
    ON CONFLICT (producer_instance_id,source_scope_id) DO UPDATE
      SET published_generation_id=EXCLUDED.published_generation_id,
          activation_epoch=EXCLUDED.activation_epoch,
          active=TRUE,published_at=EXCLUDED.published_at;
    INSERT INTO public.scanner_running_image_activation_outbox
      (producer_instance_id,source_scope_id,activation_epoch,
       published_generation_id)
    VALUES (p_instance,p_scope,new_epoch,p_generation);
    RETURN new_epoch;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.publish_running_image_generation_v1(
    TEXT,TEXT,TEXT,BIGINT,INTEGER,INTEGER,TEXT,BIGINT,UUID) FROM PUBLIC;
