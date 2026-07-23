-- 075_fact_records_active_container_image_slsa_idx.sql
--
-- Cross-scope active-fact index for the container_image_identity reducer's
-- SLSA provenance join (#5456 PR #5707 P1-b): attestation.statement,
-- attestation.slsa_provenance, and attestation.signature_verification facts
-- are written by the SBOM-attestation collector in its OWN scope, a
-- different scope than the OCI registry manifest (or Git/CI evidence) a
-- container_image_identity refresh usually runs against. Without a
-- cross-scope index/query these facts are invisible to a refresh triggered
-- from any other scope, so the slsa_provenance_commit identity tier could
-- never reach a durable decision outside a same-scope reducer run.
--
-- Mirrors fact_records_active_repository_idx (003_fact_records.sql): a plain
-- (observed_at, fact_id) partial index that does not cover is_tombstone, so
-- ListActiveContainerImageSLSAFacts applies that predicate as a residual
-- filter on the rows the index scan already visits.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_active_container_image_slsa_idx
    ON fact_records (observed_at ASC, fact_id ASC)
    WHERE fact_kind IN (
        'attestation.statement',
        'attestation.slsa_provenance',
        'attestation.signature_verification'
      )
      AND source_system = 'sbom_attestation';
