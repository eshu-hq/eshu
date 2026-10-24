-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #8880: the supply-chain impact active-evidence load resolves fact IDs once per
-- call from per-branch, index-anchored statements (storage/postgres/facts/impact).
-- This index: the foreign-key guard. For every growth-class kind and file it admits only rows
-- that carry a payload key an arm of the pre-#8880 predicate compares but no key
-- probe serves for that kind. The contracts carry none of those keys, so it is
-- expected empty; the guard statement repeats this predicate and evaluates the
-- full pre-#8880 predicate over it, which keeps the row set exact by construction.
-- The predicate is one jsonb ?| over a CASE on fact_kind, not an OR of per-kind
-- arms, so the planner cannot split it into a BitmapOr over other indexes. Each
-- insert evaluates the CASE once (a NULL key list for unlisted kinds).
-- Generated text: factimpact.NewIndexes; TestNewIndexStatementsMatchMigrations pins
-- it. Keep this the only statement in the file (CONCURRENTLY cannot run inside a
-- multi-statement string, and the runner lifts lock_timeout only for a sole
-- concurrent index statement).
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_supply_chain_impact_foreign_key_guard_idx
    ON fact_records (fact_kind, scope_id, generation_id, fact_id)
    WHERE is_tombstone = FALSE
      AND payload ?| (CASE fact_kind
        WHEN 'vulnerability.cve' THEN ARRAY['package_id', 'purl', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'vulnerability.affected_package' THEN ARRAY['subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'vulnerability.affected_product' THEN ARRAY['package_id', 'purl', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'document_id', 'image_ref']::text[]
        WHEN 'security_alert.repository_alert' THEN ARRAY['purl', 'advisory_id', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'package_registry.package_version' THEN ARRAY['cve_id', 'advisory_id', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'package_registry.vulnerability_hint' THEN ARRAY['purl', 'cve_id', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'reducer_package_consumption_correlation' THEN ARRAY['purl', 'cve_id', 'advisory_id', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'repo_id', 'image_ref']::text[]
        WHEN 'sbom.component' THEN ARRAY['cve_id', 'advisory_id', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'criteria', 'image_ref']::text[]
        WHEN 'reducer_sbom_attestation_attachment' THEN ARRAY['package_id', 'purl', 'cve_id', 'advisory_id', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'image_ref']::text[]
        WHEN 'reducer_ci_cd_run_correlation' THEN ARRAY['package_id', 'purl', 'cve_id', 'advisory_id', 'subject_digest', 'digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'repo_id']::text[]
        WHEN 'oci_registry.image_manifest' THEN ARRAY['package_id', 'purl', 'cve_id', 'advisory_id', 'subject_digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'oci_registry.image_index' THEN ARRAY['package_id', 'purl', 'cve_id', 'advisory_id', 'subject_digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'oci_registry.image_tag_observation' THEN ARRAY['package_id', 'purl', 'cve_id', 'advisory_id', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'oci_registry.image_referrer' THEN ARRAY['package_id', 'purl', 'cve_id', 'advisory_id', 'digest', 'artifact_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'file' THEN ARRAY['package_id', 'purl', 'cve_id', 'advisory_id', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'repository_id', 'scope', 'scope_id', 'image_ref']::text[]
        WHEN 'vulnerability.epss_score' THEN ARRAY['package_id', 'purl', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
        WHEN 'vulnerability.known_exploited' THEN ARRAY['package_id', 'purl', 'subject_digest', 'digest', 'artifact_digest', 'referrer_digest', 'resolved_digest', 'cpe', 'criteria', 'document_id', 'image_ref']::text[]
      END);
