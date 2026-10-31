-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The coordinator's fixed capability locks current policy and principal
-- before it touches source, target, work or cursor rows in an admission.
CREATE OR REPLACE FUNCTION eshu_security.authorize_scanner_discovery_v1(
    p_policy_revision BIGINT,p_database_identity UUID)
RETURNS BOOLEAN LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    principal OID;
BEGIN
    principal := eshu_security.assert_protected_capability_v1(
        p_policy_revision,p_database_identity,'authority-control');
    IF NOT EXISTS (
        SELECT 1 FROM eshu_security.role_contracts AS contract
        WHERE contract.policy_revision=p_policy_revision
          AND contract.session_role_oid=principal
          AND contract.effective_role_oid=principal
          AND contract.workload_class='workflow-coordinator'
          AND contract.connection_route='control'
          AND contract.profile='authority-control' AND contract.enabled
    ) THEN
        RAISE EXCEPTION 'scanner discovery principal denied';
    END IF;
    RETURN TRUE;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.authorize_scanner_discovery_v1(
    BIGINT,UUID) FROM PUBLIC;

-- The caller has already validated a page under current source/grant policy.
-- This CAS is deliberately a progress hint: each actual target admission
-- separately locks and validates its immutable observation and authority.
CREATE OR REPLACE FUNCTION eshu_security.advance_scanner_discovery_cursor_v1(
    p_request JSONB,p_expected JSONB,p_next JSONB)
RETURNS BOOLEAN LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    expected_version BIGINT;
    next_version BIGINT;
    expected_upper BIGINT;
    expected_after BIGINT;
    next_upper BIGINT;
    next_after BIGINT;
    changed_rows INTEGER;
BEGIN
    IF jsonb_typeof(p_request)<>'object'
       OR jsonb_typeof(p_expected)<>'object'
       OR jsonb_typeof(p_next)<>'object' THEN
        RAISE EXCEPTION 'scanner discovery cursor request invalid';
    END IF;
    PERFORM eshu_security.authorize_scanner_discovery_v1(
        (p_request->>'expected_role_policy_revision')::bigint,
        (p_request->>'expected_database_identity')::uuid);
    expected_version := (p_expected->>'version')::bigint;
    next_version := (p_next->>'version')::bigint;
    expected_upper := (p_expected->>'cycle_upper_observation_id')::bigint;
    expected_after := (p_expected->>'after_observation_id')::bigint;
    next_upper := (p_next->>'cycle_upper_observation_id')::bigint;
    next_after := (p_next->>'after_observation_id')::bigint;
    IF expected_version IS NULL OR next_version IS NULL
       OR expected_version<0 OR next_version<>expected_version+1
       OR expected_upper IS NULL OR expected_after IS NULL
       OR next_upper IS NULL OR next_after IS NULL
       OR expected_upper<0 OR expected_after<0 OR expected_after>expected_upper
       OR next_upper<0 OR next_after<0 OR next_after>next_upper
       OR (next_upper=0 AND
           (next_after<>0 OR p_next->>'after_authorization_domain'<>''))
       OR COALESCE(p_request->>'scanner_collector_instance_id','')=''
       OR (p_request->>'discovery_policy_revision')::bigint<=0
       OR p_request->>'authorization_namespace' NOT IN ('local','hosted') THEN
        RAISE EXCEPTION 'scanner discovery cursor state invalid';
    END IF;
    IF expected_version=0 THEN
        INSERT INTO public.scanner_image_discovery_cursors
          (scanner_collector_instance_id,authorization_namespace,tenant_id,
           workspace_id,subject_class,policy_revision_hash,
           discovery_policy_revision,version,cycle_upper_observation_id,
           after_observation_id,after_authorization_domain,advanced_at)
        VALUES (p_request->>'scanner_collector_instance_id',
                p_request->>'authorization_namespace',
                COALESCE(p_request->>'tenant_id',''),
                COALESCE(p_request->>'workspace_id',''),
                COALESCE(p_request->>'subject_class',''),
                COALESCE(p_request->>'policy_revision_hash',''),
                (p_request->>'discovery_policy_revision')::bigint,
                next_version,next_upper,next_after,
                COALESCE(p_next->>'after_authorization_domain',''),
                clock_timestamp())
        ON CONFLICT DO NOTHING;
        GET DIAGNOSTICS changed_rows = ROW_COUNT;
        RETURN changed_rows=1;
    END IF;
    UPDATE public.scanner_image_discovery_cursors AS cursor
    SET version=next_version,cycle_upper_observation_id=next_upper,
        after_observation_id=next_after,
        after_authorization_domain=COALESCE(
            p_next->>'after_authorization_domain',''),
        advanced_at=clock_timestamp()
    WHERE cursor.scanner_collector_instance_id=
          p_request->>'scanner_collector_instance_id'
      AND cursor.authorization_namespace=p_request->>'authorization_namespace'
      AND cursor.tenant_id=COALESCE(p_request->>'tenant_id','')
      AND cursor.workspace_id=COALESCE(p_request->>'workspace_id','')
      AND cursor.subject_class=COALESCE(p_request->>'subject_class','')
      AND cursor.policy_revision_hash=COALESCE(
          p_request->>'policy_revision_hash','')
      AND cursor.discovery_policy_revision=
          (p_request->>'discovery_policy_revision')::bigint
      AND cursor.version=expected_version
      AND cursor.cycle_upper_observation_id=expected_upper
      AND cursor.after_observation_id=expected_after
      AND cursor.after_authorization_domain=COALESCE(
          p_expected->>'after_authorization_domain','');
    GET DIAGNOSTICS changed_rows = ROW_COUNT;
    RETURN changed_rows=1;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.advance_scanner_discovery_cursor_v1(
    JSONB,JSONB,JSONB) FROM PUBLIC;
