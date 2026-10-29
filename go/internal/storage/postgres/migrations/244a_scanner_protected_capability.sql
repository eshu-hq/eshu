-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- This owner-only helper is called from fixed-capability protected entrypoints.
-- It cannot be called directly by runtime/control/producer principals. SHARE
-- locks remain held through the caller's transaction, fencing policy rotation.
CREATE OR REPLACE FUNCTION eshu_security.assert_protected_capability_v1(
    expected_policy_revision BIGINT, expected_database_identity UUID,
    required_profile TEXT)
RETURNS OID LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    state_row eshu_security.role_policy_state%ROWTYPE;
    contract_row eshu_security.role_contracts%ROWTYPE;
    inventory_row eshu_security.role_acl_inventory%ROWTYPE;
    login_oid OID;
    db_oid OID;
    system_id TEXT;
    permitted BOOLEAN;
    inventory_count INTEGER := 0;
BEGIN
    SELECT oid INTO login_oid FROM pg_catalog.pg_roles WHERE rolname=SESSION_USER;
    SELECT oid INTO db_oid FROM pg_catalog.pg_database
    WHERE datname=CURRENT_DATABASE();
    SELECT control.system_identifier::text INTO system_id
    FROM pg_catalog.pg_control_system() AS control;
    IF login_oid IS NULL OR db_oid IS NULL OR system_id IS NULL THEN
        RAISE EXCEPTION 'protected capability session identity unavailable';
    END IF;
    SELECT * INTO state_row FROM eshu_security.role_policy_state
    WHERE singleton FOR SHARE;
    IF NOT FOUND OR NOT state_row.active
       OR state_row.policy_revision <> expected_policy_revision
       OR state_row.database_identity <> expected_database_identity
       OR state_row.database_oid <> db_oid
       OR state_row.system_identifier <> system_id THEN
        RAISE EXCEPTION 'protected capability policy is not current';
    END IF;
    SELECT * INTO contract_row FROM eshu_security.role_contracts AS registered
    WHERE registered.policy_revision=state_row.policy_revision
      AND registered.session_role_oid=login_oid
      AND registered.effective_role_oid=login_oid
      AND registered.profile=required_profile
      AND registered.enabled
    ORDER BY registered.contract_id
    LIMIT 1 FOR SHARE;
    IF NOT FOUND OR required_profile NOT IN
       ('runtime-writer','authority-control','typed-source-producer') THEN
        RAISE EXCEPTION 'protected capability principal denied';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles AS role_state
               WHERE role_state.oid=login_oid
                 AND (role_state.rolsuper OR role_state.rolcreatedb
                      OR role_state.rolcreaterole OR role_state.rolreplication
                      OR role_state.rolbypassrls))
       OR pg_catalog.pg_has_role(login_oid,state_row.owner_role_oid,'MEMBER')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members
                  WHERE member=login_oid) THEN
        RAISE EXCEPTION 'protected capability principal can escalate';
    END IF;
    FOR inventory_row IN
        SELECT * FROM eshu_security.role_acl_inventory AS inventory
        WHERE inventory.inventory_version=state_row.inventory_version
          AND inventory.profile=contract_row.profile
        ORDER BY inventory.object_kind,inventory.object_identity,
                 inventory.privilege_name
    LOOP
        inventory_count := inventory_count + 1;
        CASE inventory_row.object_kind
        WHEN 'table' THEN
            permitted := pg_catalog.has_table_privilege(
                login_oid,inventory_row.object_identity,inventory_row.privilege_name);
            IF NOT inventory_row.required
               AND inventory_row.privilege_name IN ('SELECT','INSERT','UPDATE') THEN
                permitted := permitted OR pg_catalog.has_any_column_privilege(
                    login_oid,inventory_row.object_identity,
                    inventory_row.privilege_name);
            END IF;
        WHEN 'schema' THEN
            permitted := pg_catalog.has_schema_privilege(
                login_oid,inventory_row.object_identity,inventory_row.privilege_name);
        WHEN 'sequence' THEN
            permitted := pg_catalog.has_sequence_privilege(
                login_oid,inventory_row.object_identity,inventory_row.privilege_name);
        WHEN 'function' THEN
            permitted := pg_catalog.has_function_privilege(
                login_oid,inventory_row.object_identity,inventory_row.privilege_name);
        WHEN 'database' THEN
            permitted := pg_catalog.has_database_privilege(
                login_oid,CURRENT_DATABASE(),inventory_row.privilege_name);
        WHEN 'table_owner' THEN
            SELECT cls.relowner=state_row.owner_role_oid INTO permitted
            FROM pg_catalog.pg_class AS cls
            WHERE cls.oid=pg_catalog.to_regclass(inventory_row.object_identity);
        ELSE
            RAISE EXCEPTION 'protected capability inventory kind unsupported';
        END CASE;
        IF permitted IS DISTINCT FROM inventory_row.required THEN
            RAISE EXCEPTION 'protected capability ACL mismatch';
        END IF;
    END LOOP;
    IF inventory_count=0 THEN
        RAISE EXCEPTION 'protected capability inventory missing';
    END IF;
    RETURN login_oid;
END;
$$;
REVOKE ALL ON FUNCTION eshu_security.assert_protected_capability_v1(
    BIGINT,UUID,TEXT) FROM PUBLIC;
