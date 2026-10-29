// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// Each protected advisory dependency must remain registered or privately
// bound at its exact revision, scope and generation. Every referenced fact is
// associated with that dependency and checked against its allowed fact kinds.
// The derived candidate remains closed until advisory activation ABA fencing,
// fenced result reuse and caller visibility are complete.
const supplyChainImpactCurrentAdvisoryVectorSQL = `
        AND EXISTS (
          SELECT 1 FROM scanner_image_result_advisory_dependencies AS dependency
          WHERE dependency.publication_id = publication.publication_id
        )
        AND NOT EXISTS (
          SELECT 1
          FROM scanner_image_result_advisory_dependencies AS dependency
          WHERE dependency.publication_id = publication.publication_id
            AND NOT COALESCE((
                (dependency.authority_kind = 'public_registration'
                 AND EXISTS (
                   SELECT 1
                   FROM scanner_advisory_public_registrations AS registration
                   JOIN scanner_advisory_public_generations AS approval
                     ON approval.registration_id = registration.registration_id
                    AND approval.advisory_generation_id = dependency.advisory_generation_id
                    AND approval.approved_configuration_sha256 = registration.configuration_sha256
                    AND approval.approved_authority_epoch = registration.authority_epoch
                    AND approval.pinned_advisory_scope_epoch = dependency.advisory_scope_epoch
                    AND approval.protected_activation_epoch = dependency.advisory_scope_epoch
                    AND approval.active
                   JOIN scanner_advisory_public_activations AS activation
                     ON activation.collector_instance_id = registration.collector_instance_id
                    AND activation.advisory_scope_id = registration.advisory_scope_id
                    AND activation.active_generation_id = approval.advisory_generation_id
                    AND activation.activation_epoch = approval.protected_activation_epoch
                    AND activation.active
                   JOIN scanner_advisory_generation_receipts AS receipt
                     ON receipt.receipt_id = approval.generation_receipt_id
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
                   JOIN scanner_advisory_producer_grants AS producer_grant
                     ON producer_grant.principal_oid = receipt.producer_principal_oid
                    AND producer_grant.collector_instance_id = registration.collector_instance_id
                    AND producer_grant.advisory_scope_id = registration.advisory_scope_id
                    AND producer_grant.grant_epoch = receipt.pinned_producer_grant_epoch
                    AND producer_grant.active
                   JOIN eshu_security.role_policy_state AS role_policy
                     ON role_policy.singleton AND role_policy.active
                    AND role_policy.policy_revision = approval.pinned_role_policy_revision
                    AND role_policy.database_identity = approval.pinned_database_identity
                    AND role_policy.database_oid = (SELECT oid FROM pg_database WHERE datname = current_database())
                   JOIN collector_instances AS advisory_collector
                     ON advisory_collector.instance_id = registration.collector_instance_id
                    AND advisory_collector.collector_kind = 'vulnerability_intelligence'
                    AND advisory_collector.enabled AND advisory_collector.claims_enabled
                   JOIN scanner_authority_epochs AS advisory_collector_epoch
                     ON advisory_collector_epoch.authority_kind = 'collector_instance'
                    AND advisory_collector_epoch.authority_key = (jsonb_build_array(registration.collector_instance_id))::text
                    AND advisory_collector_epoch.authority_epoch = registration.pinned_collector_instance_epoch
                    AND advisory_collector_epoch.active
                   WHERE registration.registration_id = dependency.authority_id
                     AND registration.advisory_scope_id = dependency.advisory_scope_id
                     AND registration.authority_epoch = dependency.authority_epoch
                     AND registration.trusted_source_class = dependency.trusted_source_class
                     AND registration.provider_id = dependency.provider_id
                     AND registration.content_profile = dependency.content_profile
                     AND registration.active
                     AND approval.pinned_collector_instance_epoch = registration.pinned_collector_instance_epoch
                     AND approval.approved_max_age_seconds = registration.max_age_seconds
                     AND receipt.pinned_max_age_seconds = registration.max_age_seconds
                     AND receipt.pinned_role_policy_revision = approval.pinned_role_policy_revision
                     AND receipt.pinned_database_identity = approval.pinned_database_identity
                     AND approval.expires_at > __READ_AT__
                     AND approval.approved_observed_at + make_interval(secs => approval.approved_max_age_seconds) = approval.expires_at
                     AND receipt.fact_count = (SELECT count(*) FROM scanner_advisory_generation_facts AS member
                                               WHERE member.receipt_id = receipt.receipt_id)
                     AND receipt.fact_count = (SELECT count(*) FROM fact_records AS generation_fact
                                               WHERE generation_fact.scope_id = receipt.advisory_scope_id
                                                 AND generation_fact.generation_id = receipt.advisory_generation_id)
                     AND NOT EXISTS (
                       SELECT 1 FROM scanner_advisory_generation_facts AS member
                       LEFT JOIN fact_records AS generation_fact ON generation_fact.fact_id = member.fact_id
                       WHERE member.receipt_id = receipt.receipt_id
                         AND (generation_fact.fact_id IS NULL
                           OR generation_fact.scope_id <> receipt.advisory_scope_id
                           OR generation_fact.generation_id <> receipt.advisory_generation_id
                           OR generation_fact.fact_kind <> member.fact_kind
                           OR generation_fact.fencing_token <> receipt.claim_fencing_token
                           OR generation_fact.is_tombstone
                           OR generation_fact.source_system <> 'vulnerability_intelligence'
                           OR generation_fact.collector_kind <> 'vulnerability_intelligence'
                           OR encode(sha256(convert_to(generation_fact.payload::text,'UTF8')),'hex') <> member.payload_sha256))
                     AND encode(sha256(convert_to(advisory_collector.configuration::text,'UTF8')),'hex') = registration.configuration_sha256
                 ))
                OR (dependency.authority_kind = 'private_binding'
                    AND EXISTS (
                      SELECT 1
                      FROM scanner_advisory_private_bindings AS private_binding
                      JOIN ingestion_scopes AS advisory_scope
                        ON advisory_scope.scope_id = private_binding.advisory_scope_id
                       AND advisory_scope.status = 'active'
                       AND advisory_scope.active_generation_id = private_binding.advisory_generation_id
                      JOIN scope_generations AS advisory_generation
                        ON advisory_generation.scope_id = private_binding.advisory_scope_id
                       AND advisory_generation.generation_id = private_binding.advisory_generation_id
                       AND advisory_generation.status = 'active'
                      JOIN scanner_authority_epochs AS advisory_epoch
                        ON advisory_epoch.authority_kind = 'advisory_scope'
                       AND advisory_epoch.authority_key = (jsonb_build_array(private_binding.advisory_scope_id))::text
                       AND advisory_epoch.authority_epoch = private_binding.pinned_advisory_scope_epoch
                       AND advisory_epoch.active
                      WHERE private_binding.advisory_binding_id = dependency.authority_id
                        AND FALSE -- private advisory approval receipt is not implemented
                        AND private_binding.scan_scope_id = target.scan_scope_id
                        AND private_binding.source_binding_id = binding.binding_id
                        AND private_binding.advisory_scope_id = dependency.advisory_scope_id
                        AND private_binding.advisory_generation_id = dependency.advisory_generation_id
                        AND private_binding.pinned_advisory_scope_epoch = dependency.advisory_scope_epoch
                        AND private_binding.authority_epoch = dependency.authority_epoch
                        AND private_binding.trusted_source_class = dependency.trusted_source_class
                        AND private_binding.provider_id = dependency.provider_id
                        AND private_binding.content_profile = dependency.content_profile
                        AND private_binding.active
                    ))
            ), FALSE)
        )
        AND EXISTS (
          SELECT 1 FROM scanner_image_result_evidence_associations AS evidence_link
          WHERE evidence_link.finding_fact_id = fact.fact_id
            AND evidence_link.publication_id = publication.publication_id
        )
        AND NOT EXISTS (
          SELECT 1
          FROM scanner_image_result_evidence_associations AS evidence_link
          LEFT JOIN fact_records AS evidence
            ON evidence.fact_id = evidence_link.evidence_fact_id
          WHERE evidence_link.finding_fact_id = fact.fact_id
            AND (
              evidence_link.publication_id <> publication.publication_id
              OR evidence_link.scan_scope_id <> target.scan_scope_id
              OR evidence_link.scan_generation_id <> publication.scan_generation_id
              OR evidence.fact_id IS NULL
              OR evidence.is_tombstone
              OR (evidence_link.evidence_class = 'scan_intrinsic'
                  AND (evidence.scope_id <> target.scan_scope_id
                       OR evidence.generation_id <> publication.scan_generation_id
                       OR NOT ((evidence.fact_kind IN ('scanner_worker.analysis', 'scanner_worker.warning')
                                AND evidence.source_system = 'scanner_worker')
                            OR (evidence.fact_kind IN ('vulnerability.os_package', 'vulnerability.warning')
                                AND evidence.source_system = 'scanner_worker'
                                AND evidence.collector_kind = 'os_package_vulnerability'))
                       OR NOT EXISTS (
                           SELECT 1 FROM scanner_image_scan_fact_associations AS scan_fact
                           WHERE scan_fact.fact_id = evidence.fact_id
                             AND scan_fact.result_receipt_id = scan_result.result_receipt_id
                             AND scan_fact.scan_scope_id = evidence.scope_id
                             AND scan_fact.scan_generation_id = evidence.generation_id
                             AND scan_fact.fact_kind = evidence.fact_kind
                             AND scan_fact.fencing_token = evidence.fencing_token
                             AND scan_fact.payload_sha256 = encode(sha256(convert_to(evidence.payload::text,'UTF8')),'hex'))))
              OR (evidence_link.evidence_class = 'advisory'
                  AND NOT EXISTS (
                    SELECT 1
                    FROM scanner_image_result_advisory_dependencies AS dependency
                    WHERE dependency.publication_id = publication.publication_id
                      AND dependency.authority_kind = evidence_link.authority_kind
                      AND dependency.authority_id = evidence_link.authority_id
                      AND dependency.authority_epoch = evidence_link.authority_epoch
                      AND evidence.scope_id = dependency.advisory_scope_id
                      AND evidence.generation_id = dependency.advisory_generation_id
                      AND (
                        (dependency.authority_kind = 'public_registration'
                         AND EXISTS (
                           SELECT 1 FROM scanner_advisory_public_registrations AS registration
                           JOIN scanner_advisory_public_generations AS approval
                             ON approval.registration_id = registration.registration_id
                            AND approval.advisory_generation_id = dependency.advisory_generation_id
                            AND approval.active
                           JOIN scanner_advisory_generation_receipts AS receipt
                             ON receipt.receipt_id = approval.generation_receipt_id
                            AND receipt.status = 'published'
                           JOIN scanner_advisory_generation_facts AS member
                             ON member.receipt_id = receipt.receipt_id
                            AND member.fact_id = evidence.fact_id
                            AND member.fact_kind = evidence.fact_kind
                            AND member.payload_sha256 = encode(sha256(convert_to(evidence.payload::text,'UTF8')),'hex')
                           WHERE registration.registration_id = dependency.authority_id
                             AND registration.allowed_fact_kinds ? evidence.fact_kind
                             AND evidence.fencing_token = receipt.claim_fencing_token
                             AND evidence.source_system = 'vulnerability_intelligence'
                             AND evidence.collector_kind = 'vulnerability_intelligence'
                         ))
                        OR (dependency.authority_kind = 'private_binding'
                            AND EXISTS (
                              SELECT 1 FROM scanner_advisory_private_bindings AS private_binding
                              WHERE private_binding.advisory_binding_id = dependency.authority_id
                                AND private_binding.allowed_fact_kinds ? evidence.fact_kind
                            ))
                      )
                  ))
            )
        )
        AND NOT EXISTS (
          SELECT 1
          FROM jsonb_array_elements_text(
            CASE WHEN jsonb_typeof(fact.payload->'evidence_fact_ids') = 'array'
                 THEN fact.payload->'evidence_fact_ids'
                 ELSE '[]'::jsonb END
          ) AS payload_evidence(fact_id)
          WHERE NOT EXISTS (
            SELECT 1 FROM scanner_image_result_evidence_associations AS evidence_link
            WHERE evidence_link.finding_fact_id = fact.fact_id
              AND evidence_link.publication_id = publication.publication_id
              AND evidence_link.evidence_fact_id = payload_evidence.fact_id
          )
        )
        AND NOT EXISTS (
          SELECT 1 FROM scanner_image_result_evidence_associations AS evidence_link
          WHERE evidence_link.finding_fact_id = fact.fact_id
            AND NOT COALESCE((fact.payload->'evidence_fact_ids' ? evidence_link.evidence_fact_id),FALSE)
        )`
