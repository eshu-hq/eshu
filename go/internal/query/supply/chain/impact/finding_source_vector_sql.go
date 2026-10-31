// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// This relation proves the settled portion of a publication's current source
// vector. The caller must also prove advisory dependencies, exact publication
// or fenced reuse eligibility, and caller visibility before admitting a
// derived candidate. Until those checks are wired, the derived branch in
// finding_authority_sql.go remains closed.
const supplyChainImpactCurrentSourceVectorTemplateSQL = `
    EXISTS (
      SELECT 1
      FROM scanner_image_result_publications AS publication
      JOIN scanner_image_targets AS target
        ON target.scan_scope_id = publication.scan_scope_id
      JOIN LATERAL (
        SELECT original_binding.*, NULL::text AS reuse_receipt_id
        FROM scanner_image_target_sources AS original_binding
        WHERE original_binding.binding_id = publication.binding_id
          AND publication.status = 'current'
        UNION ALL
        SELECT new_binding.*, receipt.reuse_receipt_id
        FROM scanner_image_result_reuse_receipts AS receipt
        JOIN scanner_image_target_sources AS new_binding
          ON new_binding.binding_id = receipt.new_binding_id
        JOIN scanner_image_target_attempts AS new_attempt
          ON new_attempt.work_item_id = receipt.new_work_item_id
         AND new_attempt.binding_id = new_binding.binding_id
         AND new_attempt.scan_scope_id = publication.scan_scope_id
         AND new_attempt.status = 'completed'
         AND new_attempt.claim_id = receipt.new_claim_id
         AND new_attempt.claim_fencing_token = receipt.new_claim_fencing_token
         AND new_attempt.claim_owner_id = receipt.new_claim_owner_id
         AND new_attempt.completed_authority_vector = receipt.authority_vector
        WHERE receipt.publication_id = publication.publication_id
          AND publication.status IN ('current','revoked')
          AND new_binding.binding_id <> publication.binding_id
          AND new_binding.scan_scope_id = publication.scan_scope_id
          AND receipt.result_set_sha256 = publication.result_set_sha256
          AND receipt.advisory_set_sha256 = publication.advisory_set_sha256
          AND receipt.analysis_version = publication.analysis_version
          AND receipt.authority_vector = jsonb_build_object(
            'source_activation_epoch',new_binding.pinned_source_activation_epoch,
            'producer_grant_epoch',new_binding.pinned_producer_grant_epoch,
            'source_instance_epoch',new_binding.pinned_source_instance_epoch,
            'tenant_epoch',new_binding.pinned_tenant_epoch,
            'workspace_epoch',new_binding.pinned_workspace_epoch,
            'scope_grant_epoch',new_binding.pinned_scope_grant_epoch,
            'registry_epoch',new_binding.pinned_registry_epoch,
            'role_policy_revision',new_binding.pinned_role_policy_revision,
            'database_identity',new_binding.pinned_database_identity)
      ) AS binding ON binding.scan_scope_id = target.scan_scope_id
      JOIN scanner_image_target_attempts AS attempt
        ON attempt.work_item_id = publication.work_item_id
       AND attempt.scan_scope_id = target.scan_scope_id
       AND attempt.binding_id = publication.binding_id
       AND attempt.status = 'completed'
      JOIN scanner_image_scan_results AS scan_result
        ON scan_result.result_receipt_id = publication.scan_result_receipt_id
       AND scan_result.work_item_id = publication.work_item_id
       AND scan_result.binding_id = publication.binding_id
       AND scan_result.scan_scope_id = publication.scan_scope_id
       AND scan_result.scan_generation_id = publication.scan_generation_id
       AND scan_result.claim_id = attempt.claim_id
       AND scan_result.claim_fencing_token = attempt.claim_fencing_token
       AND scan_result.claim_owner_id = attempt.claim_owner_id
       AND scan_result.completed_authority_vector = attempt.completed_authority_vector
      JOIN scanner_running_image_observations AS observation
        ON observation.observation_id = binding.canonical_observation_id
       AND observation.producer_instance_id = binding.source_collector_instance_id
       AND observation.source_scope_id = binding.source_scope_id
       AND observation.source_generation_id = binding.source_generation_id
       AND observation.registry = binding.registry
       AND observation.repository = binding.repository
       AND observation.runtime_digest = binding.runtime_digest
       AND observation.runtime_digest_kind = binding.runtime_digest_kind
       AND observation.runtime_image_reference = binding.runtime_image_reference
      JOIN scanner_running_image_generations AS source_generation
        ON source_generation.producer_instance_id = observation.producer_instance_id
       AND source_generation.source_scope_id = observation.source_scope_id
       AND source_generation.source_generation_id = observation.source_generation_id
       AND source_generation.status = 'published'
       AND source_generation.observation_class = observation.observation_class
       AND source_generation.producer_grant_epoch = binding.pinned_producer_grant_epoch
       AND source_generation.publication_epoch = binding.pinned_source_activation_epoch
      JOIN scanner_running_image_active_scopes AS source_activation
        ON source_activation.producer_instance_id = observation.producer_instance_id
       AND source_activation.source_scope_id = observation.source_scope_id
       AND source_activation.published_generation_id = observation.source_generation_id
       AND source_activation.activation_epoch = binding.pinned_source_activation_epoch
       AND source_activation.active
      JOIN scanner_running_image_producer_grants AS producer_grant
        ON producer_grant.principal_oid = source_generation.principal_oid
       AND producer_grant.producer_instance_id = observation.producer_instance_id
       AND producer_grant.source_scope_id = observation.source_scope_id
       AND producer_grant.producer_class = observation.producer_class
       AND producer_grant.observation_class = observation.observation_class
       AND producer_grant.grant_epoch = binding.pinned_producer_grant_epoch
       AND producer_grant.active
      JOIN scanner_authority_epochs AS source_instance_epoch
        ON source_instance_epoch.authority_kind = 'collector_instance'
       AND source_instance_epoch.authority_key = (jsonb_build_array(binding.source_collector_instance_id))::text
       AND source_instance_epoch.authority_epoch = binding.pinned_source_instance_epoch
       AND source_instance_epoch.active
      JOIN collector_instances AS source_instance
        ON source_instance.instance_id = binding.source_collector_instance_id
       AND source_instance.enabled AND source_instance.claims_enabled
      JOIN tenants AS tenant
        ON tenant.tenant_id = target.tenant_id
       AND tenant.status = 'active' AND tenant.tombstoned_at IS NULL
       AND tenant.policy_revision_hash = binding.policy_revision_hash
      JOIN scanner_authority_epochs AS tenant_epoch
        ON tenant_epoch.authority_kind = 'tenant'
       AND tenant_epoch.authority_key = (jsonb_build_array(target.tenant_id))::text
       AND tenant_epoch.authority_epoch = binding.pinned_tenant_epoch
       AND tenant_epoch.active
      JOIN workspaces AS workspace
        ON workspace.tenant_id = target.tenant_id
       AND workspace.workspace_id = target.workspace_id
       AND workspace.status = 'active' AND workspace.tombstoned_at IS NULL
       AND workspace.policy_revision_hash = binding.policy_revision_hash
      JOIN scanner_authority_epochs AS workspace_epoch
        ON workspace_epoch.authority_kind = 'workspace'
       AND workspace_epoch.authority_key = (jsonb_build_array(target.tenant_id,target.workspace_id))::text
       AND workspace_epoch.authority_epoch = binding.pinned_workspace_epoch
       AND workspace_epoch.active
      JOIN tenant_scope_grants AS source_grant
        ON source_grant.tenant_id = target.tenant_id
       AND source_grant.workspace_id = target.workspace_id
       AND source_grant.scope_id = binding.source_scope_id
       AND source_grant.subject_class = target.subject_class
       AND source_grant.policy_revision_hash = binding.policy_revision_hash
       AND source_grant.tombstoned_at IS NULL
       AND source_grant.effective_at <= __READ_AT__
       AND (source_grant.expires_at IS NULL OR source_grant.expires_at > __READ_AT__)
      JOIN scanner_authority_epochs AS source_grant_epoch
        ON source_grant_epoch.authority_kind = 'scope_grant'
       AND source_grant_epoch.authority_key = (jsonb_build_array(
             target.tenant_id,target.workspace_id,binding.source_scope_id,target.subject_class))::text
       AND source_grant_epoch.authority_epoch = binding.pinned_scope_grant_epoch
       AND source_grant_epoch.active
      JOIN collector_instances AS scanner
        ON scanner.instance_id = binding.scanner_collector_instance_id
       AND scanner.collector_kind = 'scanner_worker'
       AND scanner.enabled AND scanner.claims_enabled
       AND scanner.configuration->>'derive_from_running_images' = 'true'
      JOIN scanner_authority_epochs AS registry_epoch
        ON registry_epoch.authority_kind = 'collector_instance'
       AND registry_epoch.authority_key = (jsonb_build_array(binding.scanner_collector_instance_id))::text
       AND registry_epoch.authority_epoch = binding.pinned_registry_epoch
       AND registry_epoch.active
      JOIN eshu_security.role_policy_state AS role_policy
        ON role_policy.singleton AND role_policy.active
       AND role_policy.policy_revision = binding.pinned_role_policy_revision
       AND role_policy.database_identity = binding.pinned_database_identity
       AND role_policy.database_oid = (SELECT oid FROM pg_database WHERE datname = current_database())
      JOIN ingestion_scopes AS scan_scope
        ON scan_scope.scope_id = target.scan_scope_id
       AND scan_scope.active_generation_id = publication.scan_generation_id
       AND scan_scope.status = 'active'
      JOIN scope_generations AS scan_generation
        ON scan_generation.scope_id = target.scan_scope_id
       AND scan_generation.generation_id = publication.scan_generation_id
       AND scan_generation.status = 'active'
      WHERE publication.publication_id = finding_association.publication_id
        AND publication.scan_scope_id = finding_association.scan_scope_id
        AND publication.scan_generation_id = finding_association.scan_generation_id
        AND publication.status IN ('current','revoked')
        AND (binding.reuse_receipt_id IS NOT NULL
             OR publication.role_policy_revision = role_policy.policy_revision)
        AND publication.database_identity = role_policy.database_identity
        AND target.scan_scope_id = finding_association.scan_scope_id
        AND (binding.reuse_receipt_id IS NOT NULL
             OR attempt.completed_authority_vector = jsonb_build_object(
            'source_activation_epoch',binding.pinned_source_activation_epoch,
            'producer_grant_epoch',binding.pinned_producer_grant_epoch,
            'source_instance_epoch',binding.pinned_source_instance_epoch,
            'tenant_epoch',binding.pinned_tenant_epoch,
            'workspace_epoch',binding.pinned_workspace_epoch,
            'scope_grant_epoch',binding.pinned_scope_grant_epoch,
            'registry_epoch',binding.pinned_registry_epoch,
            'role_policy_revision',binding.pinned_role_policy_revision,
            'database_identity',binding.pinned_database_identity))
        AND target.authorization_namespace = 'hosted'
        AND target.tenant_id = finding_association.tenant_id
        AND target.workspace_id = finding_association.workspace_id
__CALLER_VISIBILITY__
        AND binding.runtime_digest_kind = 'manifest'
        AND binding.runtime_digest = target.manifest_digest
        AND binding.pinned_source_activation_epoch IS NOT NULL
        AND binding.pinned_source_instance_epoch IS NOT NULL
        AND 1 = (SELECT COUNT(*)
                 FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS candidate(value)
                 WHERE candidate.value->>'authorization_domain' = target.authorization_domain)
        AND EXISTS (
          SELECT 1
          FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
          WHERE domain.value->>'authorization_domain' = target.authorization_domain
            AND domain.value->>'registry' = binding.registry
            AND encode(sha256(convert_to(domain.value::text,'UTF8')),'hex') = binding.registry_domain_config_sha256
        )
` + supplyChainImpactCurrentAdvisoryVectorSQL + `
    )`

func supplyChainImpactCurrentSourceVectorForCallerSQL(
	scoped, tenant, workspace, subjectClass, policy, allowedScopes, filterScopes, readAt string,
) string {
	return strings.ReplaceAll(strings.Replace(
		supplyChainImpactCurrentSourceVectorTemplateSQL,
		"__CALLER_VISIBILITY__",
		supplyChainImpactCallerVisibilitySQL(
			scoped, tenant, workspace, subjectClass, policy, allowedScopes, filterScopes,
		),
		1,
	), "__READ_AT__", readAt+"::timestamptz")
}
