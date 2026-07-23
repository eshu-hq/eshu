-- Widen the container-image-identity epoch/load partial index to admit
-- Dockerfile base-image evidence (#5460).
--
-- Migration 069 created fact_records_identity_epoch_idx with a content_entity
-- arm that matched only payloads carrying entity_metadata.container_images or
-- metadata.container_images. A Dockerfile content_entity carries neither: its
-- base image lives in parsed_file_data.dockerfile_stages. Base-image lineage
-- widened identityFactFilterSQL to admit that shape, so the partial index must
-- be recreated with the matching predicate -- a partial index only covers a
-- query when its predicate is a subset match of the query's filter clause, so
-- leaving 069's narrower predicate in place would silently drop the epoch
-- probe and the identity fact load back to a full scan.
--
-- Drop-then-create rather than CREATE ... IF NOT EXISTS: the index name already
-- exists with the old predicate, and IF NOT EXISTS would keep the stale
-- definition. Both statements run CONCURRENTLY so neither takes a write lock on
-- fact_records.
DROP INDEX CONCURRENTLY IF EXISTS fact_records_identity_epoch_idx;

CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_identity_epoch_idx
    ON fact_records (observed_at, fact_id)
    WHERE (
        (
            fact_kind IN ('oci_registry.image_tag_observation', 'oci_registry.image_manifest', 'oci_registry.image_index')
            AND source_system = 'oci_registry'
        )
        OR (
            fact_kind = 'aws_image_reference'
            AND source_system = 'aws'
        )
        OR (
            fact_kind = 'azure_image_reference'
            AND source_system = 'azure'
        )
        OR (
            fact_kind = 'gcp_image_reference'
            AND source_system = 'gcp'
        )
        OR (
            fact_kind = 'aws_relationship'
            AND source_system = 'aws'
            AND payload->>'target_type' = 'container_image'
        )
        OR (
            fact_kind = 'content_entity'
            AND source_system = 'git'
            AND (
                payload->'entity_metadata' ? 'container_images'
                OR payload->'metadata' ? 'container_images'
                OR payload->'parsed_file_data' ? 'dockerfile_stages'
            )
        )
    )
    AND is_tombstone = FALSE;
