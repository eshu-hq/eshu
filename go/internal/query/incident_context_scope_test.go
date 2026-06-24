// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// failingIncidentContextStore fails if its read is reached, proving a scoped
// denial fails closed before any incident context is served.
type failingIncidentContextStore struct {
	called bool
}

func (s *failingIncidentContextStore) ReadIncidentContext(
	context.Context,
	IncidentContextFilter,
) (IncidentContextSnapshot, error) {
	s.called = true
	return IncidentContextSnapshot{}, errors.New("incident context read reached under fail-closed scoped token")
}

// recordingIncidentRepositoryAuthorizer returns a fixed durable repository set
// and records the resolved arguments.
type recordingIncidentRepositoryAuthorizer struct {
	repositories       []string
	err                error
	called             bool
	provider           string
	providerIncidentID string
	scopeID            string
}

func (a *recordingIncidentRepositoryAuthorizer) ResolveDurableIncidentRepositories(
	_ context.Context,
	provider string,
	providerIncidentID string,
	scopeID string,
) ([]string, error) {
	a.called = true
	a.provider = provider
	a.providerIncidentID = providerIncidentID
	a.scopeID = scopeID
	return a.repositories, a.err
}

func TestAuthMiddlewareWithScopedTokensAllowsIncidentContextRoute(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthContextFromContext(r.Context()); !ok {
			t.Fatal("AuthContextFromContext() ok = false, want true")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=10", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNoContent; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestAuthMiddlewareWithScopedTokensRejectsAdjacentIncidentRoutes(t *testing.T) {
	t.Parallel()

	resolver := &fakeScopedTokenResolver{
		context: AuthContext{
			Mode:                 AuthModeScoped,
			TenantID:             "tenant-a",
			WorkspaceID:          "workspace-a",
			AllowedRepositoryIDs: []string{"repo-team-a"},
		},
		ok: true,
	}
	handler := AuthMiddlewareWithScopedTokens("", resolver, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, target := range []string{
		"/api/v0/incidents/PABC123/timeline?limit=10",
		"/api/v0/incidents/PABC123/context/extra",
		"/api/v0/incidents//context?limit=10",
		"/api/v0/incidents/PABC123/context/sub/context",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusForbidden; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestIncidentContextScopedEmptyGrantReturnsNotFoundWithoutReads(t *testing.T) {
	t.Parallel()

	store := &failingIncidentContextStore{}
	authorizer := &recordingIncidentRepositoryAuthorizer{repositories: []string{"repo-team-a"}}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("incident context store read for empty scoped grant")
	}
	if authorizer.called {
		t.Fatal("authorizer consulted for empty scoped grant")
	}
	assertNoIncidentIdentifierLeak(t, rec.Body.Bytes())
}

func TestIncidentContextScopedOutOfGrantReturnsNotFoundWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingIncidentContextStore{}
	authorizer := &recordingIncidentRepositoryAuthorizer{repositories: []string{"repo-owner-x"}}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod&limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("incident context store read for out-of-grant durable repository")
	}
	if !authorizer.called {
		t.Fatal("authorizer not consulted for non-empty scoped grant")
	}
	if got, want := authorizer.provider, "pagerduty"; got != want {
		t.Fatalf("authorizer provider = %q, want %q", got, want)
	}
	if got, want := authorizer.providerIncidentID, "PABC123"; got != want {
		t.Fatalf("authorizer provider incident id = %q, want %q", got, want)
	}
	if got, want := authorizer.scopeID, "pd-prod"; got != want {
		t.Fatalf("authorizer scope id = %q, want %q", got, want)
	}
	if strings.Contains(rec.Body.String(), "repo-owner-x") || strings.Contains(rec.Body.String(), "PABC123") {
		t.Fatalf("out-of-grant response leaked an identifier: %s", rec.Body.String())
	}
}

func TestIncidentContextScopedNoDurableEdgeReturnsNotFound(t *testing.T) {
	t.Parallel()

	store := &failingIncidentContextStore{}
	authorizer := &recordingIncidentRepositoryAuthorizer{repositories: nil}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=10", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("incident context store read for incident with no durable edge")
	}
	if !authorizer.called {
		t.Fatal("authorizer not consulted for non-empty scoped grant")
	}
	assertNoIncidentIdentifierLeak(t, rec.Body.Bytes())
}

func TestIncidentContextScopedInGrantServesContext(t *testing.T) {
	t.Parallel()

	store := &recordingIncidentContextStore{
		snapshot: IncidentContextSnapshot{
			Query: IncidentContextQuery{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				ServiceID:          "P-SVC",
				Limit:              6,
			},
			Incident: IncidentContextIncident{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				Title:              "checkout-api elevated errors",
				Service:            IncidentContextReference{ID: "P-SVC", Summary: "checkout-api"},
				EvidenceFactID:     "incident-fact",
			},
		},
	}
	authorizer := &recordingIncidentRepositoryAuthorizer{repositories: []string{"repo-team-a"}}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=5", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.lastFilter.ProviderIncidentID == "" {
		t.Fatal("store read not reached for in-grant scoped incident")
	}
	if !authorizer.called {
		t.Fatal("authorizer not consulted for in-grant scoped incident")
	}
}

func TestIncidentContextSharedTokenSkipsAuthorizer(t *testing.T) {
	t.Parallel()

	store := &recordingIncidentContextStore{
		snapshot: IncidentContextSnapshot{
			Query:    IncidentContextQuery{Provider: "pagerduty", ProviderIncidentID: "PABC123", Limit: 6},
			Incident: IncidentContextIncident{Provider: "pagerduty", ProviderIncidentID: "PABC123", EvidenceFactID: "incident-fact"},
		},
	}
	authorizer := &recordingIncidentRepositoryAuthorizer{err: errors.New("authorizer must not run for shared tokens")}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=5", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), sharedAuthContext()))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if authorizer.called {
		t.Fatal("authorizer consulted for shared (unscoped) token")
	}
}

func TestResolveDurableIncidentRepositoriesQueryShape(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		"fact.fact_kind = 'incident.record'",
		"fact.payload->'service'->>'id'",
		"correlation.fact_kind = 'reducer_incident_repository_correlation'",
		"correlation.payload->>'provider_service_id'",
		"correlation.payload->>'provenance_only' = 'false'",
		"NULLIF(correlation.payload->>'repository_id', '') IS NOT NULL",
		"generation.status = 'active'",
	} {
		if !strings.Contains(resolveDurableIncidentRepositoriesQuery, fragment) {
			t.Fatalf("durable incident repository query missing %q:\n%s", fragment, resolveDurableIncidentRepositoriesQuery)
		}
	}
}

func TestPostgresIncidentRepositoryAuthorizerBlankInputsSkipRead(t *testing.T) {
	t.Parallel()

	authorizer := PostgresIncidentRepositoryAuthorizer{DB: unusedIncidentContextQueryer{}}
	repositories, err := authorizer.ResolveDurableIncidentRepositories(context.Background(), "", "", "")
	if err != nil {
		t.Fatalf("ResolveDurableIncidentRepositories() error = %v, want nil", err)
	}
	if len(repositories) != 0 {
		t.Fatalf("repositories = %#v, want empty", repositories)
	}
}

func assertNoIncidentIdentifierLeak(t *testing.T, body []byte) {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode not-found envelope: %v; body = %s", err, string(body))
	}
	if envelope.Error == nil {
		t.Fatalf("not-found envelope missing error: %s", string(body))
	}
	if got, want := envelope.Error.Code, ErrorCodeNotFound; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if envelope.Error.Details != nil {
		t.Fatalf("not-found envelope leaked details: %#v", envelope.Error.Details)
	}
}
