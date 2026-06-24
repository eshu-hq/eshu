// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContainerImageIdentityAggregateCountReportsSourceBridgeMissingEvidence(t *testing.T) {
	t.Parallel()

	sourceRepoID := "repo://example/api"
	store := &stubContainerImageIdentityAggregateStore{
		count: ContainerImageIdentityAggregateCount{
			TotalIdentities:    0,
			ByOutcome:          map[string]int{},
			ByIdentityStrength: map[string]int{},
		},
	}
	handler := &SupplyChainHandler{ContainerImageAggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/container-images/identities/count?source_repository_id="+sourceRepoID,
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		SourceBridge ContainerImageIdentitySourceBridge `json:"source_bridge"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	assertStringSet(t, body.SourceBridge.MissingEvidence, []string{
		"deployment_image_reference_missing",
		"image_registry_observation_missing",
		"source_to_image_correlation_missing",
	})
}
