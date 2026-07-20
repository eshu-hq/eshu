// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDecodeSBOMAttestationAttachmentRowSurfacesDependencyAndExternalReferenceEvidence
// is the query-side accuracy regression for #5370: the reducer-persisted
// dependency_relationship_evidence/external_reference_evidence payload
// arrays and their _count siblings must decode into the typed wire rows plus
// an honest truncation flag (persistedCount > len(decodedRows), mirroring
// WarningSummariesTruncated). Before the reducer wired these kinds,
// decodeSBOMAttestationAttachmentRow had no field to read, so this asserts
// against payload shapes the reducer now actually writes.
func TestDecodeSBOMAttestationAttachmentRowSurfacesDependencyAndExternalReferenceEvidence(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"document_id":                   "doc-dep-ext",
		"attachment_status":             "attached_verified",
		"dependency_relationship_count": 3,
		"dependency_relationship_evidence": []map[string]any{
			{
				"from_component_id":   "pkg:npm/app@1.0.0",
				"to_component_id":     "pkg:npm/lib@2.0.0",
				"relationship_type":   "depends_on",
				"relationship_origin": "declared",
				"fact_id":             "dep-1",
			},
		},
		"external_reference_count": 1,
		"external_reference_evidence": []map[string]any{
			{
				"component_id":   "pkg:npm/lib@2.0.0",
				"reference_type": "advisory",
				"reference_url":  "https://example.com/advisory/1",
				"fact_id":        "ref-1",
			},
		},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	row, err := decodeSBOMAttestationAttachmentRow("attachment-dep-ext", "inferred", payloadBytes)
	if err != nil {
		t.Fatalf("decodeSBOMAttestationAttachmentRow() error = %v", err)
	}
	if got, want := len(row.DependencyRelationships), 1; got != want {
		t.Fatalf("len(DependencyRelationships) = %d, want %d", got, want)
	}
	if got, want := row.DependencyRelationships[0].FromComponentID, "pkg:npm/app@1.0.0"; got != want {
		t.Fatalf("DependencyRelationships[0].FromComponentID = %q, want %q", got, want)
	}
	if got, want := row.DependencyRelationshipCount, 3; got != want {
		t.Fatalf("DependencyRelationshipCount = %d, want %d", got, want)
	}
	if !row.DependencyRelationshipsTruncated {
		t.Fatal("DependencyRelationshipsTruncated = false, want true (count 3 > 1 decoded row)")
	}
	if got, want := len(row.ExternalReferences), 1; got != want {
		t.Fatalf("len(ExternalReferences) = %d, want %d", got, want)
	}
	if got, want := row.ExternalReferences[0].ReferenceURL, "https://example.com/advisory/1"; got != want {
		t.Fatalf("ExternalReferences[0].ReferenceURL = %q, want %q", got, want)
	}
	if row.ExternalReferencesTruncated {
		t.Fatal("ExternalReferencesTruncated = true, want false (count 1 == 1 decoded row)")
	}
}

func TestSupplyChainListSBOMAttestationAttachmentsSurfacesDependencyAndExternalReferenceWire(t *testing.T) {
	t.Parallel()

	store := &recordingSBOMAttestationAttachmentStore{
		page: SBOMAttestationAttachmentPage{
			Attachments: []SBOMAttestationAttachmentRow{
				{
					AttachmentID:     "attachment-dep-ext",
					DocumentID:       "doc-dep-ext",
					AttachmentStatus: "attached_verified",
					DependencyRelationships: []DependencyRelationshipRow{
						{FromComponentID: "pkg:npm/app@1.0.0", ToComponentID: "pkg:npm/lib@2.0.0", RelationshipType: "depends_on", FactID: "dep-1"},
					},
					DependencyRelationshipCount:      3,
					DependencyRelationshipsTruncated: true,
					ExternalReferences: []ExternalReferenceRow{
						{ComponentID: "pkg:npm/lib@2.0.0", ReferenceType: "advisory", ReferenceURL: "https://example.com/advisory/1", FactID: "ref-1"},
					},
					ExternalReferenceCount:      1,
					ExternalReferencesTruncated: false,
				},
			},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachments: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments?document_id=doc-dep-ext&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Attachments []SBOMAttestationAttachmentResult `json:"attachments"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Attachments), 1; got != want {
		t.Fatalf("len(attachments) = %d, want %d", got, want)
	}
	result := resp.Attachments[0]
	if got, want := len(result.DependencyRelationships), 1; got != want {
		t.Fatalf("len(DependencyRelationships) = %d, want %d", got, want)
	}
	if got, want := result.DependencyRelationships[0].RelationshipType, "depends_on"; got != want {
		t.Fatalf("DependencyRelationships[0].RelationshipType = %q, want %q", got, want)
	}
	if !result.DependencyRelationshipsTruncated {
		t.Fatal("DependencyRelationshipsTruncated = false, want true")
	}
	if got, want := len(result.ExternalReferences), 1; got != want {
		t.Fatalf("len(ExternalReferences) = %d, want %d", got, want)
	}
	if result.ExternalReferencesTruncated {
		t.Fatal("ExternalReferencesTruncated = true, want false")
	}
	if !strings.Contains(w.Body.String(), `"dependency_relationships"`) {
		t.Fatalf("response missing dependency_relationships key: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"external_references"`) {
		t.Fatalf("response missing external_references key: %s", w.Body.String())
	}
}
