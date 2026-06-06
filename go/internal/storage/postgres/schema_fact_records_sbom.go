package postgres

const factRecordSBOMAttestationReadIndexesSQL = `
CREATE INDEX IF NOT EXISTS fact_records_oci_image_referrer_subject_idx
    ON fact_records (
        (payload->>'subject_digest'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'oci_registry.image_referrer'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_container_image_identity_source_repository_anchor_idx
    ON fact_records USING GIN ((payload->'source_repository_ids'))
    WHERE fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_container_image_identity_workload_anchor_idx
    ON fact_records USING GIN ((payload->'workload_ids'))
    WHERE fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_container_image_identity_service_anchor_idx
    ON fact_records USING GIN ((payload->'service_ids'))
    WHERE fact_kind = 'reducer_container_image_identity'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_subject_idx
    ON fact_records (
        (payload->>'subject_digest'),
        (payload->>'attachment_status'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_sbom_attestation_attachment'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_document_idx
    ON fact_records (
        (payload->>'document_id'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_sbom_attestation_attachment'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_document_digest_idx
    ON fact_records (
        (payload->>'document_digest'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_sbom_attestation_attachment'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_status_idx
    ON fact_records (
        (payload->>'attachment_status'),
        (payload->>'artifact_kind'),
        fact_id ASC,
        generation_id
    )
    WHERE fact_kind = 'reducer_sbom_attestation_attachment'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_repository_anchor_idx
    ON fact_records USING GIN ((payload->'repository_ids'))
    WHERE fact_kind = 'reducer_sbom_attestation_attachment'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_workload_anchor_idx
    ON fact_records USING GIN ((payload->'workload_ids'))
    WHERE fact_kind = 'reducer_sbom_attestation_attachment'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_service_anchor_idx
    ON fact_records USING GIN ((payload->'service_ids'))
    WHERE fact_kind = 'reducer_sbom_attestation_attachment'
      AND is_tombstone = FALSE;
`
