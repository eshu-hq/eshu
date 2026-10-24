-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: the foreign-key guard. For every growth-class kind and file it admits only rows
-- that carry a payload key an arm of the pre-#8880 predicate compares but no key
-- probe serves for that kind. The contracts carry none of those keys, so it is
-- expected empty; the guard statement repeats this predicate and evaluates the
-- full pre-#8880 predicate over it, which keeps the row set exact by construction.
-- Each insert evaluates one kind comparison per listed kind.
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_supply_chain_impact_foreign_key_guard_idx
    ON fact_records (fact_kind, scope_id, generation_id, fact_id)
    WHERE is_tombstone = FALSE
      AND (
        (fact_kind = 'vulnerability.cve' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'vulnerability.affected_package' AND (payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'vulnerability.affected_product' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'security_alert.repository_alert' AND (payload ? 'purl' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'package_registry.package_version' AND (payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'package_registry.vulnerability_hint' AND (payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'reducer_package_consumption_correlation' AND (payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'repo_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'sbom.component' AND (payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'criteria' OR payload ? 'image_ref'))
        OR (fact_kind = 'reducer_sbom_attestation_attachment' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'image_ref'))
        OR (fact_kind = 'reducer_ci_cd_run_correlation' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'repo_id'))
        OR (fact_kind = 'oci_registry.image_manifest' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'oci_registry.image_index' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'oci_registry.image_tag_observation' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'oci_registry.image_referrer' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'file' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'cve_id' OR payload ? 'advisory_id' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'repository_id' OR payload ? 'scope' OR payload ? 'scope_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'vulnerability.epss_score' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
        OR (fact_kind = 'vulnerability.known_exploited' AND (payload ? 'package_id' OR payload ? 'purl' OR payload ? 'subject_digest' OR payload ? 'digest' OR payload ? 'artifact_digest' OR payload ? 'referrer_digest' OR payload ? 'resolved_digest' OR payload ? 'cpe' OR payload ? 'criteria' OR payload ? 'document_id' OR payload ? 'image_ref'))
      );
