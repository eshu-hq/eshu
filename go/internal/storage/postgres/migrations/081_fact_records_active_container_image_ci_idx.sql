-- 081_fact_records_active_container_image_ci_idx.sql
--
-- Cross-scope active-fact index for the container_image_identity reducer's
-- DERIVED_FROM (base-image lineage, #5460) CI bridge (#5810): ci.run and
-- container-image-typed ci.artifact facts are written by the ci_cd_run
-- collector in the CI run's OWN scope, a different scope than the repository
-- whose Dockerfile the DERIVED_FROM projection is owner-scoped to. Without a
-- cross-scope index/query these facts are invisible to a repository-scoped
-- refresh, so a CI-built child could never durably qualify as
-- BuildProvenanceRepositoryIDs at the projection that actually writes the edge.
--
-- A dedicated index (mirroring fact_records_active_container_image_slsa_idx,
-- migration 075) was chosen over widening fact_records_identity_epoch_idx
-- deliberately: ci.run/ci.artifact is the highest-churn fact family in the
-- system, and coupling it into the identity-epoch cache's drift-locked partial
-- index (069/076/077, identity_epoch_cache_contract_test.go) would move that
-- cache's probe fingerprint on every CI run, defeating the cache far more often
-- than the identity-fact family it exists for.
--
-- The predicate narrows ci.artifact to artifact_type = 'container_image':
-- non-image artifacts (coverage reports, SBOM bundles, test reports) are the
-- bulk of ci.artifact volume, and the reducer already ignores them
-- (addCICDArtifactImageReference, container_image_identity_typed_evidence.go).
-- ci.run cannot be narrowed the same way -- every run row is a potential
-- repository/commit anchor.
--
-- Mirrors fact_records_active_container_image_slsa_idx: a plain
-- (observed_at, fact_id) partial index that does not cover is_tombstone, so
-- ListActiveContainerImageCIFacts applies that predicate as a residual filter
-- on the rows the index scan already visits.
--
-- This predicate MUST stay identical to listActiveContainerImageCIFactsFilterSQL
-- (facts_active_container_image_ci.go) --
-- TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL locks the
-- two together.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_active_container_image_ci_idx
    ON fact_records (observed_at ASC, fact_id ASC)
    WHERE (
        (fact_kind = 'ci.run' AND source_system = 'ci_cd_run')
        OR (fact_kind = 'ci.artifact' AND source_system = 'ci_cd_run'
            AND payload->>'artifact_type' = 'container_image')
      );
