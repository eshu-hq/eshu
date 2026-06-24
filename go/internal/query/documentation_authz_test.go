// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddlewareWithScopedTokensAllowsDocumentationListRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "findings", method: http.MethodGet, path: "/api/v0/documentation/findings"},
		{name: "facts", method: http.MethodGet, path: "/api/v0/documentation/facts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:                 AuthModeScoped,
					TenantID:             "tenant_a",
					WorkspaceID:          "workspace_a",
					AllowedRepositoryIDs: []string{"repository:team-a"},
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsDocumentationAggregateRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "count", method: http.MethodGet, path: "/api/v0/documentation/findings/count"},
		{name: "inventory", method: http.MethodGet, path: "/api/v0/documentation/findings/inventory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:                 AuthModeScoped,
					TenantID:             "tenant_a",
					WorkspaceID:          "workspace_a",
					AllowedRepositoryIDs: []string{"repository:team-a"},
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestAuthMiddlewareWithScopedTokensAllowsDocumentationEvidencePacketRoutes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "packet by finding",
			method: http.MethodGet,
			path:   "/api/v0/documentation/findings/finding:docs:1/evidence-packet",
		},
		{
			name:   "packet freshness",
			method: http.MethodGet,
			path:   "/api/v0/documentation/evidence-packets/doc-packet:1/freshness",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolver := &fakeScopedTokenResolver{
				context: AuthContext{
					Mode:                 AuthModeScoped,
					TenantID:             "tenant_a",
					WorkspaceID:          "workspace_a",
					AllowedRepositoryIDs: []string{"repository:team-a"},
				},
				ok: true,
			}
			handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := AuthContextFromContext(r.Context()); !ok {
					t.Fatal("AuthContextFromContext() ok = false, want true")
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestDocumentationHandlerScopedEmptyGrantReturnsEmptyListsWithoutRead(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "findings", path: "/api/v0/documentation/findings?limit=2"},
		{name: "facts", path: "/api/v0/documentation/facts?fact_kind=source&limit=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := &DocumentationHandler{
				Content: fakePortContentStore{
					documentationFindingsErr: errors.New("broad documentation findings read"),
					documentationFactsErr:    errors.New("broad documentation facts read"),
				},
				Profile: ProfileProduction,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
				Mode:        AuthModeScoped,
				TenantID:    "tenant_a",
				WorkspaceID: "workspace_a",
			}))
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, `"truth"`) {
				t.Fatalf("body missing truth envelope: %s", body)
			}
			if !strings.Contains(body, `[]`) {
				t.Fatalf("body missing empty list: %s", body)
			}
		})
	}
}

func TestDocumentationListSQLAppliesScopedAuthorizationBeforePaging(t *testing.T) {
	t.Parallel()

	findingsSQL, findingsArgs := buildDocumentationFindingsSQL(documentationFindingFilter{
		AllowedRepositoryIDs: []string{"repository:team-a", "repository:team-a"},
		AllowedScopeIDs:      []string{"scope:team-a"},
		Limit:                10,
	})
	assertDocumentationAuthorizationPredicate(t, findingsSQL, "fact_records", "ingestion_scopes")
	if got, want := len(findingsArgs), 4; got != want {
		t.Fatalf("findings args len = %d, want %d", got, want)
	}

	factsSQL, factsArgs := buildDocumentationFactsSQL(documentationFactFilter{
		FactKind:             "documentation_section",
		AllowedRepositoryIDs: []string{"repository:team-a", "repository:team-a"},
		AllowedScopeIDs:      []string{"scope:team-a"},
		Limit:                10,
	})
	assertDocumentationAuthorizationPredicate(t, factsSQL, "fact_records", "ingestion_scopes")
	if got, want := len(factsArgs), 5; got != want {
		t.Fatalf("facts args len = %d, want %d", got, want)
	}
}

func assertDocumentationAuthorizationPredicate(
	t *testing.T,
	query string,
	factAlias string,
	scopeAlias string,
) {
	t.Helper()

	for _, want := range []string{
		factAlias + ".scope_id IN (",
		scopeAlias + ".payload->>'repo' IN (",
		factAlias + ".payload->'candidate_refs'",
		factAlias + ".payload->'linked_entities'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("documentation query missing scoped authorization fragment %q:\n%s", want, query)
		}
	}
	orderIndex := strings.Index(query, "ORDER BY")
	if orderIndex >= 0 && strings.Index(query, factAlias+".scope_id IN (") > orderIndex {
		t.Fatalf("scoped authorization predicate appears after ORDER BY:\n%s", query)
	}
}
