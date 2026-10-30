-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Reducer impact publication persist. The reducer's impact transaction
-- cannot write the protected publication tables directly (runtime-writer
-- holds no protected DML), so publication runs here as SECURITY DEFINER,
-- following authorize_static_impact_public_v1 (#9299). It persists the
-- authorized set: static authority revisions plus one immutable revision
-- per derived target with its finding, classification, evidence, and
-- advisory rows. Set hashes are computed here over the canonical jsonb
-- encoding, so a conflicting retry at the same impact fence (same key,
-- different content) is rejected while a byte-identical retry replays as
-- a no-op. It takes the same canonical set JSON the Go adapter marshals.
INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
VALUES
    (1,'runtime-writer','function',
     'eshu_security.publish_impact_publication_v1(text,text,bigint,jsonb,bigint,uuid)',
     'EXECUTE',TRUE)
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO NOTHING;

-- publish_impact_publication_v1 persists the authorized set. Child inserts
-- after a verified identical publication row are skipped: they committed
-- with it.
CREATE OR REPLACE FUNCTION eshu_security.publish_impact_publication_v1(
    p_impact_scope TEXT,p_impact_generation TEXT,p_impact_fence BIGINT,
    p_set JSONB,p_role_revision BIGINT,p_database_identity UUID)
RETURNS VOID LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    v_principal_oid OID;
    v_finding JSONB;
    v_target JSONB;
    v_payload_sha TEXT;
    v_dependency_count INTEGER;
    v_revision public.scanner_static_impact_finding_authority_revisions%ROWTYPE;
    v_evidence_count INTEGER;
    v_publication_id TEXT;
    v_result_sha TEXT;
    v_advisory_sha TEXT;
    v_receipt_id TEXT;
    v_publication public.scanner_image_result_publications%ROWTYPE;
    v_association JSONB;
    v_dependency JSONB;
BEGIN
    v_principal_oid := eshu_security.assert_protected_capability_v1(
        p_role_revision,p_database_identity,'runtime-writer');
    IF v_principal_oid IS NULL OR p_impact_scope IS NULL OR p_impact_scope=''
       OR p_impact_generation IS NULL OR p_impact_generation=''
       OR p_impact_fence<=0 OR p_set IS NULL
       OR jsonb_typeof(p_set)<>'object'
       OR jsonb_typeof(COALESCE(p_set->'static_findings','[]'))<>'array'
       OR jsonb_typeof(COALESCE(p_set->'target_publications','[]'))<>'array' THEN
        RAISE EXCEPTION 'impact publication input invalid';
    END IF;
    FOR v_finding IN
        SELECT value FROM jsonb_array_elements(
            COALESCE(p_set->'static_findings','[]'::jsonb)) AS finding(value)
        ORDER BY finding.value->>'finding_fact_id'
    LOOP
        IF v_finding->>'finding_fact_id' IS NULL
           OR v_finding->>'payload_json' IS NULL
           OR jsonb_typeof(COALESCE(v_finding->'advisory_evidence','[]'))<>'array' THEN
            RAISE EXCEPTION 'impact static finding invalid';
        END IF;
        v_payload_sha := encode(pg_catalog.sha256(pg_catalog.convert_to(
            v_finding->>'payload_json','UTF8')),'hex');
        v_dependency_count := (SELECT count(*)
            FROM jsonb_array_elements(
                COALESCE(v_finding->'advisory_evidence','[]'::jsonb)));
        INSERT INTO public.scanner_static_impact_finding_authority_revisions
            (finding_fact_id,impact_scope_id,impact_generation_id,
             impact_fencing_token,finding_payload_sha256,
             advisory_dependency_count,pinned_role_policy_revision,
             pinned_database_identity)
        VALUES (v_finding->>'finding_fact_id',p_impact_scope,p_impact_generation,
                p_impact_fence,v_payload_sha,v_dependency_count,
                p_role_revision,p_database_identity)
        ON CONFLICT (finding_fact_id,impact_fencing_token) DO NOTHING;
        SELECT * INTO v_revision
        FROM public.scanner_static_impact_finding_authority_revisions AS revision
        WHERE revision.finding_fact_id=v_finding->>'finding_fact_id'
          AND revision.impact_fencing_token=p_impact_fence;
        IF v_revision.impact_scope_id<>p_impact_scope
           OR v_revision.impact_generation_id<>p_impact_generation
           OR v_revision.finding_payload_sha256<>v_payload_sha
           OR v_revision.advisory_dependency_count<>v_dependency_count
           OR v_revision.pinned_role_policy_revision<>p_role_revision
           OR v_revision.pinned_database_identity<>p_database_identity THEN
            RAISE EXCEPTION 'impact static publication conflicts at fence %',p_impact_fence;
        END IF;
        SELECT count(*) INTO v_evidence_count
        FROM public.scanner_static_impact_public_advisory_evidence AS evidence
        WHERE evidence.finding_fact_id=v_finding->>'finding_fact_id'
          AND evidence.impact_fencing_token=p_impact_fence;
        IF v_evidence_count<>v_dependency_count
           OR EXISTS (
               SELECT 1 FROM public.scanner_static_impact_public_advisory_evidence AS evidence
               WHERE evidence.finding_fact_id=v_finding->>'finding_fact_id'
                 AND evidence.impact_fencing_token=p_impact_fence
                 AND NOT EXISTS (
                   SELECT 1 FROM jsonb_array_elements(
                       COALESCE(v_finding->'advisory_evidence','[]'::jsonb)) AS wanted(value)
                   WHERE wanted.value->>'evidence_fact_id'=evidence.evidence_fact_id)) THEN
            IF v_evidence_count>0 THEN
                RAISE EXCEPTION 'impact static evidence conflicts at fence %',p_impact_fence;
            END IF;
            INSERT INTO public.scanner_static_impact_public_advisory_evidence
                (finding_fact_id,impact_fencing_token,evidence_fact_id,
                 registration_id,generation_receipt_id,registration_epoch,
                 activation_epoch,evidence_payload_sha256)
            SELECT v_finding->>'finding_fact_id',p_impact_fence,
                   wanted.value->>'evidence_fact_id',
                   wanted.value->>'registration_id',
                   wanted.value->>'generation_receipt_id',
                   (wanted.value->>'registration_epoch')::BIGINT,
                   (wanted.value->>'activation_epoch')::BIGINT,
                   wanted.value->>'evidence_payload_sha256'
            FROM jsonb_array_elements(
                COALESCE(v_finding->'advisory_evidence','[]'::jsonb)) AS wanted(value)
            ON CONFLICT (finding_fact_id,impact_fencing_token,evidence_fact_id)
            DO NOTHING;
        END IF;
        INSERT INTO public.scanner_image_result_fact_classifications
            (finding_fact_id,source_class,lifecycle)
        VALUES (v_finding->>'finding_fact_id','static','active')
        ON CONFLICT (finding_fact_id) DO NOTHING;
    END LOOP;
    FOR v_target IN
        SELECT value FROM jsonb_array_elements(
            COALESCE(p_set->'target_publications','[]'::jsonb)) AS target(value)
        ORDER BY target.value->>'scan_target_id',target.value->>'binding_id'
    LOOP
        IF v_target->>'scan_target_id' IS NULL OR v_target->>'binding_id' IS NULL
           OR v_target->>'work_item_id' IS NULL
           OR v_target->>'scan_generation_id' IS NULL
           OR v_target->>'analysis_version' IS NULL
           OR jsonb_typeof(COALESCE(v_target->'finding_payloads','[]'))<>'array'
           OR jsonb_typeof(COALESCE(v_target->'authority_dependencies','[]'))<>'array' THEN
            RAISE EXCEPTION 'impact target publication invalid';
        END IF;
        v_result_sha := encode(pg_catalog.sha256(pg_catalog.convert_to(
            (COALESCE(v_target->'finding_payloads','[]'::jsonb))::text,'UTF8')),'hex');
        v_advisory_sha := encode(pg_catalog.sha256(pg_catalog.convert_to(
            (COALESCE(v_target->'authority_dependencies','[]'::jsonb))::text,'UTF8')),'hex');
        v_publication_id := encode(pg_catalog.sha256(pg_catalog.convert_to(
            p_impact_scope||chr(31)||p_impact_generation||chr(31)||
            p_impact_fence::text||chr(31)||(v_target->>'scan_target_id'),'UTF8')),'hex');
        SELECT result.result_receipt_id INTO v_receipt_id
        FROM public.scanner_image_scan_results AS result
        WHERE result.work_item_id=v_target->>'work_item_id'
          AND result.binding_id=v_target->>'binding_id'
          AND result.scan_scope_id=v_target->>'scan_target_id'
          AND result.scan_generation_id=v_target->>'scan_generation_id';
        IF NOT FOUND THEN
            RAISE EXCEPTION 'impact publication scan receipt absent';
        END IF;
        INSERT INTO public.scanner_image_result_publications
            (publication_id,scan_scope_id,scan_generation_id,binding_id,
             work_item_id,scan_result_receipt_id,impact_scope_id,
             impact_generation_id,impact_fencing_token,result_set_sha256,
             advisory_set_sha256,analysis_version,role_policy_revision,
             database_identity,status,published_at)
        VALUES (v_publication_id,v_target->>'scan_target_id',
                v_target->>'scan_generation_id',v_target->>'binding_id',
                v_target->>'work_item_id',v_receipt_id,p_impact_scope,
                p_impact_generation,p_impact_fence,v_result_sha,
                v_advisory_sha,v_target->>'analysis_version',
                p_role_revision,p_database_identity,'current',clock_timestamp())
        ON CONFLICT (impact_scope_id,impact_generation_id,
                     impact_fencing_token,scan_scope_id) DO NOTHING;
        SELECT * INTO v_publication
        FROM public.scanner_image_result_publications AS publication
        WHERE publication.impact_scope_id=p_impact_scope
          AND publication.impact_generation_id=p_impact_generation
          AND publication.impact_fencing_token=p_impact_fence
          AND publication.scan_scope_id=v_target->>'scan_target_id';
        IF v_publication.result_set_sha256<>v_result_sha
           OR v_publication.advisory_set_sha256<>v_advisory_sha
           OR v_publication.binding_id<>v_target->>'binding_id'
           OR v_publication.work_item_id<>v_target->>'work_item_id'
           OR v_publication.scan_result_receipt_id<>v_receipt_id
           OR v_publication.scan_generation_id<>v_target->>'scan_generation_id'
           OR v_publication.analysis_version<>v_target->>'analysis_version'
           OR v_publication.role_policy_revision<>p_role_revision
           OR v_publication.database_identity<>p_database_identity
           OR v_publication.status<>'current' THEN
            RAISE EXCEPTION 'impact target publication conflicts at fence %',p_impact_fence;
        END IF;
        IF v_publication.publication_id<>v_publication_id THEN
            CONTINUE;
        END IF;
        FOR v_dependency IN
            SELECT value FROM jsonb_array_elements(
                COALESCE(v_target->'authority_dependencies','[]'::jsonb)
            ) AS dependency(value)
            ORDER BY dependency.value->>'kind',dependency.value->>'id'
        LOOP
            INSERT INTO public.scanner_image_result_advisory_dependencies
                (publication_id,authority_kind,authority_id,authority_epoch,
                 advisory_scope_id,advisory_generation_id,advisory_scope_epoch,
                 provider_id,trusted_source_class,content_profile)
            VALUES (v_publication_id,v_dependency->>'kind',v_dependency->>'id',
                    (v_dependency->>'authority_epoch')::BIGINT,
                    v_dependency->>'advisory_scope_id',
                    v_dependency->>'advisory_generation_id',
                    (v_dependency->>'advisory_scope_epoch')::BIGINT,
                    v_dependency->>'provider_id',
                    v_dependency->>'trusted_source_class',
                    v_dependency->>'content_profile');
        END LOOP;
        FOR v_association IN
            SELECT value FROM jsonb_array_elements(
                COALESCE(v_target->'finding_payloads','[]'::jsonb)
            ) AS finding(value)
            ORDER BY finding.value->>'fact_id'
        LOOP
            INSERT INTO public.scanner_image_result_fact_associations
                (finding_fact_id,publication_id,scan_scope_id,
                 scan_generation_id,tenant_id,workspace_id)
            VALUES (v_association->>'fact_id',v_publication_id,
                    v_target->>'scan_target_id',v_target->>'scan_generation_id',
                    COALESCE(v_target->>'tenant_id',''),
                    COALESCE(v_target->>'workspace_id',''));
            INSERT INTO public.scanner_image_result_fact_classifications
                (finding_fact_id,source_class,lifecycle)
            VALUES (v_association->>'fact_id','derived','active')
            ON CONFLICT (finding_fact_id) DO NOTHING;
        END LOOP;
        FOR v_association IN
            SELECT value FROM jsonb_array_elements(
                COALESCE(v_target->'evidence_associations','[]'::jsonb)
            ) AS evidence(value)
            ORDER BY evidence.value->>'finding_fact_id',
                     evidence.value->>'evidence_fact_id'
        LOOP
            INSERT INTO public.scanner_image_result_evidence_associations
                (finding_fact_id,evidence_fact_id,publication_id,
                 scan_scope_id,scan_generation_id,evidence_class,
                 authority_kind,authority_id,authority_epoch)
            VALUES (v_association->>'finding_fact_id',
                    v_association->>'evidence_fact_id',v_publication_id,
                    v_target->>'scan_target_id',v_target->>'scan_generation_id',
                    v_association->>'evidence_class',
                    NULLIF(v_association->>'authority_kind',''),
                    NULLIF(v_association->>'authority_id',''),
                    NULLIF(v_association->>'authority_epoch','')::BIGINT);
        END LOOP;
    END LOOP;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.publish_impact_publication_v1(
    TEXT,TEXT,BIGINT,JSONB,BIGINT,UUID) FROM PUBLIC;
