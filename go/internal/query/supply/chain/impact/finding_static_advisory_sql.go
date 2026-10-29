// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import "strings"

// A protected static revision is authoritative only at the fact's current
// impact fence and payload. A zero-dependency revision is independently static;
// advisory-dependent revisions require every pinned receipt and member to
// remain current at the read snapshot.
const supplyChainImpactCurrentStaticAuthorityTemplateSQL = `EXISTS (
  SELECT 1
  FROM scanner_static_impact_finding_authority_revisions AS static_revision
  JOIN eshu_security.role_policy_state AS static_role
    ON static_role.singleton AND static_role.active
   AND static_role.policy_revision = static_revision.pinned_role_policy_revision
   AND static_role.database_identity = static_revision.pinned_database_identity
   AND static_role.database_oid = (SELECT oid FROM pg_database WHERE datname = current_database())
  WHERE static_revision.finding_fact_id = fact.fact_id
    AND static_revision.impact_scope_id = fact.scope_id
    AND static_revision.impact_generation_id = fact.generation_id
    AND static_revision.impact_fencing_token = fact.fencing_token
    AND static_revision.finding_payload_sha256 = encode(sha256(convert_to(fact.payload::text,'UTF8')),'hex')
    AND static_revision.advisory_dependency_count = (
      SELECT count(*) FROM scanner_static_impact_public_advisory_evidence AS static_child
      WHERE static_child.finding_fact_id = static_revision.finding_fact_id
        AND static_child.impact_fencing_token = static_revision.impact_fencing_token)
    AND NOT EXISTS (
      SELECT 1
      FROM scanner_static_impact_public_advisory_evidence AS static_child
      WHERE static_child.finding_fact_id = static_revision.finding_fact_id
        AND static_child.impact_fencing_token = static_revision.impact_fencing_token
        AND NOT COALESCE((
          EXISTS (
            SELECT 1
            FROM scanner_advisory_public_registrations AS registration
            JOIN scanner_advisory_public_generations AS approval
              ON approval.registration_id = registration.registration_id
             AND approval.active
             AND approval.approved_configuration_sha256 = registration.configuration_sha256
             AND approval.approved_authority_epoch = registration.authority_epoch
             AND approval.protected_activation_epoch = static_child.activation_epoch
             AND approval.pinned_advisory_scope_epoch = static_child.activation_epoch
            JOIN scanner_advisory_public_activations AS activation
              ON activation.collector_instance_id = registration.collector_instance_id
             AND activation.advisory_scope_id = registration.advisory_scope_id
             AND activation.active_generation_id = approval.advisory_generation_id
             AND activation.activation_epoch = approval.protected_activation_epoch
             AND activation.active
            JOIN scanner_advisory_generation_receipts AS receipt
              ON receipt.receipt_id = static_child.generation_receipt_id
             AND receipt.receipt_id = approval.generation_receipt_id
             AND receipt.registration_id = registration.registration_id
             AND receipt.collector_instance_id = registration.collector_instance_id
             AND receipt.advisory_scope_id = registration.advisory_scope_id
             AND receipt.advisory_generation_id = approval.advisory_generation_id
             AND receipt.pinned_registration_epoch = registration.authority_epoch
             AND receipt.pinned_collector_instance_epoch = registration.pinned_collector_instance_epoch
             AND receipt.pinned_configuration_sha256 = registration.configuration_sha256
             AND receipt.producer_principal_oid = approval.producer_principal_oid
             AND receipt.pinned_producer_grant_epoch = approval.producer_grant_epoch
             AND receipt.fact_set_sha256 = approval.fact_set_sha256
             AND receipt.status = 'published'
            JOIN scanner_advisory_generation_facts AS member
              ON member.receipt_id = receipt.receipt_id
             AND member.fact_id = static_child.evidence_fact_id
            JOIN fact_records AS evidence
              ON evidence.fact_id = member.fact_id
             AND evidence.scope_id = receipt.advisory_scope_id
             AND evidence.generation_id = receipt.advisory_generation_id
             AND evidence.fact_kind = member.fact_kind
             AND evidence.fencing_token = receipt.claim_fencing_token
             AND NOT evidence.is_tombstone
             AND evidence.source_system = 'vulnerability_intelligence'
             AND evidence.collector_kind = 'vulnerability_intelligence'
             AND encode(sha256(convert_to(evidence.payload::text,'UTF8')),'hex') = member.payload_sha256
             AND member.payload_sha256 = static_child.evidence_payload_sha256
            JOIN scanner_advisory_producer_grants AS producer_grant
              ON producer_grant.principal_oid = receipt.producer_principal_oid
             AND producer_grant.collector_instance_id = registration.collector_instance_id
             AND producer_grant.advisory_scope_id = registration.advisory_scope_id
             AND producer_grant.grant_epoch = receipt.pinned_producer_grant_epoch
             AND producer_grant.active
            JOIN collector_instances AS advisory_collector
              ON advisory_collector.instance_id = registration.collector_instance_id
             AND advisory_collector.collector_kind = 'vulnerability_intelligence'
             AND advisory_collector.enabled AND advisory_collector.claims_enabled
             AND encode(sha256(convert_to(advisory_collector.configuration::text,'UTF8')),'hex') = registration.configuration_sha256
            JOIN scanner_authority_epochs AS collector_epoch
              ON collector_epoch.authority_kind = 'collector_instance'
             AND collector_epoch.authority_key = jsonb_build_array(registration.collector_instance_id)::text
             AND collector_epoch.authority_epoch = registration.pinned_collector_instance_epoch
             AND collector_epoch.active
            WHERE registration.registration_id = static_child.registration_id
              AND registration.authority_epoch = static_child.registration_epoch
              AND registration.active
              AND registration.content_profile = 'supply_chain_impact_v1'
              AND registration.allowed_fact_kinds ? evidence.fact_kind
              AND approval.pinned_collector_instance_epoch = registration.pinned_collector_instance_epoch
              AND approval.approved_max_age_seconds = registration.max_age_seconds
              AND receipt.pinned_max_age_seconds = registration.max_age_seconds
              AND approval.expires_at > __READ_AT__
              AND approval.approved_observed_at + make_interval(secs => approval.approved_max_age_seconds) = approval.expires_at
              AND approval.pinned_role_policy_revision = static_revision.pinned_role_policy_revision
              AND approval.pinned_database_identity = static_revision.pinned_database_identity
              AND receipt.pinned_role_policy_revision = static_revision.pinned_role_policy_revision
              AND receipt.pinned_database_identity = static_revision.pinned_database_identity
              AND fact.payload->'evidence_fact_ids' ? static_child.evidence_fact_id
          )
        ), FALSE))
    AND NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements_text(
        CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array'
             THEN fact.payload->'evidence_fact_ids' ELSE '[]'::jsonb END
      ) AS payload_evidence(fact_id)
      JOIN fact_records AS advisory_evidence ON advisory_evidence.fact_id = payload_evidence.fact_id
      WHERE advisory_evidence.fact_kind IN (
        'vulnerability.cve','vulnerability.affected_package','vulnerability.affected_product',
        'vulnerability.epss_score','vulnerability.known_exploited')
        AND NOT EXISTS (
          SELECT 1 FROM scanner_static_impact_public_advisory_evidence AS static_child
          WHERE static_child.finding_fact_id = static_revision.finding_fact_id
            AND static_child.impact_fencing_token = static_revision.impact_fencing_token
            AND static_child.evidence_fact_id = advisory_evidence.fact_id))
)`

func supplyChainImpactCurrentStaticAuthoritySQL(readAt string) string {
	return strings.ReplaceAll(supplyChainImpactCurrentStaticAuthorityTemplateSQL,
		"__READ_AT__", readAt+"::timestamptz")
}
