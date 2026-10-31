// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Current readiness applies the same protected target classification for every
// caller. A trusted shared caller can cross tenant boundaries only after the
// scan fact's own current authority is proved.
var listCurrentReadinessQuery = currentScannerReadinessSQL(ListReadinessQuery)

// The scan-result receipt owns exact analysis, warning, and OS-package fact
// membership. The final guard remains closed until the protected completion
// writer and full current source vector are available; a target-scope fact by
// itself cannot make readiness current.
const currentScannerFactPredicate = `
      AND (
        (fact.scope_id NOT LIKE 'image://derived/%'
         AND NOT EXISTS (SELECT 1 FROM scanner_image_targets AS target
                         WHERE target.scan_scope_id = fact.scope_id)
         AND (NOT $26::boolean OR fact.scope_id = ANY($31::text[])))
        OR EXISTS (
          SELECT 1
          FROM scanner_image_scan_fact_associations AS membership
          JOIN scanner_image_scan_results AS receipt
            ON receipt.result_receipt_id = membership.result_receipt_id
           AND receipt.scan_scope_id = membership.scan_scope_id
           AND receipt.scan_generation_id = membership.scan_generation_id
          JOIN scanner_image_targets AS target
            ON target.scan_scope_id = receipt.scan_scope_id
          JOIN scanner_image_target_sources AS binding
            ON binding.binding_id = receipt.binding_id
           AND binding.scan_scope_id = target.scan_scope_id
          JOIN scanner_image_target_attempts AS attempt
            ON attempt.work_item_id = receipt.work_item_id
           AND attempt.binding_id = binding.binding_id
           AND attempt.scan_scope_id = target.scan_scope_id
           AND attempt.status = 'completed'
           AND attempt.claim_id = receipt.claim_id
           AND attempt.claim_fencing_token = receipt.claim_fencing_token
           AND attempt.claim_owner_id = receipt.claim_owner_id
           AND attempt.completed_authority_vector = receipt.completed_authority_vector
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
       AND source_grant.effective_at <= statement_timestamp()
       AND (source_grant.expires_at IS NULL OR source_grant.expires_at > statement_timestamp())
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
       AND scan_scope.active_generation_id = receipt.scan_generation_id
       AND scan_scope.status = 'active'
      JOIN scope_generations AS scan_generation
        ON scan_generation.scope_id = target.scan_scope_id
       AND scan_generation.generation_id = receipt.scan_generation_id
       AND scan_generation.status = 'active'
      WHERE membership.fact_id = fact.fact_id
            AND membership.scan_scope_id = fact.scope_id
            AND membership.scan_generation_id = fact.generation_id
            AND membership.fact_kind = fact.fact_kind
            AND membership.fencing_token = fact.fencing_token
            AND membership.payload_sha256 = encode(sha256(convert_to(fact.payload::text,'UTF8')),'hex')
            AND receipt.scan_scope_id = fact.scope_id
            AND receipt.scan_generation_id = fact.generation_id
            AND target.authorization_namespace = 'hosted'
            AND target.tenant_id <> ''
            AND target.workspace_id <> ''
            AND binding.runtime_digest_kind = 'manifest'
            AND binding.runtime_digest = target.manifest_digest
            AND binding.pinned_source_activation_epoch IS NOT NULL
            AND binding.pinned_source_instance_epoch IS NOT NULL
            AND receipt.completed_authority_vector = jsonb_build_object(
                'source_activation_epoch',binding.pinned_source_activation_epoch,
                'producer_grant_epoch',binding.pinned_producer_grant_epoch,
                'source_instance_epoch',binding.pinned_source_instance_epoch,
                'tenant_epoch',binding.pinned_tenant_epoch,
                'workspace_epoch',binding.pinned_workspace_epoch,
                'scope_grant_epoch',binding.pinned_scope_grant_epoch,
                'registry_epoch',binding.pinned_registry_epoch,
                'role_policy_revision',binding.pinned_role_policy_revision,
                'database_identity',binding.pinned_database_identity)
            AND 1 = (SELECT COUNT(*)
                     FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS candidate(value)
                     WHERE candidate.value->>'authorization_domain' = target.authorization_domain)
            AND EXISTS (
              SELECT 1
              FROM jsonb_array_elements(COALESCE(scanner.configuration->'registry_domains','[]'::jsonb)) AS domain(value)
              WHERE domain.value->>'authorization_domain' = target.authorization_domain
                AND domain.value->>'registry' = binding.registry
                AND encode(sha256(convert_to(domain.value::text,'UTF8')),'hex') = binding.registry_domain_config_sha256)
            AND (NOT $26::boolean OR (
                 target.tenant_id = $27
                 AND target.workspace_id = $28
                 AND target.subject_class = $29
                 AND binding.policy_revision_hash = $30
                 AND binding.source_scope_id = ANY($31::text[])))
            AND FALSE
        )
      )`

// readinessAuthorityLayoutPresent reports whether the protected
// statement can run from db. That needs the global role-policy layout
// AND the 241a claim columns on the schema-visible attempts table:
// isolated test schemas skip every public-only migration, so a database
// whose global eshu_security layout is installed (by another test's
// full bootstrap, e.g. CI shard mates) still has no authority objects
// in the querying schema, and readiness must fall back to the legacy
// statement instead of failing on the missing columns (#9544 shard
// 2/6: column attempt.claim_id does not exist). to_regclass resolves
// through the same search_path as the business query and returns NULL
// instead of erroring on a missing relation, so the probe never aborts
// a wrapping repeatable-read transaction. An existing but empty policy
// table still reports present: with no policy row installed the receipt
// branch cannot fire, which the legacy fact branch already handles.
func readinessAuthorityLayoutPresent(ctx context.Context, database db.Queryer) (bool, error) {
	rows, err := database.QueryContext(ctx, `SELECT to_regclass('eshu_security.role_policy_state') IS NOT NULL
   AND (SELECT count(*) FROM pg_attribute
        WHERE attrelid = to_regclass('scanner_image_target_attempts')
          AND attname IN ('claim_id', 'claim_fencing_token', 'claim_owner_id', 'completed_authority_vector')
          AND NOT attisdropped) = 4`)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, nil
	}
	var present bool
	if err := rows.Scan(&present); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return present, nil
}

func currentScannerReadinessSQL(query string) string {
	for _, marker := range []string{
		"WHERE fact.fact_kind = 'scanner_worker.warning'\n      AND fact.is_tombstone = FALSE\n      AND generation.status = 'active'",
		"WHERE fact.fact_kind = ANY($16::text[])\n      AND fact.is_tombstone = FALSE\n      AND generation.status IN ('active', 'superseded')",
	} {
		if strings.Count(query, marker) != 1 {
			panic("scanner readiness SQL authorization marker changed")
		}
		query = strings.Replace(query, marker, marker+currentScannerFactPredicate, 1)
	}
	return query
}
