-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Physical-session attestation runs with the caller's actual effective role.
-- Protected SECURITY DEFINER APIs perform a separate transaction-time check.
CREATE OR REPLACE FUNCTION eshu_security.attest_connection_v1(
    contract_id TEXT, expected_policy_revision BIGINT,
    workload_class TEXT, connection_route TEXT)
RETURNS TABLE (
    policy_revision BIGINT, profile TEXT, session_role_oid OID,
    effective_role_oid OID, database_oid OID, database_identity UUID,
    acl_inventory_version INTEGER)
LANGUAGE plpgsql SECURITY INVOKER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    state_row eshu_security.role_policy_state%ROWTYPE;
    contract_row eshu_security.role_contracts%ROWTYPE;
    inventory_row eshu_security.role_acl_inventory%ROWTYPE;
    login_oid OID;
    effective_oid OID;
    db_oid OID;
    system_id TEXT;
    permitted BOOLEAN;
    inventory_count INTEGER := 0;
BEGIN
    SELECT oid INTO login_oid FROM pg_catalog.pg_roles WHERE rolname=SESSION_USER;
    SELECT oid INTO effective_oid FROM pg_catalog.pg_roles WHERE rolname=CURRENT_USER;
    SELECT oid INTO db_oid FROM pg_catalog.pg_database WHERE datname=CURRENT_DATABASE();
    SELECT control.system_identifier::text INTO system_id
    FROM pg_catalog.pg_control_system() AS control;
    IF login_oid IS NULL OR effective_oid IS NULL OR db_oid IS NULL
       OR system_id IS NULL THEN
        RAISE EXCEPTION 'role attestation identity unavailable';
    END IF;
    SELECT * INTO state_row FROM eshu_security.role_policy_state WHERE singleton;
    IF NOT FOUND OR NOT state_row.active
       OR state_row.policy_revision <> expected_policy_revision
       OR state_row.database_oid <> db_oid
       OR state_row.system_identifier <> system_id THEN
        RAISE EXCEPTION 'role attestation policy is not current';
    END IF;
    SELECT * INTO contract_row FROM eshu_security.role_contracts AS registered
    WHERE registered.contract_id = attest_connection_v1.contract_id
      AND registered.policy_revision = state_row.policy_revision
      AND registered.workload_class = attest_connection_v1.workload_class
      AND registered.connection_route = attest_connection_v1.connection_route
      AND registered.enabled;
    IF NOT FOUND OR contract_row.session_role_oid <> login_oid
       OR contract_row.effective_role_oid <> effective_oid THEN
        RAISE EXCEPTION 'role attestation contract mismatch';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles AS role_state
               WHERE role_state.oid=login_oid
                 AND (role_state.rolsuper OR role_state.rolcreatedb
                      OR role_state.rolcreaterole OR role_state.rolreplication
                      OR role_state.rolbypassrls)) THEN
        RAISE EXCEPTION 'role attestation rejects privileged login';
    END IF;
    IF contract_row.profile <> 'schema-bootstrap'
       AND (pg_catalog.pg_has_role(login_oid,state_row.owner_role_oid,'MEMBER')
            OR pg_catalog.pg_has_role(login_oid,state_row.owner_role_oid,'SET')
            OR pg_catalog.pg_has_role(login_oid,state_row.owner_role_oid,'USAGE')
            OR EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members
                       WHERE member=login_oid)) THEN
        RAISE EXCEPTION 'role attestation rejects owner membership';
    END IF;
    IF contract_row.profile = 'schema-bootstrap'
       AND (effective_oid <> state_row.owner_role_oid
            OR NOT pg_catalog.pg_has_role(login_oid,state_row.owner_role_oid,'SET')
            OR NOT pg_catalog.pg_has_role(effective_oid,'pg_read_all_stats','USAGE')) THEN
        RAISE EXCEPTION 'role attestation bootstrap owner or monitor missing';
    END IF;
    -- Any cross-profile SET or inherited path can bypass the route contract.
    IF EXISTS (
        SELECT 1 FROM eshu_security.role_contracts AS other
        WHERE other.policy_revision=state_row.policy_revision
          AND other.session_role_oid <> login_oid
          AND other.profile <> contract_row.profile
          AND (pg_catalog.pg_has_role(login_oid,other.session_role_oid,'SET')
               OR pg_catalog.pg_has_role(login_oid,other.session_role_oid,'USAGE'))
    ) THEN
        RAISE EXCEPTION 'role attestation rejects cross-profile membership';
    END IF;
    FOR inventory_row IN
        SELECT * FROM eshu_security.role_acl_inventory AS inventory
        WHERE inventory.inventory_version=state_row.inventory_version
          AND inventory.profile=contract_row.profile
        ORDER BY inventory.object_kind,inventory.object_identity,inventory.privilege_name
    LOOP
        inventory_count := inventory_count + 1;
        CASE inventory_row.object_kind
        WHEN 'table' THEN
            permitted := pg_catalog.has_table_privilege(
                effective_oid,inventory_row.object_identity,inventory_row.privilege_name);
            IF NOT inventory_row.required
               AND inventory_row.privilege_name IN ('SELECT','INSERT','UPDATE') THEN
                permitted := permitted OR pg_catalog.has_any_column_privilege(
                    effective_oid,inventory_row.object_identity,
                    inventory_row.privilege_name);
            END IF;
        WHEN 'schema' THEN
            permitted := pg_catalog.has_schema_privilege(
                effective_oid,inventory_row.object_identity,inventory_row.privilege_name);
        WHEN 'sequence' THEN
            permitted := pg_catalog.has_sequence_privilege(
                effective_oid,inventory_row.object_identity,inventory_row.privilege_name);
        WHEN 'function' THEN
            permitted := pg_catalog.has_function_privilege(
                effective_oid,inventory_row.object_identity,inventory_row.privilege_name);
        WHEN 'database' THEN
            permitted := pg_catalog.has_database_privilege(
                effective_oid,CURRENT_DATABASE(),inventory_row.privilege_name);
        WHEN 'table_owner' THEN
            SELECT cls.relowner = state_row.owner_role_oid INTO permitted
            FROM pg_catalog.pg_class AS cls
            WHERE cls.oid=pg_catalog.to_regclass(inventory_row.object_identity);
        ELSE
            RAISE EXCEPTION 'role attestation inventory kind unsupported';
        END CASE;
        IF permitted IS DISTINCT FROM inventory_row.required THEN
            RAISE EXCEPTION 'role attestation ACL mismatch';
        END IF;
    END LOOP;
    IF inventory_count = 0 THEN
        RAISE EXCEPTION 'role attestation inventory missing';
    END IF;
    RETURN QUERY SELECT state_row.policy_revision,contract_row.profile,
                        login_oid,effective_oid,db_oid,
                        state_row.database_identity,state_row.inventory_version;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.attest_connection_v1(
    TEXT,BIGINT,TEXT,TEXT) FROM PUBLIC;
