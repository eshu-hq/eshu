-- examples/supply-chain-demo/scripts/seed-image-identity-facts.sql
--
-- Tier-1 SQL seed for the supply-chain demo OCI image-IDENTITY proof
-- (issue #3061). It writes the durable raw collector facts that the three
-- reducer domains
--
--     DomainContainerImageIdentity   (reducer_container_image_identity)
--     DomainSBOMAttestationAttachment (reducer_sbom_attestation_attachment)
--     DomainSupplyChainImpact         (reducer_supply_chain_impact_finding)
--
-- join into one impact finding that carries image_ref — the sub-hop the live
-- run-full-chain-proof.sh deliberately skips. Modeled on the tier-1 pattern in
-- tests/fixtures/tfstate_drift/seed.sql: write the exact rows the production
-- loaders already SELECT, rather than running registry / OSV collectors.
--
-- WHAT IS SEEDED (raw collector facts) vs WHAT IS COLLECTOR-DERIVED at runtime:
--
--   Seeded here (no real registry, no real OSV, everything synthetic/public):
--     * oci_registry.image_manifest  — registry-observed image digest identity
--     * sbom.document + sbom.component — CycloneDX SBOM body
--     * attestation.statement        — in-toto/CycloneDX subject binding
--     * vulnerability.cve + vulnerability.affected_package — synthetic advisory
--
--   Derived at runtime by the live reducers (NOT seeded), from the above plus
--   the K8s Deployment manifest in the corpus that references the same image:
--     * reducer_container_image_identity (exact_digest, image_ref populated)
--     * reducer_sbom_attestation_attachment (subject digest matched)
--     * reducer_supply_chain_impact_finding (image_ref on the finding)
--
-- The K8s manifest -> image identity tie is what classifies the digest as
-- exact_digest; that manifest lives in the proof script's corpus, not here.
--
-- Idempotent: every INSERT carries ON CONFLICT DO NOTHING. Safe to re-run.
--
-- Nothing here is real or secret:
--   * registry demo.invalid is RFC 6761 reserved; it resolves nowhere.
--   * sha256:1111…1111 is an obviously-synthetic placeholder digest.
--   * CVE-2026-SYNTHETIC-NPM / GHSA-synthetic-npm-0001 exist on no feed.
--   * synthetic-vulnerable-npm is the demo's own non-registry package.
-- The literals match examples/supply-chain-demo/sbom/app.cdx.json and the
-- synthetic identifiers in go/cmd/eshu/vuln_scan_supply_chain_demo_test.go.

BEGIN;

-- ============================================================================
-- 1. ingestion_scopes — one scope per source system feeding the chain. Columns
--    match schema/data-plane/postgres/001_ingestion_scopes.sql exactly.
-- ============================================================================

INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES
    -- OCI registry observation of the demo image repository.
    ('oci-registry://demo.invalid/vuln-demo-app',
     'oci_repository', 'oci_registry', 'demo.invalid/vuln-demo-app',
     'oci_registry', 'demo.invalid/vuln-demo-app',
     '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
     'active', 'gen:oci:vuln-demo-app'),

    -- SBOM + attestation attestation document scope.
    ('sbom-attestation://demo.invalid/vuln-demo-app',
     'sbom_attestation', 'sbom_attestation', 'demo.invalid/vuln-demo-app',
     'sbom_attestation', 'demo.invalid/vuln-demo-app',
     '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
     'active', 'gen:sbom:vuln-demo-app'),

    -- Synthetic vulnerability-intelligence advisory scope.
    ('vuln-intel://osv/npm/synthetic-vulnerable-npm',
     'vulnerability_advisory', 'vulnerability_intelligence',
     'osv/npm/synthetic-vulnerable-npm', 'vulnerability_intelligence',
     'osv/npm/synthetic-vulnerable-npm',
     '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
     'active', 'gen:vuln:synthetic-vulnerable-npm')
ON CONFLICT (scope_id) DO NOTHING;

-- ============================================================================
-- 2. scope_generations — one active generation per scope. Columns match
--    schema/data-plane/postgres/002_scope_generations.sql exactly. The active
--    generation is what the reducer's "active fact" loaders join against.
-- ============================================================================

INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at
) VALUES
    ('gen:oci:vuln-demo-app',
     'oci-registry://demo.invalid/vuln-demo-app',
     'registry_scan', NULL,
     '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
     'active', '2026-06-18T00:00:01Z'),

    ('gen:sbom:vuln-demo-app',
     'sbom-attestation://demo.invalid/vuln-demo-app',
     'attestation_scan', NULL,
     '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
     'active', '2026-06-18T00:00:01Z'),

    ('gen:vuln:synthetic-vulnerable-npm',
     'vuln-intel://osv/npm/synthetic-vulnerable-npm',
     'advisory_sync', NULL,
     '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
     'active', '2026-06-18T00:00:01Z')
ON CONFLICT (generation_id) DO NOTHING;

-- ============================================================================
-- 3. fact_records — raw collector facts. Columns match
--    schema/data-plane/postgres/003_fact_records.sql exactly. All carry
--    is_tombstone=FALSE (default) and source_confidence='reported' so the
--    reducer active-fact loaders (which filter is_tombstone = FALSE on the
--    active generation) pick them up.
-- ============================================================================

-- ----------------------------------------------------------------------------
-- 3a. oci_registry.image_manifest — the registry-observed image digest.
--     Payload fields mirror NewManifestEnvelope/descriptorPayload in
--     go/internal/collector/ociregistry/envelope.go. The (registry, repository,
--     digest) triple plus correlation_anchors is what the image-identity
--     reducer joins against the K8s manifest's digest reference to classify
--     exact_digest and emit image_ref = registry/repository@digest.
-- ----------------------------------------------------------------------------
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    source_confidence, observed_at, ingested_at, payload
) VALUES (
    'fact:oci-manifest:vuln-demo-app',
    'oci-registry://demo.invalid/vuln-demo-app',
    'gen:oci:vuln-demo-app',
    'oci_registry.image_manifest',
    'oci-manifest:demo.invalid/vuln-demo-app@sha256:1111',
    '1.0.0', 'oci_registry', 'oci_registry', 'oci-manifest:vuln-demo-app',
    'reported', '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
    jsonb_build_object(
        'provider', 'generic',
        'registry', 'demo.invalid',
        'repository', 'vuln-demo-app',
        'repository_id', 'oci-registry://demo.invalid/vuln-demo-app',
        'descriptor_id', 'oci-descriptor://demo.invalid/vuln-demo-app@sha256:1111111111111111111111111111111111111111111111111111111111111111',
        'digest', 'sha256:1111111111111111111111111111111111111111111111111111111111111111',
        'media_type', 'application/vnd.oci.image.manifest.v1+json',
        'source_tag', '1.0.0',
        'correlation_anchors', jsonb_build_array(
            'oci-registry://demo.invalid/vuln-demo-app',
            'sha256:1111111111111111111111111111111111111111111111111111111111111111'
        )
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 3b. sbom.document — the CycloneDX SBOM body. subject_digest binds it to the
--     image. parse_status='parsed' so the attachment reducer does not classify
--     it unparseable. Fields mirror sbomDocumentFromEnvelope in
--     go/internal/reducer/sbom_attestation_attachment_index.go.
-- ----------------------------------------------------------------------------
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    source_confidence, observed_at, ingested_at, payload
) VALUES (
    'fact:sbom-document:vuln-demo-app',
    'sbom-attestation://demo.invalid/vuln-demo-app',
    'gen:sbom:vuln-demo-app',
    'sbom.document',
    'sbom-document:demo.invalid/vuln-demo-app@sha256:1111',
    '1.0.0', 'sbom_attestation', 'sbom_attestation', 'sbom-document:vuln-demo-app',
    'reported', '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
    jsonb_build_object(
        'document_id', 'sbom-doc:vuln-demo-app',
        'document_digest', 'sha256:2222222222222222222222222222222222222222222222222222222222222222',
        'subject_digest', 'sha256:1111111111111111111111111111111111111111111111111111111111111111',
        'parse_status', 'parsed',
        'format', 'cyclonedx',
        'spec_version', '1.4'
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 3c. sbom.component — the synthetic vulnerable component. purl
--     pkg:npm/synthetic-vulnerable-npm@1.0.0 is the join key to the advisory's
--     affected package (componentMatchesAffectedPackage in
--     go/internal/reducer/supply_chain_impact_match.go matches on purl). The
--     shared document_id links the component to the document/attachment.
-- ----------------------------------------------------------------------------
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    source_confidence, observed_at, ingested_at, payload
) VALUES (
    'fact:sbom-component:synthetic-vulnerable-npm',
    'sbom-attestation://demo.invalid/vuln-demo-app',
    'gen:sbom:vuln-demo-app',
    'sbom.component',
    'sbom-component:synthetic-vulnerable-npm@1.0.0',
    '1.0.0', 'sbom_attestation', 'sbom_attestation', 'sbom-component:synthetic-vulnerable-npm',
    'reported', '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
    jsonb_build_object(
        'document_id', 'sbom-doc:vuln-demo-app',
        'purl', 'pkg:npm/synthetic-vulnerable-npm@1.0.0',
        'name', 'synthetic-vulnerable-npm',
        'version', '1.0.0',
        'ecosystem', 'npm'
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 3d. attestation.statement — the in-toto/CycloneDX subject binding. subject_
--     digest(s) bind the predicate to the image digest. parse_status='parsed';
--     predicate_type is the CycloneDX BOM type. Fields mirror
--     attestationDocumentFromEnvelope: a single subject_digest avoids the
--     ambiguous-subject path. The same document_id ties it to the SBOM body.
-- ----------------------------------------------------------------------------
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    source_confidence, observed_at, ingested_at, payload
) VALUES (
    'fact:attestation-statement:vuln-demo-app',
    'sbom-attestation://demo.invalid/vuln-demo-app',
    'gen:sbom:vuln-demo-app',
    'attestation.statement',
    'attestation-statement:demo.invalid/vuln-demo-app@sha256:1111',
    '1.0.0', 'sbom_attestation', 'sbom_attestation', 'attestation-statement:vuln-demo-app',
    'reported', '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
    jsonb_build_object(
        'statement_id', 'sbom-doc:vuln-demo-app',
        'document_id', 'sbom-doc:vuln-demo-app',
        'subject_digest', 'sha256:1111111111111111111111111111111111111111111111111111111111111111',
        'subject_digests', jsonb_build_array('sha256:1111111111111111111111111111111111111111111111111111111111111111'),
        'parse_status', 'parsed',
        'predicate_type', 'https://cyclonedx.org/bom'
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 3e. vulnerability.cve — the synthetic advisory identity. Fields mirror
--     supplyChainCVEFromEnvelope in
--     go/internal/reducer/supply_chain_impact_match.go (cve_id, advisory_id,
--     source, cvss_score, severity_label).
-- ----------------------------------------------------------------------------
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    source_confidence, observed_at, ingested_at, payload
) VALUES (
    'fact:vuln-cve:synthetic-npm',
    'vuln-intel://osv/npm/synthetic-vulnerable-npm',
    'gen:vuln:synthetic-vulnerable-npm',
    'vulnerability.cve',
    'vuln-cve:CVE-2026-SYNTHETIC-NPM',
    '1.0.0', 'vulnerability_intelligence', 'vulnerability_intelligence', 'vuln-cve:synthetic-npm',
    'reported', '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
    jsonb_build_object(
        'cve_id', 'CVE-2026-SYNTHETIC-NPM',
        'advisory_id', 'GHSA-synthetic-npm-0001',
        'source', 'osv',
        'cvss_score', 9.8,
        'severity_label', 'CRITICAL',
        'aliases', jsonb_build_array('CVE-2026-SYNTHETIC-NPM', 'GHSA-synthetic-npm-0001')
    )
)
ON CONFLICT (fact_id) DO NOTHING;

-- ----------------------------------------------------------------------------
-- 3f. vulnerability.affected_package — the synthetic affected package. Fields
--     mirror supplyChainAffectedPackageFromEnvelope. The purl
--     pkg:npm/synthetic-vulnerable-npm@1.0.0 is the join key to the SBOM
--     component (package_id npm:synthetic-vulnerable-npm differs from the
--     component's derived pkg:npm/... id, so the match fires via purl).
--
--     NOTE (impact_status): affected_versions=['1.0.0'] is the EXACT version
--     match that lets the SBOM-derived path classify affected_derived. The
--     open-ended affected_range '[,1.0.1)' is intentionally OMITTED: the npm
--     range matcher treats the empty-lower-bound form as a malformed range and
--     fail-closes to possibly_affected. The image_ref hop is populated in BOTH
--     states (the finding sets ImageRef before status is finalized), so the
--     proof's image_ref assertion holds either way; omitting the range keeps
--     the documented affected_derived status. fixed_versions=['1.0.1'] records
--     the remediation target.
-- ----------------------------------------------------------------------------
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, source_system, source_fact_key,
    source_confidence, observed_at, ingested_at, payload
) VALUES (
    'fact:vuln-affected-package:synthetic-npm',
    'vuln-intel://osv/npm/synthetic-vulnerable-npm',
    'gen:vuln:synthetic-vulnerable-npm',
    'vulnerability.affected_package',
    'vuln-affected-package:CVE-2026-SYNTHETIC-NPM:npm:synthetic-vulnerable-npm',
    '1.0.0', 'vulnerability_intelligence', 'vulnerability_intelligence', 'vuln-affected-package:synthetic-npm',
    'reported', '2026-06-18T00:00:00Z', '2026-06-18T00:00:00Z',
    jsonb_build_object(
        'cve_id', 'CVE-2026-SYNTHETIC-NPM',
        'advisory_id', 'GHSA-synthetic-npm-0001',
        'source', 'osv',
        'package_id', 'npm:synthetic-vulnerable-npm',
        'ecosystem', 'npm',
        'package_name', 'synthetic-vulnerable-npm',
        'purl', 'pkg:npm/synthetic-vulnerable-npm@1.0.0',
        'affected_versions', jsonb_build_array('1.0.0'),
        'fixed_versions', jsonb_build_array('1.0.1')
    )
)
ON CONFLICT (fact_id) DO NOTHING;

COMMIT;

-- ----------------------------------------------------------------------------
-- Sanity-check queries (informational; not executed by the proof script):
--
--   SELECT scope_id, status, active_generation_id FROM ingestion_scopes
--     WHERE scope_id IN (
--       'oci-registry://demo.invalid/vuln-demo-app',
--       'sbom-attestation://demo.invalid/vuln-demo-app',
--       'vuln-intel://osv/npm/synthetic-vulnerable-npm');
--
--   SELECT fact_kind, payload->>'purl' AS purl, payload->>'digest' AS digest,
--          payload->>'subject_digest' AS subject_digest
--     FROM fact_records
--     WHERE scope_id LIKE '%vuln-demo-app%'
--        OR scope_id LIKE '%synthetic-vulnerable-npm%'
--     ORDER BY fact_kind;
-- ----------------------------------------------------------------------------
