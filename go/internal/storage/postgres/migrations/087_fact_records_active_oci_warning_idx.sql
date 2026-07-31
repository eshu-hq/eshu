-- 087_fact_records_active_oci_warning_idx.sql
--
-- The container_image_identity reducer reads active OCI warning facts before
-- publishing a demotion. These warnings are cross-scope safety declarations:
-- config-blob failures, truncated tag listings, and missing manifest digests
-- must be visible before absence can retire previously canonical truth.
--
-- The ordered partial index matches that global warning loader. It avoids one
-- scope-generation probe per active scope and satisfies the loader's keyset
-- order directly. scope_id and generation_id are trailing keys for the two
-- active-generation joins; payload and the remaining envelope columns stay in
-- the heap so warning writes do not pay for a wide covering index.

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_active_oci_warning_idx
    ON fact_records (
        observed_at ASC,
        fact_id ASC,
        scope_id,
        generation_id
    )
    WHERE fact_kind = 'oci_registry.warning'
      AND source_system = 'oci_registry'
      AND is_tombstone = FALSE;
