// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSBOMAttestationAttachmentAggregateCountReportsMissingEvidence(t *testing.T) {
	t.Parallel()

	store := &stubSBOMAttestationAttachmentAggregateStore{
		count: SBOMAttestationAttachmentAggregateCount{
			TotalAttachments:   0,
			ByAttachmentStatus: map[string]int{},
			ByArtifactKind:     map[string]int{},
			MissingEvidence:    []string{"repository_to_image_evidence_missing"},
		},
	}
	handler := &SupplyChainHandler{SBOMAttachmentAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/sbom-attestations/attachments/count?repository_id=repo://example/api",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		MissingEvidence []string `json:"missing_evidence"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	assertStringSet(t, body.MissingEvidence, []string{"repository_to_image_evidence_missing"})
}
