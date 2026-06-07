package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeSBOMAttestationAttachmentFactIndexes(t *testing.T) {
	t.Parallel()

	var sbomIndexes Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_record_sbom_attestation_indexes" {
			sbomIndexes = def
			break
		}
	}
	if sbomIndexes.Name == "" {
		t.Fatal("fact_record_sbom_attestation_indexes definition missing")
	}
	for _, want := range []string{
		"fact_records_oci_image_referrer_subject_idx",
		"fact_records_container_image_identity_source_repository_anchor_idx",
		"fact_records_container_image_identity_workload_anchor_idx",
		"fact_records_container_image_identity_service_anchor_idx",
		"fact_records_sbom_attestation_attachments_subject_idx",
		"fact_records_sbom_attestation_attachments_document_idx",
		"fact_records_sbom_attestation_attachments_document_digest_idx",
		"fact_records_sbom_attestation_attachments_status_idx",
		"fact_records_sbom_attestation_attachments_repository_anchor_idx",
		"fact_records_sbom_attestation_attachments_workload_anchor_idx",
		"fact_records_sbom_attestation_attachments_service_anchor_idx",
		"'oci_registry.image_referrer'",
		"'reducer_sbom_attestation_attachment'",
		"(payload->>'subject_digest')",
		"(payload->>'document_id')",
		"(payload->>'document_digest')",
		"(payload->>'attachment_status')",
		"USING GIN ((payload->'repository_ids'))",
		"USING GIN ((payload->'workload_ids'))",
		"USING GIN ((payload->'service_ids'))",
		"USING GIN ((payload->'source_repository_ids'))",
	} {
		if !strings.Contains(sbomIndexes.SQL, want) {
			t.Fatalf("sbom attachment index SQL missing %q", want)
		}
	}
}
