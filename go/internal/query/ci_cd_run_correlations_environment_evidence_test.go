// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCICDListRunCorrelationsExposesEnvironmentEvidence pins that the
// environment_evidence key survives all the way to the wire.
//
// The handler builds its response with a direct struct conversion,
// CICDRunCorrelationResult(row), so a field present on the row but missing from
// the result type would not compile, while a field present on both but never
// decoded from the payload would silently serialize as empty. #5426 branches on
// this value to stop promoting deployed_image from a CI-declared environment
// alone, so an empty value there is indistinguishable from "declared" and would
// quietly re-enable the promotion this exists to gate.
func TestCICDListRunCorrelationsExposesEnvironmentEvidence(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{rows: []CICDRunCorrelationRow{{
		CorrelationID:       "fact-deploy-1",
		Provider:            "github_actions",
		RunID:               "5150",
		RepositoryID:        "repository:r_69256c06",
		CommitSHA:           "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c",
		Environment:         "production",
		EnvironmentEvidence: "deploy_event",
		Outcome:             "exact",
	}}}
	handler := &CICDHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=repository:r_69256c06&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var body struct {
		Data struct {
			Correlations []map[string]any `json:"correlations"`
		} `json:"data"`
		Correlations []map[string]any `json:"correlations"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, w.Body.String())
	}
	rows := body.Correlations
	if len(rows) == 0 {
		rows = body.Data.Correlations
	}
	if len(rows) != 1 {
		t.Fatalf("correlations = %d, want 1; body = %s", len(rows), w.Body.String())
	}
	if got := rows[0]["environment_evidence"]; got != "deploy_event" {
		t.Fatalf("environment_evidence = %v, want deploy_event; body = %s", got, w.Body.String())
	}
}
