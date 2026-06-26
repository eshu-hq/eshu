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

-- #3389: the SBOM/attestation attachment count and inventory aggregates
-- (GET /api/v0/supply-chain/sbom-attestations/attachments/count) run a COUNT(*)
-- and GROUP BY over every active reducer_sbom_attestation_attachment fact with
-- no payload anchor in the common "count everything" case. The payload-leading
-- indexes above (subject_digest, document_id, attachment_status, ...) cannot
-- bound that no-anchor enumeration to the fact_kind, so the planner falls back
-- to a whole-table scan at collector scale. This partial index's predicate
-- bounds the scan to exactly this fact_kind's active tuples, and its
-- (scope_id, generation_id) leading keys resolve the
-- ingestion_scopes/scope_generations active-generation join straight from the
-- index (index-only when the heap is vacuum-fresh, a bounded index scan
-- otherwise).
CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_active_scan_idx
    ON fact_records (
        scope_id,
        generation_id,
        fact_id ASC
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
