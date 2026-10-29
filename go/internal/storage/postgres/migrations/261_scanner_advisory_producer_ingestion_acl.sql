-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The typed advisory commit uses the ordinary ingestion transaction after its
-- protected preclaim check. These are the exact ordinary table operations in
-- that path, including its unconditional accepted-fact side-key refreshes and
-- the invoker trigger on a newly inserted ingestion scope. No ordinary
-- sequence is used: workflow, fact, and projector IDs are caller supplied;
-- protected epochs are allocated by owner-definer functions and triggers.
WITH required(object_identity,privilege_name) AS (
    VALUES
      ('public.workflow_work_items','SELECT'),
      ('public.workflow_work_items','UPDATE'),
      ('public.workflow_claims','SELECT'),
      ('public.workflow_claims','UPDATE'),
      ('public.tenant_scope_grants','SELECT'),
      ('public.tenants','SELECT'),
      ('public.workspaces','SELECT'),
      ('public.repository_retirements','SELECT'),
      ('public.ingestion_scopes','SELECT'),
      ('public.ingestion_scopes','INSERT'),
      ('public.ingestion_scopes','UPDATE'),
      ('public.scope_generations','SELECT'),
      ('public.scope_generations','INSERT'),
      ('public.scope_generations','UPDATE'),
      ('public.fact_records','SELECT'),
      ('public.fact_records','INSERT'),
      ('public.fact_records','UPDATE'),
      ('public.fact_work_items','SELECT'),
      ('public.fact_work_items','INSERT'),
      ('public.projector_scope_claim_fences','SELECT'),
      ('public.projector_scope_claim_fences','INSERT'),
      ('public.relationship_reference_candidate_keys','DELETE'),
      ('public.relationship_reference_candidate_keys','INSERT'),
      ('public.relationship_reference_candidate_keys','UPDATE'),
      ('public.package_manifest_consumption_keys','DELETE'),
      ('public.package_manifest_consumption_keys','INSERT'),
      ('public.package_manifest_consumption_keys','UPDATE'),
      ('public.package_registry_identity_keys','DELETE'),
      ('public.package_registry_identity_keys','INSERT'),
      ('public.package_registry_identity_keys','UPDATE')
), operation(privilege_name) AS (
    VALUES ('SELECT'),('INSERT'),('UPDATE'),('DELETE'),('TRUNCATE'),('TRIGGER')
), ordinary_table(object_identity) AS (
    SELECT DISTINCT object_identity FROM required
)
INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
SELECT 1,'typed-source-producer','table',ordinary_table.object_identity,
       operation.privilege_name,required.object_identity IS NOT NULL
FROM ordinary_table CROSS JOIN operation
LEFT JOIN required ON required.object_identity=ordinary_table.object_identity
                  AND required.privilege_name=operation.privilege_name
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO UPDATE SET required=EXCLUDED.required;

INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
VALUES (1,'typed-source-producer','schema','public','USAGE',TRUE)
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO UPDATE SET required=EXCLUDED.required;

-- Protected sequence mutation is never part of the producer profile. These
-- negatives catch an accidental direct grant or PUBLIC escalation at connect.
INSERT INTO eshu_security.role_acl_inventory
    (inventory_version,profile,object_kind,object_identity,privilege_name,required)
SELECT 1,'typed-source-producer','sequence',sequence_name,privilege_name,FALSE
FROM (VALUES
    ('public.scanner_authority_epoch_seq'),
    ('public.scanner_running_image_observation_id_seq'),
    ('public.container_image_identity_activation_epoch_seq')
) AS sequence(sequence_name)
CROSS JOIN (VALUES ('USAGE'),('SELECT'),('UPDATE')) AS privilege(privilege_name)
ON CONFLICT (inventory_version,profile,object_kind,object_identity,privilege_name)
DO UPDATE SET required=EXCLUDED.required;
