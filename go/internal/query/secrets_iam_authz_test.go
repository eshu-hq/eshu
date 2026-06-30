// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsSecretsIAMRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/api/v0/secrets-iam/identity-trust-chains",
		"/api/v0/secrets-iam/privilege-posture-observations",
		"/api/v0/secrets-iam/secret-access-paths",
		"/api/v0/secrets-iam/posture-gaps",
		"/api/v0/secrets-iam/posture-summary",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if !scopedHTTPRouteSupportsTenantFilter(req) {
				t.Fatalf("scopedHTTPRouteSupportsTenantFilter(GET %s) = false, want true", path)
			}
		})
	}
}

func TestSecretsIAMPostureSummaryScopedGrantUsesInGrantScope(t *testing.T) {
	t.Parallel()

	store := &recordingPostureSummaryStore{}
	handler := &SecretsIAMHandler{Summary: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-a", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"scope-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastScopeID != "scope-a" {
		t.Fatalf("store scope = %q, want scope-a", store.lastScopeID)
	}
}

func TestSecretsIAMPostureSummaryScopedGrantRefusesOutOfGrantBeforeRead(t *testing.T) {
	t.Parallel()

	store := &recordingPostureSummaryStore{}
	handler := &SecretsIAMHandler{Summary: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-b", nil)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"scope-a"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastScopeID != "" {
		t.Fatalf("store was called for out-of-grant scope %q", store.lastScopeID)
	}
	if strings.Contains(w.Body.String(), "scope-b") {
		t.Fatalf("out-of-grant response leaked requested scope: %s", w.Body.String())
	}
}
