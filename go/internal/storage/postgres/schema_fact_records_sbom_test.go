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

	// #3389: the SBOM/attestation attachment count and inventory aggregates
	// (GET /api/v0/supply-chain/sbom-attestations/attachments/count) run a
	// COUNT(*) and GROUP BY over every active reducer_sbom_attestation_attachment
	// fact with no payload anchor in the common case. The existing attachment
	// indexes all lead with a payload column (subject_digest, document_id,
	// attachment_status, ...) so they cannot bound the no-anchor enumeration to
	// the fact_kind; a partial index whose predicate bounds the scan to this
	// fact_kind's active tuples, leading with the (scope_id, generation_id) join
	// keys, gives the planner a fact-kind-bounded index scan instead of a
	// whole-table scan at collector scale.
	scanIdx := "CREATE INDEX IF NOT EXISTS fact_records_sbom_attestation_attachments_active_scan_idx"
	if !strings.Contains(sbomIndexes.SQL, scanIdx) {
		t.Fatalf("sbom attachment index SQL missing active-scan index %q:\n%s", scanIdx, sbomIndexes.SQL)
	}
	scanSQL := sbomIndexes.SQL[strings.Index(sbomIndexes.SQL, scanIdx):]
	for _, want := range []string{
		"(\n        scope_id,\n        generation_id,\n        fact_id ASC\n    )",
		"WHERE fact_kind = 'reducer_sbom_attestation_attachment'",
		"AND is_tombstone = FALSE",
	} {
		if !strings.Contains(scanSQL, want) {
			t.Fatalf("sbom attachment active-scan index missing %q:\n%s", want, scanSQL[:min(len(scanSQL), 400)])
		}
	}
}
