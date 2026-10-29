-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9299. Authority-epoch backfill for the scanner typed-source tables.
--
-- Split from 241 (#7961): table-locking DDL (the ancestor triggers on
-- tenants, workspaces, grants, collector instances, and ingestion scopes)
-- must not share a file with bulk DML. 241 installs the DDL; this file
-- seeds scanner_authority_epochs from the current ancestor rows. Every
-- statement is idempotent (ON CONFLICT DO NOTHING), so re-apply changes
-- nothing.
--
-- Public-schema-only (#7033), like 241: the unqualified INSERTs below must
-- land in public. Fail closed here, before any DML, so a missed harness
-- caller errors loudly instead of half-installing.
DO $scanner_241b_public_only$
BEGIN
    IF current_schema() IS DISTINCT FROM 'public' THEN
        RAISE EXCEPTION 'migration 241b applies to the public schema only (current_schema() = %)', current_schema();
    END IF;
END
$scanner_241b_public_only$;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'tenant',(jsonb_build_array(tenant_id))::text,
       status='active' AND tombstoned_at IS NULL FROM tenants
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'workspace',(jsonb_build_array(tenant_id,workspace_id))::text,
       status='active' AND tombstoned_at IS NULL FROM workspaces
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'scope_grant',
       (jsonb_build_array(tenant_id,workspace_id,scope_id,subject_class))::text,
       tombstoned_at IS NULL FROM tenant_scope_grants
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'collector_instance',(jsonb_build_array(instance_id))::text,
       enabled AND claims_enabled FROM collector_instances
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
INSERT INTO scanner_authority_epochs (authority_kind,authority_key,active)
SELECT 'advisory_scope',(jsonb_build_array(scope_id))::text,
       status='active' AND active_generation_id IS NOT NULL FROM ingestion_scopes
ON CONFLICT (authority_kind,authority_key) DO NOTHING;
