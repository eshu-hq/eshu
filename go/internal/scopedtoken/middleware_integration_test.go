// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// TestScopedTokenRegistryEnforcesIsolationThroughMiddleware proves the full
// keystone path for #1852: a registry-resolved per-team token flows through
// query.AuthMiddlewareWithScopedTokensAndGovernanceAudit, lands a scoped
// AuthContext carrying only the team's grants on an allowlisted route, is
// refused on a not-yet-enabled scoped route, and never displaces shared-token
// or unauthenticated handling.
func TestScopedTokenRegistryEnforcesIsolationThroughMiddleware(t *testing.T) {
	t.Parallel()

	const teamToken = "team-payments-secret-token"
	path := writeRegistryFile(t, `{
      "version": 1,
      "tokens": [
        {
          "token_sha256": "`+tokenHash(teamToken)+`",
          "tenant_id": "team-payments",
          "workspace_id": "team-payments",
          "subject_class": "team_token",
          "all_scopes": false,
          "allowed_repository_ids": ["repo://acme/payments"]
        }
      ]
    }`)
	registry, err := LoadRegistryFromFile(path)
	if err != nil {
		t.Fatalf("LoadRegistryFromFile: %v", err)
	}

	var sawAuth query.AuthContext
	var sawAuthOK bool
	mux := http.NewServeMux()
	// /api/v0/repositories is an allowlisted scoped route.
	mux.HandleFunc("GET /api/v0/repositories", func(w http.ResponseWriter, r *http.Request) {
		sawAuth, sawAuthOK = query.AuthContextFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	// A route NOT on the scoped allowlist must be refused for scoped tokens.
	mux.HandleFunc("GET /api/v0/admin/secrets", func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("scoped token reached a non-allowlisted route")
		w.WriteHeader(http.StatusOK)
	})

	handler := query.AuthMiddlewareWithScopedTokensAndGovernanceAudit("shared-admin-key", registry, mux, nil)

	t.Run("scoped token lands scoped AuthContext on allowlisted route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
		req.Header.Set("Authorization", "Bearer "+teamToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusNoContent; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if !sawAuthOK {
			t.Fatal("handler did not observe an AuthContext")
		}
		if sawAuth.Mode != query.AuthModeScoped {
			t.Fatalf("Mode = %q, want scoped", sawAuth.Mode)
		}
		if sawAuth.AllScopes {
			t.Fatal("AllScopes = true, want false for a team token")
		}
		if len(sawAuth.AllowedRepositoryIDs) != 1 || sawAuth.AllowedRepositoryIDs[0] != "repo://acme/payments" {
			t.Fatalf("AllowedRepositoryIDs = %#v, want [repo://acme/payments]", sawAuth.AllowedRepositoryIDs)
		}
	})

	t.Run("scoped token refused on non-allowlisted route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/secrets", nil)
		req.Header.Set("Authorization", "Bearer "+teamToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusForbidden; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
	})

	t.Run("unknown token is unauthenticated", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
		req.Header.Set("Authorization", "Bearer not-a-real-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusUnauthorized; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
	})

	t.Run("shared admin key keeps all-scope access", func(t *testing.T) {
		sawAuthOK = false
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories", nil)
		req.Header.Set("Authorization", "Bearer shared-admin-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusNoContent; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
		}
		if !sawAuthOK || sawAuth.Mode != query.AuthModeShared || !sawAuth.AllScopes {
			t.Fatalf("shared key auth = %#v (ok=%v), want shared all-scope", sawAuth, sawAuthOK)
		}
	})
}
