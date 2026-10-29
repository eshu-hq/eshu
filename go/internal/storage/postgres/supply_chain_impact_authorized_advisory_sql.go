// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const authorizedImpactExpectedPublicAuthoritiesSQL = `
SELECT count(*) FROM scanner_advisory_public_registrations WHERE active`

// The protected receipt and activation prove that this exact generation was
// completed by the registered producer. The integrity checks include the
// source snapshot fact, even though only matcher kinds enter classification.
const authorizedImpactPublicAdvisoriesSQL = `
WITH authorized AS MATERIALIZED (
 SELECT registration.registration_id,registration.collector_instance_id,
        registration.authority_epoch,registration.advisory_scope_id,
        approval.advisory_generation_id,registration.configuration_sha256,
        registration.provider_id,registration.trusted_source_class,
        registration.content_profile,registration.allowed_fact_kinds::text,
        approval.protected_activation_epoch,receipt.receipt_id
 FROM scanner_advisory_public_registrations AS registration
 JOIN scanner_advisory_public_generations AS approval
   ON approval.registration_id=registration.registration_id
  AND approval.approved_configuration_sha256=registration.configuration_sha256
  AND approval.approved_authority_epoch=registration.authority_epoch
  AND approval.active
 JOIN scanner_advisory_generation_receipts AS receipt
   ON receipt.receipt_id=approval.generation_receipt_id
  AND receipt.registration_id=registration.registration_id
  AND receipt.collector_instance_id=registration.collector_instance_id
  AND receipt.advisory_scope_id=registration.advisory_scope_id
  AND receipt.advisory_generation_id=approval.advisory_generation_id
  AND receipt.pinned_registration_epoch=registration.authority_epoch
  AND receipt.pinned_collector_instance_epoch=registration.pinned_collector_instance_epoch
  AND receipt.pinned_configuration_sha256=registration.configuration_sha256
  AND receipt.pinned_max_age_seconds=registration.max_age_seconds
  AND receipt.pinned_role_policy_revision=approval.pinned_role_policy_revision
  AND receipt.pinned_database_identity=approval.pinned_database_identity
  AND receipt.producer_principal_oid=approval.producer_principal_oid
  AND receipt.pinned_producer_grant_epoch=approval.producer_grant_epoch
  AND receipt.fact_set_sha256=approval.fact_set_sha256
  AND approval.approved_max_age_seconds=registration.max_age_seconds
  AND approval.expires_at>clock_timestamp()
  AND receipt.status='published'
 JOIN scanner_advisory_public_activations AS activation
   ON activation.collector_instance_id=registration.collector_instance_id
  AND activation.advisory_scope_id=registration.advisory_scope_id
  AND activation.active_generation_id=approval.advisory_generation_id
  AND activation.activation_epoch=approval.protected_activation_epoch
  AND activation.active
 JOIN scanner_advisory_producer_grants AS producer_grant
   ON producer_grant.principal_oid=receipt.producer_principal_oid
  AND producer_grant.collector_instance_id=registration.collector_instance_id
  AND producer_grant.advisory_scope_id=registration.advisory_scope_id
  AND producer_grant.grant_epoch=receipt.pinned_producer_grant_epoch
  AND producer_grant.active
 JOIN collector_instances AS collector
   ON collector.instance_id=registration.collector_instance_id
  AND collector.collector_kind='vulnerability_intelligence'
  AND collector.enabled AND collector.claims_enabled
  AND encode(sha256(convert_to(collector.configuration::text,'UTF8')),'hex')=registration.configuration_sha256
 JOIN scanner_authority_epochs AS collector_epoch
   ON collector_epoch.authority_kind='collector_instance'
  AND collector_epoch.authority_key=jsonb_build_array(registration.collector_instance_id)::text
  AND collector_epoch.authority_epoch=registration.pinned_collector_instance_epoch
  AND collector_epoch.active
 JOIN ingestion_scopes AS advisory_scope
   ON advisory_scope.scope_id=registration.advisory_scope_id
  AND advisory_scope.active_generation_id=approval.advisory_generation_id
  AND advisory_scope.status='active'
 JOIN scope_generations AS generation
   ON generation.scope_id=registration.advisory_scope_id
  AND generation.generation_id=approval.advisory_generation_id
  AND generation.status='active'
  AND generation.observed_at=approval.approved_observed_at
 JOIN eshu_security.role_policy_state AS role_policy
   ON role_policy.singleton AND role_policy.active
  AND role_policy.policy_revision=approval.pinned_role_policy_revision
  AND role_policy.database_identity=approval.pinned_database_identity
  AND role_policy.database_oid=(SELECT oid FROM pg_database WHERE datname=current_database())
 WHERE registration.active
   AND registration.max_age_seconds BETWEEN 1 AND 2592000
   AND approval.expires_at=approval.approved_observed_at
       + make_interval(secs=>registration.max_age_seconds)
   AND receipt.fact_count=(SELECT count(*) FROM scanner_advisory_generation_facts AS member
                           WHERE member.receipt_id=receipt.receipt_id)
   AND receipt.fact_count=(SELECT count(*) FROM fact_records AS fact
                           WHERE fact.scope_id=receipt.advisory_scope_id
                             AND fact.generation_id=receipt.advisory_generation_id)
   AND NOT EXISTS (
     SELECT 1 FROM scanner_advisory_generation_facts AS member
     LEFT JOIN fact_records AS fact ON fact.fact_id=member.fact_id
     WHERE member.receipt_id=receipt.receipt_id
       AND (fact.fact_id IS NULL
         OR fact.scope_id<>receipt.advisory_scope_id
         OR fact.generation_id<>receipt.advisory_generation_id
         OR fact.fact_kind<>member.fact_kind
         OR fact.collector_kind<>'vulnerability_intelligence'
         OR fact.source_system<>'vulnerability_intelligence'
         OR fact.fencing_token<>receipt.claim_fencing_token
         OR fact.is_tombstone
         OR encode(sha256(convert_to(fact.payload::text,'UTF8')),'hex')<>member.payload_sha256))
)
SELECT * FROM authorized ORDER BY advisory_scope_id,registration_id LIMIT $1`

const authorizedImpactPrivateAdvisoriesExistSQL = `
SELECT EXISTS (
 SELECT 1 FROM scanner_advisory_private_bindings AS binding
 WHERE binding.scan_scope_id=$1 AND binding.active)`

// The membership and payload hash are checked before the row limit. Raw
// scope/generation matches do not supply authority.
const authorizedImpactAdvisoryFactsSQL = `
WITH authorized AS MATERIALIZED (
 SELECT fact.fact_id,fact.scope_id,fact.generation_id,fact.fact_kind,
        fact.stable_fact_key,fact.schema_version,fact.collector_kind,
        fact.fencing_token,fact.source_confidence,fact.source_system,
        fact.source_fact_key,COALESCE(fact.source_uri,''),
        COALESCE(fact.source_record_id,''),fact.observed_at,
        fact.is_tombstone,fact.payload,member.payload_sha256
 FROM scanner_advisory_generation_facts AS member
 JOIN scanner_advisory_generation_receipts AS receipt
   ON receipt.receipt_id=member.receipt_id AND receipt.status='published'
 JOIN fact_records AS fact ON fact.fact_id=member.fact_id
  AND fact.fact_kind=member.fact_kind
  AND fact.fencing_token=receipt.claim_fencing_token
  AND fact.source_system='vulnerability_intelligence'
  AND fact.collector_kind='vulnerability_intelligence'
  AND encode(sha256(convert_to(fact.payload::text,'UTF8')),'hex')=member.payload_sha256
 WHERE member.receipt_id=$1
   AND fact.scope_id=$2 AND fact.generation_id=$3
   AND NOT fact.is_tombstone
   AND $4::jsonb ? fact.fact_kind
)
SELECT * FROM authorized ORDER BY fact_id LIMIT $5`
