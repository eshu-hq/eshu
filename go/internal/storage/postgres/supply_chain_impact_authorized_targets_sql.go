// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// authorizedImpactCurrentTargetsCTE checks current hosted source authority
// independently of scan completion. It is shared by the target census and
// receipt query, so a pending scan cannot disappear from a complete-set pass.
const authorizedImpactCurrentTargetsCTE = `
WITH authorized_targets AS MATERIALIZED (
 SELECT target.scan_scope_id,target.tenant_id,target.workspace_id,
        binding.binding_id,
        jsonb_build_object(
          'source_activation_epoch',binding.pinned_source_activation_epoch,
          'producer_grant_epoch',binding.pinned_producer_grant_epoch,
          'source_instance_epoch',binding.pinned_source_instance_epoch,
          'tenant_epoch',binding.pinned_tenant_epoch,
          'workspace_epoch',binding.pinned_workspace_epoch,
          'scope_grant_epoch',binding.pinned_scope_grant_epoch,
          'registry_epoch',binding.pinned_registry_epoch,
          'role_policy_revision',binding.pinned_role_policy_revision,
          'database_identity',binding.pinned_database_identity) AS authority_vector
 FROM scanner_image_targets AS target
 JOIN scanner_image_target_sources AS binding
   ON binding.scan_scope_id=target.scan_scope_id
 JOIN scanner_running_image_observations AS observation
   ON observation.observation_id=binding.canonical_observation_id
  AND observation.producer_instance_id=binding.source_collector_instance_id
  AND observation.source_scope_id=binding.source_scope_id
  AND observation.source_generation_id=binding.source_generation_id
  AND observation.registry=binding.registry
  AND observation.repository=binding.repository
  AND observation.runtime_digest=binding.runtime_digest
  AND observation.runtime_digest_kind=binding.runtime_digest_kind
  AND observation.runtime_image_reference=binding.runtime_image_reference
  AND observation.payload_sha256=binding.source_payload_sha256
 JOIN scanner_running_image_generations AS source_generation
   ON source_generation.producer_instance_id=observation.producer_instance_id
  AND source_generation.source_scope_id=observation.source_scope_id
  AND source_generation.source_generation_id=observation.source_generation_id
  AND source_generation.status='published'
  AND source_generation.producer_class=observation.producer_class
  AND source_generation.observation_class=observation.observation_class
  AND source_generation.principal_oid=binding.pinned_producer_principal_oid
  AND source_generation.producer_grant_epoch=binding.pinned_producer_grant_epoch
  AND source_generation.publication_epoch=binding.pinned_source_activation_epoch
 JOIN scanner_running_image_active_scopes AS activation
   ON activation.producer_instance_id=observation.producer_instance_id
  AND activation.source_scope_id=observation.source_scope_id
  AND activation.published_generation_id=observation.source_generation_id
  AND activation.activation_epoch=binding.pinned_source_activation_epoch
  AND activation.active
 JOIN scanner_running_image_producer_grants AS producer_grant
   ON producer_grant.principal_oid=binding.pinned_producer_principal_oid
  AND producer_grant.producer_instance_id=observation.producer_instance_id
  AND producer_grant.source_scope_id=observation.source_scope_id
  AND producer_grant.producer_class=observation.producer_class
  AND producer_grant.observation_class=observation.observation_class
  AND producer_grant.grant_epoch=binding.pinned_producer_grant_epoch
  AND producer_grant.active
 JOIN collector_instances AS source_instance
   ON source_instance.instance_id=binding.source_collector_instance_id
  AND source_instance.enabled AND source_instance.claims_enabled
 JOIN scanner_authority_epochs AS source_epoch
   ON source_epoch.authority_kind='collector_instance'
  AND source_epoch.authority_key=jsonb_build_array(binding.source_collector_instance_id)::text
  AND source_epoch.authority_epoch=binding.pinned_source_instance_epoch
  AND source_epoch.active
 JOIN collector_instances AS scanner
   ON scanner.instance_id=binding.scanner_collector_instance_id
  AND scanner.collector_kind='scanner_worker'
  AND scanner.enabled AND scanner.claims_enabled
  AND scanner.configuration->>'derive_from_running_images'='true'
 JOIN scanner_authority_epochs AS scanner_epoch
   ON scanner_epoch.authority_kind='collector_instance'
  AND scanner_epoch.authority_key=jsonb_build_array(binding.scanner_collector_instance_id)::text
  AND scanner_epoch.authority_epoch=binding.pinned_registry_epoch
  AND scanner_epoch.active
 JOIN tenants AS tenant
   ON tenant.tenant_id=target.tenant_id
  AND tenant.status='active' AND tenant.tombstoned_at IS NULL
  AND tenant.policy_revision_hash=binding.policy_revision_hash
 JOIN scanner_authority_epochs AS tenant_epoch
   ON tenant_epoch.authority_kind='tenant'
  AND tenant_epoch.authority_key=jsonb_build_array(target.tenant_id)::text
  AND tenant_epoch.authority_epoch=binding.pinned_tenant_epoch
  AND tenant_epoch.active
 JOIN workspaces AS workspace
   ON workspace.tenant_id=target.tenant_id
  AND workspace.workspace_id=target.workspace_id
  AND workspace.status='active' AND workspace.tombstoned_at IS NULL
  AND workspace.policy_revision_hash=binding.policy_revision_hash
 JOIN scanner_authority_epochs AS workspace_epoch
   ON workspace_epoch.authority_kind='workspace'
  AND workspace_epoch.authority_key=jsonb_build_array(target.tenant_id,target.workspace_id)::text
  AND workspace_epoch.authority_epoch=binding.pinned_workspace_epoch
  AND workspace_epoch.active
 JOIN tenant_scope_grants AS source_grant
   ON source_grant.tenant_id=target.tenant_id
  AND source_grant.workspace_id=target.workspace_id
  AND source_grant.scope_id=binding.source_scope_id
  AND source_grant.subject_class=target.subject_class
  AND source_grant.policy_revision_hash=binding.policy_revision_hash
  AND source_grant.tombstoned_at IS NULL
  AND source_grant.effective_at<=transaction_timestamp()
  AND (source_grant.expires_at IS NULL OR source_grant.expires_at>transaction_timestamp())
 JOIN scanner_authority_epochs AS source_grant_epoch
   ON source_grant_epoch.authority_kind='scope_grant'
  AND source_grant_epoch.authority_key=jsonb_build_array(target.tenant_id,target.workspace_id,binding.source_scope_id,target.subject_class)::text
  AND source_grant_epoch.authority_epoch=binding.pinned_scope_grant_epoch
  AND source_grant_epoch.active
 JOIN eshu_security.role_policy_state AS policy
   ON policy.singleton AND policy.active
  AND policy.policy_revision=binding.pinned_role_policy_revision
  AND policy.database_identity=binding.pinned_database_identity
  AND policy.database_oid=(SELECT oid FROM pg_database WHERE datname=current_database())
 WHERE target.authorization_namespace='hosted'
   AND binding.runtime_digest_kind IN ('manifest','config')
   AND ((binding.runtime_digest_kind='manifest'
         AND binding.runtime_digest=target.manifest_digest
         AND binding.manifest_verification_id IS NULL)
     OR (binding.runtime_digest_kind='config'
         AND EXISTS (
           SELECT 1 FROM scanner_running_image_manifest_verifications AS verified
           WHERE verified.verification_id=binding.manifest_verification_id
             AND verified.observation_id=observation.observation_id
             AND verified.manifest_digest=target.manifest_digest
             AND verified.config_digest=binding.runtime_digest
             AND verified.registry=binding.registry
             AND verified.repository=binding.repository
             AND verified.verification_epoch=binding.pinned_manifest_verification_epoch
             AND 1=(SELECT count(*) FROM scanner_running_image_manifest_verifications AS peer
                    WHERE peer.observation_id=observation.observation_id))))
   AND 1=(SELECT count(*) FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
          WHERE domain.value->>'authorization_domain'=target.authorization_domain)
   AND EXISTS (
     SELECT 1 FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
     WHERE domain.value->>'authorization_domain'=target.authorization_domain
       AND domain.value->>'registry'=binding.registry
       AND encode(sha256(convert_to(domain.value::text,'UTF8')),'hex')=binding.registry_domain_config_sha256)
)`

// authorizedImpactCurrentTargetsSQL enumerates every current authorized
// target before a cardinality bound, including targets awaiting scan results.
const authorizedImpactCurrentTargetsSQL = authorizedImpactCurrentTargetsCTE + `
SELECT DISTINCT scan_scope_id FROM authorized_targets
ORDER BY scan_scope_id LIMIT $1`

// authorizedImpactTargetReceiptsSQL requires an exact completed protected scan
// receipt for a currently authorized target and binding. The caller compares
// its result to the independent current-target census before publishing.
const authorizedImpactTargetReceiptsSQL = authorizedImpactCurrentTargetsCTE + `,
authorized AS MATERIALIZED (
 SELECT target.scan_scope_id,result.scan_generation_id,
        result.result_receipt_id,result.binding_id,result.work_item_id,
        result.claim_id,result.claim_fencing_token,result.claim_owner_id,
        target.tenant_id,target.workspace_id
 FROM authorized_targets AS target
 JOIN scanner_image_scan_results AS result
   ON result.scan_scope_id=target.scan_scope_id
  AND result.binding_id=target.binding_id
 JOIN scanner_image_target_attempts AS attempt
   ON attempt.work_item_id=result.work_item_id
  AND attempt.scan_scope_id=result.scan_scope_id
  AND attempt.binding_id=result.binding_id
  AND attempt.status='completed'
  AND attempt.claim_id=result.claim_id
  AND attempt.claim_fencing_token=result.claim_fencing_token
  AND attempt.claim_owner_id=result.claim_owner_id
  AND attempt.completed_authority_vector=result.completed_authority_vector
  AND attempt.completed_authority_vector=target.authority_vector
 JOIN ingestion_scopes AS scan_scope
   ON scan_scope.scope_id=target.scan_scope_id
  AND scan_scope.active_generation_id=result.scan_generation_id
  AND scan_scope.status='active'
 JOIN scope_generations AS scan_generation
   ON scan_generation.scope_id=target.scan_scope_id
  AND scan_generation.generation_id=result.scan_generation_id
  AND scan_generation.status='active'
)
SELECT scan_scope_id,scan_generation_id,result_receipt_id,binding_id,
       work_item_id,claim_id,claim_fencing_token,claim_owner_id,tenant_id,workspace_id
FROM authorized ORDER BY scan_scope_id LIMIT $1`
