// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestResolveEntityGraphAppliesScopedAuthBeforeLimit(t *testing.T) {
	t.Parallel()

	content := &recordingEntityResolveContentStore{byRepo: map[string][]EntityContent{"repo-team-a": {{
		EntityID: "entity-a", RepoID: "repo-team-a", EntityName: "HandlePayment", EntityType: "Function", RelativePath: "payments/handler.go", Language: "go",
	}}}}
	handler := &EntityHandler{Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"HandlePayment","type":"function","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeEntityResolveAuthzBody(t, rec)
	if got, want := body["count"], float64(1); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], float64(5); got != want {
		t.Fatalf("limit = %#v, want %#v", got, want)
	}
	if got := body["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
}

func TestResolveEntityContentAppliesScopedAuthWithoutAnyRepoFallback(t *testing.T) {
	t.Parallel()

	content := &recordingEntityResolveContentStore{
		byRepo: map[string][]EntityContent{
			"repo-team-a": {{
				RepoID:       "repo-team-a",
				EntityID:     "entity-a",
				EntityName:   "HandlePayment",
				EntityType:   "Function",
				RelativePath: "payments/handler.go",
				Language:     "go",
			}},
		},
	}
	handler := &EntityHandler{Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"HandlePayment","type":"function","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if content.anyRepoNameCalls != 0 {
		t.Fatalf("anyRepoNameCalls = %d, want 0", content.anyRepoNameCalls)
	}
	if got, want := content.repoNameCalls, []string{"repo-team-a"}; !slices.Equal(got, want) {
		t.Fatalf("repoNameCalls = %#v, want %#v", got, want)
	}
	body := decodeEntityResolveAuthzBody(t, rec)
	if got, want := body["count"], float64(1); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
}

func TestResolveEntityEmptyGrantReturnsEmptyWithoutBroadScan(t *testing.T) {
	t.Parallel()

	content := &recordingEntityResolveContentStore{}
	handler := &EntityHandler{Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"HandlePayment","type":"function","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if content.anyRepoNameCalls != 0 || len(content.repoNameCalls) != 0 {
		t.Fatalf("content calls = %+v, want no broad or scoped calls", content)
	}
	body := decodeEntityResolveAuthzBody(t, rec)
	if got, want := body["count"], float64(0); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got := body["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
	entities := body["entities"].([]any)
	if got := len(entities); got != 0 {
		t.Fatalf("len(entities) = %d, want 0", got)
	}
}

func TestResolveEntityAllScopeAdminKeepsAnyRepoFallback(t *testing.T) {
	t.Parallel()

	content := &recordingEntityResolveContentStore{
		anyRepo: []EntityContent{{
			RepoID:       "repo-team-a",
			EntityID:     "entity-a",
			EntityName:   "HandlePayment",
			EntityType:   "Function",
			RelativePath: "payments/handler.go",
			Language:     "go",
		}},
	}
	handler := &EntityHandler{Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"HandlePayment","type":"function","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-admin",
		WorkspaceID: "workspace-admin",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if content.anyRepoNameCalls != 1 {
		t.Fatalf("anyRepoNameCalls = %d, want 1", content.anyRepoNameCalls)
	}
}

func TestResolveEntityScopedSelectorFiltersDuplicateRepositoryNames(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Content: fakePortContentStore{repositories: tenantAuthzRepositories()},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"HandlePayment","repo_id":"payments","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after scoped duplicate filtering; body = %s", rec.Code, rec.Body.String())
	}
}

func TestResolveEntityScopedSelectorDeniesOutOfScopeCanonicalID(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Content: fakePortContentStore{repositories: tenantAuthzRepositories()},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"HandlePayment","repo_id":"repo-team-b","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d for out-of-scope repo; body = %s", got, want, rec.Body.String())
	}
}

func TestResolveEntityEmptyGrantDeniesExplicitRepositorySelector(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Content: fakePortContentStore{repositories: tenantAuthzRepositories()},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"HandlePayment","repo_id":"repo-team-a","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.resolveEntity(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d for explicit selector with empty grant; body = %s", got, want, rec.Body.String())
	}
}

func decodeEntityResolveAuthzBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	return body
}

type recordingEntityResolveContentStore struct {
	fakePortContentStore
	byRepo           map[string][]EntityContent
	anyRepo          []EntityContent
	repoNameCalls    []string
	anyRepoNameCalls int
}

func (s *recordingEntityResolveContentStore) SearchEntityNames(_ context.Context, search EntityNameSearch) ([]EntityContent, error) {
	if search.Scope == EntityNameScopeAll {
		s.anyRepoNameCalls++
		return limitEntityContent(s.anyRepo, search.Limit), nil
	}
	rows := make([]EntityContent, 0)
	for _, repoID := range search.RepositoryIDs {
		s.repoNameCalls = append(s.repoNameCalls, repoID)
		rows = append(rows, s.byRepo[repoID]...)
	}
	return limitEntityContent(rows, search.Limit), nil
}

func (s *recordingEntityResolveContentStore) SearchEntitiesByName(
	_ context.Context,
	repoID string,
	_ string,
	_ string,
	limit int,
) ([]EntityContent, error) {
	s.repoNameCalls = append(s.repoNameCalls, repoID)
	return limitEntityContent(s.byRepo[repoID], limit), nil
}

func (s *recordingEntityResolveContentStore) SearchEntitiesByNameAnyRepo(
	context.Context,
	string,
	string,
	int,
) ([]EntityContent, error) {
	s.anyRepoNameCalls++
	return append([]EntityContent(nil), s.anyRepo...), nil
}
