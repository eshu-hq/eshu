-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Fixed migration-owned inventory. The installer changes contracts and
-- policy revision but cannot accept caller-provided ACL rules.
WITH profiles(profile) AS (
    VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
           ('typed-source-producer'),('schema-bootstrap')
), protected_objects(object_identity) AS (
    VALUES ('eshu_security.role_policy_state'),('eshu_security.role_contracts'),
           ('eshu_security.role_acl_inventory'),
           ('public.scanner_running_image_producer_grants'),
           ('public.scanner_running_image_scope_claims'),
           ('public.scanner_running_image_generations'),
           ('public.scanner_running_image_page_receipts'),
           ('public.scanner_running_image_delivery_receipts'),
           ('public.scanner_running_image_observations'),
           ('public.scanner_running_image_active_scopes'),
           ('public.scanner_authority_epochs'),
           ('public.scanner_image_result_publications'),
           ('public.scanner_image_result_fact_associations'),
           ('public.scanner_image_result_fact_classifications'),
           ('public.scanner_image_result_evidence_associations'),
           ('public.scanner_image_result_advisory_dependencies'),
           ('public.scanner_image_result_reuse_receipts'),
           ('public.scanner_image_result_suppressions'),
           ('public.scanner_static_impact_finding_authority_revisions'),
           ('public.scanner_static_impact_public_advisory_evidence'),
           ('public.scanner_advisory_public_registrations'),
           ('public.scanner_advisory_public_generations'),
           ('public.scanner_advisory_private_bindings'),
           ('public.scanner_advisory_desired_policy'),
           ('public.scanner_advisory_owned_instances'),
           ('public.scanner_advisory_producer_grants'),
           ('public.scanner_advisory_generation_receipts'),
           ('public.scanner_advisory_generation_facts'),
           ('public.scanner_advisory_public_activations'),
           ('public.scanner_image_scan_results'),
           ('public.scanner_image_scan_fact_associations'),
           ('public.scanner_running_image_projection_outbox'),
           ('public.scanner_running_image_activation_outbox'),
           ('public.scanner_running_image_manifest_verifications'),
           ('public.scanner_image_discovery_cursors')
), privileges(privilege_name) AS (
    VALUES ('INSERT'),('UPDATE'),('DELETE'),('TRUNCATE'),('TRIGGER')
)
INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
SELECT 1,p.profile,'table',o.object_identity,v.privilege_name,
       p.profile='schema-bootstrap'
FROM profiles AS p CROSS JOIN protected_objects AS o CROSS JOIN privileges AS v
UNION ALL
SELECT 1,p.profile,'table_owner',o.object_identity,'OWNER',TRUE
FROM profiles AS p CROSS JOIN protected_objects AS o
UNION ALL
SELECT 1,p.profile,'table',o.object_identity,'SELECT',
       p.profile='schema-bootstrap' OR
       o.object_identity IN ('eshu_security.role_policy_state',
                             'eshu_security.role_contracts',
                             'eshu_security.role_acl_inventory') OR
       p.profile IN ('runtime-writer','runtime-reader','authority-control')
FROM profiles AS p CROSS JOIN protected_objects AS o
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO NOTHING;

INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
SELECT 1,profile,'schema','eshu_security','USAGE',TRUE
FROM (VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
             ('typed-source-producer'),('schema-bootstrap')) AS p(profile)
UNION ALL
SELECT 1,profile,'schema','eshu_security','CREATE',profile='schema-bootstrap'
FROM (VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
             ('typed-source-producer'),('schema-bootstrap')) AS p(profile)
UNION ALL
SELECT 1,profile,'schema','public','CREATE',profile='schema-bootstrap'
FROM (VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
             ('typed-source-producer'),('schema-bootstrap')) AS p(profile)
UNION ALL
SELECT 1,profile,'database','current_database','CREATE',FALSE
FROM (VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
             ('typed-source-producer'),('schema-bootstrap')) AS p(profile)
UNION ALL
SELECT 1,profile,'function',
       'eshu_security.attest_connection_v1(text,bigint,text,text)',
       'EXECUTE',TRUE
FROM (VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
             ('typed-source-producer'),('schema-bootstrap')) AS p(profile)
UNION ALL
SELECT 1,profile,'function','pg_catalog.pg_control_system()',
       'EXECUTE',TRUE
FROM (VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
             ('typed-source-producer'),('schema-bootstrap')) AS p(profile)
UNION ALL
SELECT 1,p.profile,'function',f.object_identity,'EXECUTE',
       p.profile='schema-bootstrap' OR
       (p.profile='typed-source-producer' AND f.capability='producer') OR
       (p.profile='runtime-writer' AND f.capability='scanner-worker') OR
       (p.profile='authority-control' AND f.capability='control')
FROM (VALUES ('runtime-writer'),('runtime-reader'),('authority-control'),
             ('typed-source-producer'),('schema-bootstrap')) AS p(profile)
CROSS JOIN (VALUES
    ('eshu_security.start_running_image_collection_v1(text,text,text,text,text,timestamp with time zone,timestamp with time zone,text,integer,bigint,uuid)','producer'),
    ('eshu_security.stage_running_image_page_v1(text,text,text,bigint,text,integer,text,text,boolean,jsonb,bigint,uuid)','producer'),
    ('eshu_security.publish_running_image_generation_v1(text,text,text,bigint,integer,integer,text,bigint,uuid)','producer'),
    ('eshu_security.authorize_scanner_attempt_v1(text,text,bigint,text)','scanner-worker'),
    ('eshu_security.authorize_scanner_discovery_v1(bigint,uuid)','control'),
    ('eshu_security.advance_scanner_discovery_cursor_v1(jsonb,jsonb,jsonb)','control'),
    ('eshu_security.validate_scanner_source_admission_v1(bigint,text,text,text,text,text,text,text,text,text,text[],bigint,uuid)','control'),
    ('eshu_security.reconcile_public_advisories_v1(bigint,bigint,jsonb,bigint,uuid)','control'),
    ('eshu_security.fence_advisory_owned_instance_v1()','owner-only'),
    ('eshu_security.bump_advisory_registration_epoch_v1()','owner-only'),
    ('eshu_security.bump_advisory_producer_grant_epoch_v1()','owner-only'),
    ('eshu_security.guard_advisory_generation_retirement_v1()','owner-only'),
    ('eshu_security.guard_advisory_desired_revision_v1()','owner-only'),
    ('eshu_security.guard_public_advisory_completion_v1()','owner-only'),
    ('eshu_security.stage_public_advisory_generation_v1(text,text,text,text,bigint,text,bigint,uuid)','producer'),
    ('eshu_security.lock_public_advisory_receipt_v1(text,bigint,uuid)','owner-only'),
    ('eshu_security.record_public_advisory_facts_v1(text,jsonb,bigint,uuid)','producer'),
    ('eshu_security.complete_public_advisory_generation_v1(text,text,text,bigint,uuid)','producer'),
    ('eshu_security.publish_public_advisory_generation_v1(text,text,text,text,bigint,text,bigint,uuid)','producer'),
    ('eshu_security.assert_protected_capability_v1(bigint,uuid,text)','owner-only'),
    ('eshu_security.install_role_policy_v1(bigint,bigint,jsonb)','owner-only')
) AS f(object_identity,capability)
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO NOTHING;

CREATE OR REPLACE FUNCTION eshu_security.install_role_policy_v1(
    expected_current_revision BIGINT, new_revision BIGINT, contracts JSONB)
RETURNS UUID LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    state_row eshu_security.role_policy_state%ROWTYPE;
    contract_json JSONB;
    login_role pg_catalog.pg_roles%ROWTYPE;
    effective_role pg_catalog.pg_roles%ROWTYPE;
    owner_oid OID;
    database_oid OID;
    system_id TEXT;
    incarnation UUID;
    contract_profile TEXT;
    contract_route TEXT;
    former_name TEXT;
    former_profile TEXT;
    protected_name TEXT;
    protected_function TEXT;
    ingestion_acl RECORD;
BEGIN
    SELECT relowner INTO owner_oid FROM pg_catalog.pg_class
    WHERE oid='eshu_security.role_policy_state'::regclass;
    IF owner_oid IS NULL OR (SELECT oid FROM pg_catalog.pg_roles
                             WHERE rolname=CURRENT_USER) <> owner_oid THEN
        RAISE EXCEPTION 'role policy install requires effective protected owner';
    END IF;
    IF contracts IS NULL OR jsonb_typeof(contracts) <> 'array'
       OR jsonb_array_length(contracts)=0 OR jsonb_array_length(contracts)>256
       OR new_revision <= expected_current_revision THEN
        RAISE EXCEPTION 'role policy install input invalid';
    END IF;
    SELECT oid INTO database_oid FROM pg_catalog.pg_database
    WHERE datname=CURRENT_DATABASE();
    SELECT control.system_identifier::text INTO system_id
    FROM pg_catalog.pg_control_system() AS control;
    SELECT * INTO state_row FROM eshu_security.role_policy_state
    WHERE singleton FOR UPDATE;
    IF FOUND AND state_row.policy_revision <> expected_current_revision THEN
        RAISE EXCEPTION 'role policy revision conflict';
    ELSIF NOT FOUND AND expected_current_revision <> 0 THEN
        RAISE EXCEPTION 'role policy initial revision conflict';
    END IF;
    IF FOUND AND state_row.system_identifier=system_id
       AND state_row.database_oid=database_oid THEN
        incarnation := state_row.database_identity;
    ELSE
        incarnation := pg_catalog.gen_random_uuid();
    END IF;
    -- Old grants are revoked inside the same policy transaction. Old pooled
    -- sessions also fail the protected transaction-time revision check.
    FOR former_name,former_profile IN
        SELECT role_state.rolname,old_contract.profile
        FROM eshu_security.role_contracts AS old_contract
        JOIN pg_catalog.pg_roles AS role_state
          ON role_state.oid=old_contract.session_role_oid
    LOOP
        EXECUTE format('REVOKE USAGE ON SCHEMA eshu_security FROM %I',former_name);
        EXECUTE format('REVOKE SELECT ON eshu_security.role_policy_state, eshu_security.role_contracts, eshu_security.role_acl_inventory FROM %I',former_name);
        EXECUTE format('REVOKE EXECUTE ON FUNCTION eshu_security.attest_connection_v1(text,bigint,text,text) FROM %I',former_name);
        FOR protected_function IN
            SELECT object_identity FROM eshu_security.role_acl_inventory
            WHERE inventory_version=1 AND object_kind='function'
              AND object_identity LIKE 'eshu_security.%'
        LOOP
            EXECUTE format('REVOKE EXECUTE ON FUNCTION %s FROM %I',
                           protected_function,former_name);
        END LOOP;
        FOR protected_name IN
            SELECT object_identity FROM eshu_security.role_acl_inventory
            WHERE inventory_version=1 AND object_kind='table'
              AND privilege_name='SELECT' AND object_identity LIKE 'public.scanner_%'
        LOOP
            EXECUTE format('REVOKE SELECT ON TABLE %I.%I FROM %I',
                           split_part(protected_name,'.',1),
                           split_part(protected_name,'.',2),former_name);
        END LOOP;
        -- A retired typed producer must lose its ordinary fact transaction
        -- grants as well as its protected stage/publish entrypoints.
        IF former_profile='typed-source-producer' THEN
            FOR ingestion_acl IN
                SELECT object_identity,privilege_name
                FROM eshu_security.role_acl_inventory
                WHERE inventory_version=1 AND profile='typed-source-producer'
                  AND object_kind='table' AND required
                  AND object_identity LIKE 'public.%'
                  AND object_identity NOT LIKE 'public.scanner_%'
            LOOP
                EXECUTE format('REVOKE %s ON TABLE %I.%I FROM %I',
                               ingestion_acl.privilege_name,
                               split_part(ingestion_acl.object_identity,'.',1),
                               split_part(ingestion_acl.object_identity,'.',2),former_name);
            END LOOP;
        END IF;
    END LOOP;
    DELETE FROM eshu_security.role_contracts;
    FOR contract_json IN SELECT value FROM jsonb_array_elements(contracts)
    LOOP
        SELECT * INTO login_role FROM pg_catalog.pg_roles
        WHERE rolname=contract_json->>'session_role';
        SELECT * INTO effective_role FROM pg_catalog.pg_roles
        WHERE rolname=contract_json->>'effective_role';
        contract_profile := contract_json->>'profile';
        contract_route := contract_json->>'connection_route';
        IF login_role.oid IS NULL OR effective_role.oid IS NULL
           OR NOT login_role.rolcanlogin OR login_role.rolsuper
           OR login_role.rolcreatedb OR login_role.rolcreaterole
           OR login_role.rolreplication OR login_role.rolbypassrls
           OR NOT EXISTS (SELECT 1 FROM eshu_security.role_acl_inventory
                          WHERE inventory_version=1 AND profile=contract_profile)
           OR NOT ((contract_route='writer' AND contract_profile='runtime-writer')
                OR (contract_route='reader' AND contract_profile='runtime-reader')
                OR (contract_route='control' AND contract_profile='authority-control')
                OR (contract_route='producer' AND contract_profile='typed-source-producer')
                OR (contract_route='bootstrap' AND contract_profile='schema-bootstrap'))
           OR (contract_profile='schema-bootstrap' AND effective_role.oid<>owner_oid)
           OR (contract_profile<>'schema-bootstrap' AND effective_role.oid<>login_role.oid)
           OR EXISTS (
               SELECT 1 FROM eshu_security.role_contracts AS existing
               WHERE existing.session_role_oid=login_role.oid
                 AND existing.profile<>contract_profile
           )
           OR contract_json->>'contract_id' IS NULL
           OR contract_json->>'workload_class' IS NULL THEN
            RAISE EXCEPTION 'role policy contract invalid';
        END IF;
        INSERT INTO eshu_security.role_contracts
          (contract_id,policy_revision,workload_class,connection_route,profile,
           session_role_oid,effective_role_oid,enabled)
        VALUES (contract_json->>'contract_id',new_revision,
                contract_json->>'workload_class',contract_route,contract_profile,
                login_role.oid,effective_role.oid,TRUE);
        EXECUTE format('GRANT USAGE ON SCHEMA eshu_security TO %I',login_role.rolname);
        EXECUTE format('GRANT SELECT ON eshu_security.role_policy_state, eshu_security.role_contracts, eshu_security.role_acl_inventory TO %I',login_role.rolname);
        EXECUTE format('GRANT EXECUTE ON FUNCTION eshu_security.attest_connection_v1(text,bigint,text,text) TO %I',login_role.rolname);
        FOR protected_function IN
            SELECT object_identity FROM eshu_security.role_acl_inventory
            WHERE inventory_version=1 AND profile=contract_profile
              AND object_kind='function' AND privilege_name='EXECUTE'
              AND required AND object_identity LIKE 'eshu_security.%'
        LOOP
            EXECUTE format('GRANT EXECUTE ON FUNCTION %s TO %I',
                           protected_function,login_role.rolname);
        END LOOP;
        IF contract_profile IN ('runtime-writer','runtime-reader','authority-control') THEN
            FOR protected_name IN
                SELECT object_identity FROM eshu_security.role_acl_inventory
                WHERE inventory_version=1 AND profile=contract_profile
                  AND object_kind='table' AND privilege_name='SELECT'
                  AND required AND object_identity LIKE 'public.scanner_%'
            LOOP
                EXECUTE format('GRANT SELECT ON TABLE %I.%I TO %I',
                               split_part(protected_name,'.',1),
                               split_part(protected_name,'.',2),login_role.rolname);
            END LOOP;
        END IF;
        IF contract_profile='typed-source-producer' THEN
            -- public schema USAGE is a DBA pregrant checked by the fixed
            -- inventory. The protected owner need not own that schema and
            -- must not silently assume it has GRANT OPTION.
            FOR ingestion_acl IN
                SELECT object_identity,privilege_name
                FROM eshu_security.role_acl_inventory
                WHERE inventory_version=1 AND profile='typed-source-producer'
                  AND object_kind='table' AND required
                  AND object_identity LIKE 'public.%'
                  AND object_identity NOT LIKE 'public.scanner_%'
            LOOP
                EXECUTE format('GRANT %s ON TABLE %I.%I TO %I',
                               ingestion_acl.privilege_name,
                               split_part(ingestion_acl.object_identity,'.',1),
                               split_part(ingestion_acl.object_identity,'.',2),login_role.rolname);
            END LOOP;
        END IF;
    END LOOP;
    INSERT INTO eshu_security.role_policy_state
      (singleton,policy_revision,database_oid,database_identity,
       system_identifier,inventory_version,owner_role_oid,authority_epoch,active)
    VALUES (TRUE,new_revision,database_oid,incarnation,
            system_id,1,owner_oid,nextval('public.scanner_authority_epoch_seq'),TRUE)
    ON CONFLICT (singleton) DO UPDATE
      SET policy_revision=EXCLUDED.policy_revision,
          database_oid=EXCLUDED.database_oid,
          database_identity=EXCLUDED.database_identity,
          system_identifier=EXCLUDED.system_identifier,
          inventory_version=EXCLUDED.inventory_version,
          owner_role_oid=EXCLUDED.owner_role_oid,
          authority_epoch=EXCLUDED.authority_epoch,active=TRUE;
    RETURN incarnation;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.install_role_policy_v1(
    BIGINT,BIGINT,JSONB) FROM PUBLIC;
