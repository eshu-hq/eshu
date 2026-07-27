// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	vulnerabilitysuppressionv1 "github.com/eshu-hq/eshu/sdk/go/factschema/vulnerabilitysuppression/v1"
)

type recordingVulnerabilitySuppressionMutationStore struct {
	value  vulnerabilitysuppressionv1.Suppression
	result VulnerabilitySuppressionMutationResult
	calls  int
}

func (s *recordingVulnerabilitySuppressionMutationStore) UpsertVulnerabilitySuppression(
	_ context.Context,
	value vulnerabilitysuppressionv1.Suppression,
) (VulnerabilitySuppressionMutationResult, error) {
	s.calls++
	s.value = value
	return s.result, nil
}

func TestCreateVulnerabilitySuppressionValidatesAndPersistsOperatorFact(t *testing.T) {
	t.Parallel()

	store := &recordingVulnerabilitySuppressionMutationStore{
		result: VulnerabilitySuppressionMutationResult{
			SuppressionID: "suppression-CVE-2026-00001",
			GenerationID:  "suppression-generation-1",
			Changed:       true,
		},
	}
	handler := &SupplyChainHandler{SuppressionMutations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{
		"suppression_id":"suppression-CVE-2026-00001",
		"justification":"accepted_risk",
		"authored_at":"2026-07-27T12:00:00Z",
		"expires_at":"2026-08-01T12:00:00Z",
		"reason":"compensating control verified",
		"evidence_ref":"evidence://suppression/CVE-2026-00001",
		"scope":{"cve_id":"CVE-2026-00001","repository_id":"repo://example/api"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/supply-chain/impact/suppressions", strings.NewReader(body))
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:          AuthModeShared,
		SubjectClass:  "shared_token",
		SubjectIDHash: "sha256:operator",
		AllScopes:     true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusCreated; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if store.value.Source != "eshu_policy" {
		t.Fatalf("Source = %q, want eshu_policy", store.value.Source)
	}
	if store.value.Author != "shared_token:sha256:operator" {
		t.Fatalf("Author = %q, want authenticated subject hash", store.value.Author)
	}
	if store.value.Scope.CVEID == nil || *store.value.Scope.CVEID != "CVE-2026-00001" {
		t.Fatalf("Scope.CVEID = %#v, want CVE-2026-00001", store.value.Scope.CVEID)
	}
	var response VulnerabilitySuppressionMutationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal(response): %v", err)
	}
	if response.Status != "created" || response.GenerationID != "suppression-generation-1" {
		t.Fatalf("response = %#v, want created generation", response)
	}
}

func TestCreateVulnerabilitySuppressionReturnsUnchangedOnIdenticalRetry(t *testing.T) {
	t.Parallel()

	store := &recordingVulnerabilitySuppressionMutationStore{
		result: VulnerabilitySuppressionMutationResult{
			SuppressionID: "suppression-CVE-2026-00001",
			GenerationID:  "suppression-generation-1",
			Changed:       false,
		},
	}
	handler := &SupplyChainHandler{SuppressionMutations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/supply-chain/impact/suppressions",
		strings.NewReader(`{"suppression_id":"suppression-CVE-2026-00001","justification":"accepted_risk","authored_at":"2026-07-27T12:00:00Z","reason":"verified","scope":{"cve_id":"CVE-2026-00001"}}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:         AuthModeShared,
		SubjectClass: "shared_token",
		AllScopes:    true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var response VulnerabilitySuppressionMutationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal(response): %v", err)
	}
	if response.Status != "unchanged" {
		t.Fatalf("Status = %q, want unchanged", response.Status)
	}
}

func TestCreateVulnerabilitySuppressionFailsClosedForInvalidOrScopedRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		body       string
		auth       AuthContext
		wantStatus int
	}{
		{
			name:       "empty scope",
			body:       `{"suppression_id":"suppression-1","justification":"accepted_risk","authored_at":"2026-07-27T12:00:00Z","reason":"verified","scope":{}}`,
			auth:       AuthContext{Mode: AuthModeShared, SubjectClass: "shared_token", AllScopes: true},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed expiry",
			body:       `{"suppression_id":"suppression-1","justification":"ignored","authored_at":"2026-07-27T12:00:00Z","expires_at":"later","reason":"temporary","scope":{"cve_id":"CVE-2026-00001"}}`,
			auth:       AuthContext{Mode: AuthModeShared, SubjectClass: "shared_token", AllScopes: true},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "scoped caller",
			body:       `{"suppression_id":"suppression-1","justification":"accepted_risk","authored_at":"2026-07-27T12:00:00Z","reason":"verified","scope":{"cve_id":"CVE-2026-00001"}}`,
			auth:       AuthContext{Mode: AuthModeScoped, SubjectClass: "api_token", AllScopes: false},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &recordingVulnerabilitySuppressionMutationStore{}
			handler := &SupplyChainHandler{SuppressionMutations: store}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/supply-chain/impact/suppressions", strings.NewReader(tc.body))
			req = req.WithContext(ContextWithAuthContext(req.Context(), tc.auth))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if store.calls != 0 {
				t.Fatalf("store calls = %d, want 0 on rejected request", store.calls)
			}
		})
	}
}
