-- Recreate the container-image-identity epoch/load partial index with the
-- Dockerfile base-image arm (#5460); migration 076 dropped the narrow one.
--
-- Migration 069's identity filter admitted image-bearing content_entity facts
-- but no Dockerfile base images. A Dockerfile is never a content_entity: the
-- parser emits it as a `file` fact whose parsed_file_data carries
-- dockerfile_stages (the base image FROM refs). Base-image lineage added a
-- `file` arm to identityFactFilterSQL to admit that shape, so this predicate
-- must match it -- a partial index only covers a query when its predicate is a
-- subset match of the query's filter, so leaving the narrower predicate in
-- place would silently drop the epoch probe and the identity fact load back to
-- a full scan.
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
            )
        )
        OR (
            fact_kind = 'file'
            AND source_system = 'git'
            AND payload->'parsed_file_data' ? 'dockerfile_stages'
        )
    )
    AND is_tombstone = FALSE;
