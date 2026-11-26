// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"testing"
)

// TestContractShapesLane2SBOM pins the lane-2 shape-completeness fixes for
// the SBOM attestation fragments: the full reducer-owned attachment row
// (SBOMAttestationAttachmentResult) and the aggregate envelopes.
func TestContractShapesLane2SBOM(t *testing.T) {
	t.Parallel()

	t.Run("sbom attachments", func(t *testing.T) {
		t.Parallel()
		// Owners: listSBOMAttachments (sbom_attachments.go) emits
		// SBOMAttestationAttachmentResult rows built by
		// buildSBOMAttestationAttachmentResult; the cursor carries the
		// last attachment id and is built inline.
		doc := lane2Fragment(t, SBOMAttestations, "SBOMAttestations")
		schema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/sbom-attestations/attachments", "get")
		props := lane2PropsOf(t, schema, "attachments")
		lane2RequireExactKeys(t, props, "attachments",
			"attachments", "count", "missing_evidence", "limit", "truncated",
			"next_cursor", "collector_readiness")
		lane2RequireScalarTypes(t, props, "attachments", map[string]string{
			"attachments": "array", "count": "integer",
			"missing_evidence": "array", "limit": "integer",
			"truncated": "boolean", "next_cursor": "object",
		})
		lane2RequireDescribed(t, props, "attachments",
			"attachments", "count", "missing_evidence", "limit", "truncated",
			"next_cursor")

		row := lane2ItemsOf(t, lane2Child(t, props, "attachments", "attachments"), "attachments[]")
		rowProps := lane2PropsOf(t, row, "attachments[]")
		lane2RequireExactKeys(t, rowProps, "attachments[]",
			"attachment_id", "subject_digest", "document_id", "document_digest",
			"repository_ids", "workload_ids", "service_ids",
			"attachment_status", "parse_status", "verification_status",
			"verification_policy", "artifact_kind", "format", "spec_version",
			"reason", "attachment_scope", "missing_evidence",
			"canonical_writes", "component_count", "component_evidence",
			"component_evidence_truncated", "dependency_relationships",
			"dependency_relationship_count",
			"dependency_relationships_truncated", "external_references",
			"external_reference_count", "external_references_truncated",
			"slsa_provenance_predicate_type", "slsa_provenance_builder_id",
			"slsa_provenance_materials", "slsa_provenance_material_count",
			"slsa_provenance_materials_truncated",
			"slsa_provenance_config_source_uri",
			"slsa_provenance_config_source_entry_point",
			"slsa_provenance_config_source_digest", "warning_summaries",
			"warning_summary_count", "warning_summaries_truncated",
			"evidence_fact_ids", "source_freshness", "source_confidence")
		lane2RequireScalarTypes(t, rowProps, "attachments[]", map[string]string{
			"attachment_id": "string", "attachment_status": "string",
			"canonical_writes": "integer", "component_count": "integer",
			"component_evidence_truncated":        "boolean",
			"dependency_relationship_count":       "integer",
			"dependency_relationships_truncated":  "boolean",
			"external_reference_count":            "integer",
			"external_references_truncated":       "boolean",
			"slsa_provenance_material_count":      "integer",
			"slsa_provenance_materials_truncated": "boolean",
			"warning_summary_count":               "integer",
			"warning_summaries_truncated":         "boolean",
		})
		lane2RequireDescribed(t, rowProps, "attachments[]",
			"attachment_id", "subject_digest", "document_id", "document_digest",
			"repository_ids", "workload_ids", "service_ids",
			"attachment_status", "parse_status", "verification_status",
			"verification_policy", "artifact_kind", "format", "spec_version",
			"reason", "attachment_scope", "missing_evidence",
			"canonical_writes", "component_count", "component_evidence",
			"component_evidence_truncated", "dependency_relationships",
			"dependency_relationship_count",
			"dependency_relationships_truncated", "external_references",
			"external_reference_count", "external_references_truncated",
			"slsa_provenance_predicate_type", "slsa_provenance_builder_id",
			"slsa_provenance_materials", "slsa_provenance_material_count",
			"slsa_provenance_materials_truncated",
			"slsa_provenance_config_source_uri",
			"slsa_provenance_config_source_entry_point",
			"slsa_provenance_config_source_digest", "warning_summaries",
			"warning_summary_count", "warning_summaries_truncated",
			"evidence_fact_ids", "source_freshness", "source_confidence")

		// Owner: ComponentEvidenceRow.
		comp := lane2ItemsOf(t, lane2Child(t, rowProps, "component_evidence", "sbom.comp"), "sbom.comp[]")
		compProps := lane2PropsOf(t, comp, "sbom.comp[]")
		lane2RequireExactKeys(t, compProps, "sbom.comp[]",
			"component_id", "name", "version", "purl", "cpe", "fact_id")
		lane2RequireDescribed(t, compProps, "sbom.comp[]",
			"component_id", "name", "version", "purl", "cpe", "fact_id")

		// Owner: DependencyRelationshipRow.
		rel := lane2ItemsOf(t, lane2Child(t, rowProps, "dependency_relationships", "sbom.rel"), "sbom.rel[]")
		relProps := lane2PropsOf(t, rel, "sbom.rel[]")
		lane2RequireExactKeys(t, relProps, "sbom.rel[]",
			"from_component_id", "to_component_id", "relationship_type",
			"relationship_origin", "fact_id")
		lane2RequireDescribed(t, relProps, "sbom.rel[]",
			"from_component_id", "to_component_id", "relationship_type",
			"relationship_origin", "fact_id")

		// Owner: ExternalReferenceRow.
		ext := lane2ItemsOf(t, lane2Child(t, rowProps, "external_references", "sbom.ext"), "sbom.ext[]")
		extProps := lane2PropsOf(t, ext, "sbom.ext[]")
		lane2RequireExactKeys(t, extProps, "sbom.ext[]",
			"component_id", "reference_type", "reference_url",
			"reference_locator", "fact_id")
		lane2RequireDescribed(t, extProps, "sbom.ext[]",
			"component_id", "reference_type", "reference_url",
			"reference_locator", "fact_id")

		// Owner: SLSAMaterialRow.
		mat := lane2ItemsOf(t, lane2Child(t, rowProps, "slsa_provenance_materials", "sbom.mat"), "sbom.mat[]")
		matProps := lane2PropsOf(t, mat, "sbom.mat[]")
		lane2RequireExactKeys(t, matProps, "sbom.mat[]", "uri", "digest")
		lane2RequireDescribed(t, matProps, "sbom.mat[]", "uri", "digest")

		// Owner: listSBOMAttachments builds next_cursor only when
		// truncated, carrying the last attachment id forward.
		cursor := lane2Child(t, props, "next_cursor", "attachments")
		cursorProps := lane2PropsOf(t, cursor, "attachments.next_cursor")
		lane2RequireExactKeys(t, cursorProps, "attachments.next_cursor",
			"after_attachment_id")
		lane2RequireScalarTypes(t, cursorProps, "attachments.next_cursor",
			map[string]string{"after_attachment_id": "string"})
		lane2RequireDescribed(t, cursorProps, "attachments.next_cursor",
			"after_attachment_id")

		lane2RequireNoBareObjects(t, schema, "attachments")
	})

	t.Run("sbom attachment aggregates", func(t *testing.T) {
		t.Parallel()
		// Owners: the count/inventory aggregate handlers emit
		// SBOMAttestationAttachmentAggregateCount rollups and
		// SBOMAttestationAttachmentInventoryRow buckets
		// (store_sbom_attestation.go), with string-map scope echoes.
		doc := lane2Fragment(t, SBOMAttestationAttachmentAggregate, "SBOMAttestationAttachmentAggregate")

		countSchema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/sbom-attestations/attachments/count", "get")
		countProps := lane2PropsOf(t, countSchema, "sbom count")
		lane2RequireExactKeys(t, countProps, "sbom count",
			"total_attachments", "by_attachment_status", "by_artifact_kind",
			"missing_evidence", "scope")
		lane2RequireScalarTypes(t, countProps, "sbom count", map[string]string{
			"total_attachments": "integer", "by_attachment_status": "object",
			"by_artifact_kind": "object", "missing_evidence": "array",
			"scope": "object",
		})
		lane2RequireDescribed(t, countProps, "sbom count",
			"total_attachments", "by_attachment_status", "by_artifact_kind",
			"missing_evidence", "scope")
		scope := lane2Child(t, countProps, "scope", "sbom count.scope")
		if add, ok := scope["additionalProperties"].(map[string]any); !ok {
			t.Errorf("sbom count.scope: missing additionalProperties for the string filter map")
		} else if typ, _ := add["type"].(string); typ != "string" {
			t.Errorf("sbom count.scope: additionalProperties type %q, want string", typ)
		}
		if desc, _ := scope["description"].(string); desc == "" {
			t.Errorf("sbom count.scope: missing filter-echo description naming key semantics")
		}

		invSchema := lane2SchemaAt(t, doc, "/api/v0/supply-chain/sbom-attestations/attachments/inventory", "get")
		invProps := lane2PropsOf(t, invSchema, "sbom inventory")
		lane2RequireExactKeys(t, invProps, "sbom inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope")
		lane2RequireDescribed(t, invProps, "sbom inventory",
			"buckets", "count", "limit", "offset", "group_by", "truncated",
			"next_offset", "scope")
		bucket := lane2ItemsOf(t, lane2Child(t, invProps, "buckets", "sbom buckets"), "sbom buckets[]")
		bucketProps := lane2PropsOf(t, bucket, "sbom buckets[]")
		lane2RequireExactKeys(t, bucketProps, "sbom buckets[]",
			"dimension", "value", "count")
		lane2RequireScalarTypes(t, bucketProps, "sbom buckets[]", map[string]string{
			"dimension": "string", "value": "string", "count": "integer",
		})
		lane2RequireDescribed(t, bucketProps, "sbom buckets[]",
			"dimension", "value", "count")

		lane2RequireNoBareObjects(t, countSchema, "sbom count")
		lane2RequireNoBareObjects(t, invSchema, "sbom inventory")
	})
}
