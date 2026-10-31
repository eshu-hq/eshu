-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Scanner result-commit persist. The scanner worker's commit transaction
-- cannot write the protected receipt tables directly (runtime-writer holds
-- no protected DML, and granting it would trip the fail-closed ACL
-- mismatch), so the receipt and its fact associations persist here as
-- SECURITY DEFINER, following publish_impact_publication_v1 (#9468). The
-- caller keeps the pre-claim authority lock and the persisted-fact
-- membership check: this function only verifies that the attempt row still
-- carries the same fenced claim before writing. A conflicting retry with
-- the same receipt id replays as a no-op row count, exactly like the
-- previous ON CONFLICT DO NOTHING inserts.
INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
VALUES
    (1,'runtime-writer','function',
     'eshu_security.commit_scanner_result_v1(text,text,text,text,text,text,bigint,text,jsonb,timestamp with time zone,text[],bigint,uuid)',
     'EXECUTE',TRUE)
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO NOTHING;

-- commit_scanner_result_v1 persists one scan receipt plus its exact fact
-- membership and returns both inserted row counts for the caller's
-- exactly-one-receipt / exactly-N-associations checks.
CREATE OR REPLACE FUNCTION eshu_security.commit_scanner_result_v1(
    p_receipt_id TEXT,p_work_item_id TEXT,p_binding_id TEXT,
    p_scan_scope_id TEXT,p_scan_generation_id TEXT,
    p_claim_id TEXT,p_claim_fencing_token BIGINT,p_claim_owner_id TEXT,
    p_completed_authority_vector JSONB,p_completed_at TIMESTAMPTZ,
    p_fact_ids TEXT[],p_role_revision BIGINT,p_database_identity UUID,
    OUT o_receipt_rows BIGINT,OUT o_association_rows BIGINT)
RETURNS RECORD LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_role_revision,p_database_identity,'runtime-writer');
    IF v_principal_oid IS NULL OR p_receipt_id IS NULL OR p_receipt_id=''
       OR p_work_item_id IS NULL OR p_work_item_id=''
       OR p_binding_id IS NULL OR p_binding_id=''
       OR p_scan_scope_id IS NULL OR p_scan_scope_id=''
       OR p_scan_generation_id IS NULL OR p_scan_generation_id=''
       OR p_claim_id IS NULL OR p_claim_id=''
       OR p_claim_fencing_token IS NULL OR p_claim_fencing_token<=0
       OR p_claim_owner_id IS NULL OR p_claim_owner_id=''
       OR p_completed_authority_vector IS NULL
       OR jsonb_typeof(p_completed_authority_vector)<>'object'
       OR p_completed_at IS NULL
       OR p_fact_ids IS NULL OR cardinality(p_fact_ids)=0 THEN
        RAISE EXCEPTION 'scanner result commit input invalid';
    END IF;
    PERFORM 1 FROM public.scanner_image_target_attempts AS attempt
    WHERE attempt.work_item_id=p_work_item_id
      AND attempt.binding_id=p_binding_id
      AND attempt.scan_scope_id=p_scan_scope_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'scanner result commit binding changed';
    END IF;
    PERFORM 1 FROM public.workflow_work_items AS item
    WHERE item.work_item_id=p_work_item_id
      AND item.status='claimed' AND item.current_claim_id=p_claim_id
      AND item.current_fencing_token=p_claim_fencing_token
      AND item.current_owner_id=p_claim_owner_id;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'scanner result commit unauthorized';
    END IF;
    INSERT INTO public.scanner_image_scan_results
    (result_receipt_id,work_item_id,binding_id,scan_scope_id,scan_generation_id,
     claim_id,claim_fencing_token,claim_owner_id,completed_authority_vector,completed_at)
    VALUES (p_receipt_id,p_work_item_id,p_binding_id,p_scan_scope_id,p_scan_generation_id,
     p_claim_id,p_claim_fencing_token,p_claim_owner_id,p_completed_authority_vector,p_completed_at)
    ON CONFLICT DO NOTHING;
    GET DIAGNOSTICS o_receipt_rows = ROW_COUNT;
    -- Stamp the attempt with the fenced claim and its exact authority
    -- vector: nothing else writes these columns, and the derived read
    -- path joins attempt to receipt on this exact vector. Status stays
    -- for the work-item trigger to flip on claim completion.
    UPDATE public.scanner_image_target_attempts AS attempt
    SET claim_id=p_claim_id,
        claim_fencing_token=p_claim_fencing_token,
        claim_owner_id=p_claim_owner_id,
        completed_authority_vector=p_completed_authority_vector
    WHERE attempt.work_item_id=p_work_item_id
      AND attempt.binding_id=p_binding_id
      AND attempt.scan_scope_id=p_scan_scope_id;
    INSERT INTO public.scanner_image_scan_fact_associations
    (fact_id,result_receipt_id,scan_scope_id,scan_generation_id,fact_kind,
     payload_sha256,fencing_token)
    SELECT fact_id,p_receipt_id,p_scan_scope_id,p_scan_generation_id,fact_kind,
           encode(sha256(convert_to(payload::text,'UTF8')),'hex'),fencing_token
    FROM public.fact_records
    WHERE scope_id=p_scan_scope_id AND generation_id=p_scan_generation_id
      AND fact_id=ANY(p_fact_ids)
    ON CONFLICT DO NOTHING;
    GET DIAGNOSTICS o_association_rows = ROW_COUNT;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.commit_scanner_result_v1(
    TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,BIGINT,TEXT,JSONB,TIMESTAMPTZ,TEXT[],BIGINT,UUID)
FROM PUBLIC;
