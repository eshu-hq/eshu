// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incident

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// failingIncidentContextStore fails if its read is reached, proving a scoped
// denial fails closed before any incident context is served.
type failingIncidentContextStore struct {
	called bool
}

func (s *failingIncidentContextStore) ReadIncidentContext(
	context.Context,
	model.IncidentContextFilter,
) (model.IncidentContextSnapshot, error) {
	s.called = true
	return model.IncidentContextSnapshot{}, errors.New("incident context read reached under fail-closed scoped token")
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

func TestIncidentContextScopedEmptyGrantReturnsNotFoundWithoutReads(t *testing.T) {
	t.Parallel()

	store := &failingIncidentContextStore{}
	authorizer := &recordingIncidentRepositoryAuthorizer{repositories: []string{"repo-team-a"}}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:        queryauth.AuthModeScoped,
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
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?provider=pagerduty&scope_id=pd-prod&limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
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
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
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
		snapshot: model.IncidentContextSnapshot{
			Query: model.IncidentContextQuery{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				ServiceID:          "P-SVC",
				Limit:              6,
			},
			Incident: model.IncidentContextIncident{
				Provider:           "pagerduty",
				ProviderIncidentID: "PABC123",
				Title:              "checkout-api elevated errors",
				Service:            model.IncidentContextReference{ID: "P-SVC", Summary: "checkout-api"},
				EvidenceFactID:     "incident-fact",
			},
		},
	}
	authorizer := &recordingIncidentRepositoryAuthorizer{repositories: []string{"repo-team-a"}}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=5", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
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
		snapshot: model.IncidentContextSnapshot{
			Query:    model.IncidentContextQuery{Provider: "pagerduty", ProviderIncidentID: "PABC123", Limit: 6},
			Incident: model.IncidentContextIncident{Provider: "pagerduty", ProviderIncidentID: "PABC123", EvidenceFactID: "incident-fact"},
		},
	}
	authorizer := &recordingIncidentRepositoryAuthorizer{err: errors.New("authorizer must not run for shared tokens")}
	handler := &IncidentHandler{Context: store, Authorizer: authorizer, Profile: querycontract.ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/incidents/PABC123/context?limit=5", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{Mode: queryauth.AuthModeShared, SubjectClass: "shared_token", AllScopes: true}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if authorizer.called {
		t.Fatal("authorizer consulted for shared (unscoped) token")
	}
}

func assertNoIncidentIdentifierLeak(t *testing.T, body []byte) {
	t.Helper()

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode not-found envelope: %v; body = %s", err, string(body))
	}
	if envelope.Error == nil {
		t.Fatalf("not-found envelope missing error: %s", string(body))
	}
	if got, want := envelope.Error.Code, querycontract.ErrorCodeNotFound; got != want {
		t.Fatalf("error code = %q, want %q", got, want)
	}
	if envelope.Error.Details != nil {
		t.Fatalf("not-found envelope leaked details: %#v", envelope.Error.Details)
	}
}
