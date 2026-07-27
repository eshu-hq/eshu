-- 082_fact_records_active_container_image_ci_run_repository_idx.sql
--
-- Owner-scoped lookup index for the container_image_identity reducer's CI
-- bridge (#5810 P1 follow-up): ListActiveContainerImageCIFacts now pushes the
-- calling intent's owning repository_id into the query itself instead of
-- loading every active ci.run/ci.artifact fact platform-wide and filtering
-- in Go (filterContainerImageCIFactsForOwner, container_image_identity_ci_loader.go).
-- Without this index, resolving "which ci.run rows name repository X" has no
-- targeted access path -- fact_records_active_container_image_ci_idx
-- (migration 081) only narrows to the ci.run/ci.artifact family as a whole,
-- which at the documented 500,000-active-CI-run-scope worst case still means
-- visiting every one of those rows to find the handful (usually one) naming
-- a given repository. This expression index resolves that lookup directly.
--
-- Mirrors fact_records_workload_identity_workload_idx (migration for #5747,
-- schema_fact_records.go's fact_records_n precedent) and the
-- fact_records_container_image_identity_repository_idx shape already used
-- for the reducer's OWN identity facts: a leading JSONB expression column for
-- point lookups by a caller-supplied repository_id, scoped to exactly the
-- fact_kind/source_system this bridge's ci.run half consumes.
--
-- is_tombstone is included in the predicate (unlike migration 081, which
-- applies it as a residual filter) because this index services an equality
-- point lookup rather than a range scan -- folding the tombstone check into
-- the predicate keeps a retracted run's stale repository_id claim out of the
-- index entirely, rather than filtering it out of a handful of already-cheap
-- rows after the fact.
--
-- This predicate MUST stay identical to
-- listActiveContainerImageCIRunRepositoryFilterSQL
-- (facts_active_container_image_ci.go) --
-- TestFactRecordsActiveContainerImageCIRunRepositoryIndexPredicateMatchesFilterSQL
-- locks the two together, mirroring
-- TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL for
-- migration 081.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_active_container_image_ci_run_repository_idx
    ON fact_records ((payload->>'repository_id'), fact_id ASC, generation_id, scope_id)
    WHERE fact_kind = 'ci.run'
      AND source_system = 'ci_cd_run'
      AND is_tombstone = FALSE;
