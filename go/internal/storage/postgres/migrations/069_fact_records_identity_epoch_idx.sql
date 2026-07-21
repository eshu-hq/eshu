-- Fast epoch probe for the container-image-identity fact cache (#5438).
-- Serves SELECT count(*), max(observed_at) over the identity-fact filter
-- without scanning the full fact_records join.
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
    )
    AND is_tombstone = FALSE;
