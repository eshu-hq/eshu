// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Test 8: environment_evidence round-trips through the actual HTTP findings
// route response body -- decoded from raw JSON bytes into a fresh anonymous
// type, the way an external caller would, rather than by reusing
// SupplyChainImpactFindingResult on both sides of the assertion.
func TestSupplyChainImpactFindingsExposeEnvironmentEvidenceInResponseBody(t *testing.T) {
	t.Parallel()

	store := &recordingSupplyChainImpactFindingStore{
		rows: []SupplyChainImpactFindingRow{
			{
				FindingID:    "finding-env-evidence",
				CVEID:        "CVE-2026-5426",
				ImpactStatus: "affected_exact",
				Confidence:   "exact",
				RepositoryID: "repo://example/svc",
				Environments: []string{"prod", "staging"},
				EnvironmentEvidence: map[string]string{
					"prod":    "deploy_event",
					"staging": "declared",
				},
			},
		},
	}
	handler := &SupplyChainHandler{ImpactFindings: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/svc&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Findings []struct {
			EnvironmentEvidence map[string]string `json:"environment_evidence"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("Findings length = %d, want 1; body = %s", len(resp.Findings), w.Body.String())
	}
	if got, want := resp.Findings[0].EnvironmentEvidence["prod"], "deploy_event"; got != want {
		t.Fatalf("environment_evidence[prod] = %q, want %q", got, want)
	}
	if got, want := resp.Findings[0].EnvironmentEvidence["staging"], "declared"; got != want {
		t.Fatalf("environment_evidence[staging] = %q, want %q", got, want)
	}
}
