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
	"strings"
	"testing"
)

func TestCodeSearchGraphAppliesScopedAuthBeforeLimit(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "allowed_repository_ids") {
				t.Fatalf("code search query missing scoped repository predicate:\n%s", cypher)
			}
			allowed, ok := params["allowed_repository_ids"].([]string)
			if !ok || len(allowed) != 1 || allowed[0] != "repo-team-a" {
				t.Fatalf("allowed_repository_ids = %#v, want repo-team-a", params["allowed_repository_ids"])
			}
			return []map[string]any{{
				"entity_id":  "entity-a",
				"name":       "HandlePayment",
				"labels":     []any{"Function"},
				"file_path":  "payments/handler.go",
				"repo_id":    "repo-team-a",
				"repo_name":  "payments",
				"language":   "go",
				"start_line": int64(12),
				"end_line":   int64(20),
			}}, nil
		},
	}
	handler := &CodeHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"Handle","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.handleSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeCodeSearchAuthzBody(t, rec)
	matches := body["matches"].([]any)
	if got, want := len(matches), 1; got != want {
		t.Fatalf("len(matches) = %d, want %d", got, want)
	}
}

func TestCodeSearchContentAppliesScopedAuthWithoutAnyRepoFallback(t *testing.T) {
	t.Parallel()

	content := &recordingCodeSearchContentStore{
		byRepo: map[string][]EntityContent{
			"repo-team-a": {{
				RepoID:       "repo-team-a",
				EntityID:     "entity-a",
				EntityName:   "HandlePayment",
				EntityType:   "function",
				RelativePath: "payments/handler.go",
				Language:     "go",
			}},
		},
	}
	handler := &CodeHandler{Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"Handle","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.handleSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if content.anyRepoNameCalls != 0 || content.anyRepoSourceCalls != 0 {
		t.Fatalf("any-repo calls = name:%d source:%d, want none", content.anyRepoNameCalls, content.anyRepoSourceCalls)
	}
	if got, want := content.repoNameCalls, []string{"repo-team-a"}; !slices.Equal(got, want) {
		t.Fatalf("repoNameCalls = %#v, want %#v", got, want)
	}
	body := decodeCodeSearchAuthzBody(t, rec)
	matches := body["matches"].([]any)
	if got, want := len(matches), 1; got != want {
		t.Fatalf("len(matches) = %d, want %d", got, want)
	}
}

func TestCodeSearchContentEmptyGrantReturnsEmptyWithoutBroadScan(t *testing.T) {
	t.Parallel()

	content := &recordingCodeSearchContentStore{}
	handler := &CodeHandler{Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"Handle","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.handleSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if content.anyRepoNameCalls != 0 || content.anyRepoSourceCalls != 0 ||
		len(content.repoNameCalls) != 0 || len(content.repoSourceCalls) != 0 {
		t.Fatalf("content calls = %+v, want no broad or scoped calls", content)
	}
	body := decodeCodeSearchAuthzBody(t, rec)
	matches := body["matches"].([]any)
	if got := len(matches); got != 0 {
		t.Fatalf("len(matches) = %d, want 0", got)
	}
}

func TestCodeSearchAllScopeAdminKeepsAnyRepoFallback(t *testing.T) {
	t.Parallel()

	content := &recordingCodeSearchContentStore{
		anyRepo: []EntityContent{{
			RepoID:       "repo-team-a",
			EntityID:     "entity-a",
			EntityName:   "HandlePayment",
			EntityType:   "function",
			RelativePath: "payments/handler.go",
			Language:     "go",
		}},
	}
	handler := &CodeHandler{Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/search",
		bytes.NewBufferString(`{"query":"Handle","limit":5}`),
	)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-admin",
		WorkspaceID: "workspace-admin",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	handler.handleSearch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if content.anyRepoNameCalls != 1 || content.anyRepoSourceCalls != 1 {
		t.Fatalf("any-repo calls = name:%d source:%d, want one each", content.anyRepoNameCalls, content.anyRepoSourceCalls)
	}
}

func TestCodeSearchScopedSelectorFiltersDuplicateRepositoryNames(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content: fakePortContentStore{repositories: tenantAuthzRepositories()},
	}
	ctx := ContextWithAuthContext(context.Background(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	})

	repoID, err := handler.resolveRepositorySelector(ctx, "payments")
	if err != nil {
		t.Fatalf("resolveRepositorySelector() error = %v, want nil", err)
	}
	if got, want := repoID, "repo-team-a"; got != want {
		t.Fatalf("repoID = %q, want %q", got, want)
	}
}

func TestCodeSearchScopedSelectorDeniesOutOfScopeCanonicalID(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content: fakePortContentStore{repositories: tenantAuthzRepositories()},
	}
	ctx := ContextWithAuthContext(context.Background(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	})

	_, err := handler.resolveRepositorySelector(ctx, "repo-team-b")
	if err == nil {
		t.Fatal("resolveRepositorySelector() error = nil, want out-of-scope not found")
	}
	if !isRepositorySelectorNotFound(err) {
		t.Fatalf("resolveRepositorySelector() error = %T %v, want not found", err, err)
	}
}

func decodeCodeSearchAuthzBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	return body
}

type recordingCodeSearchContentStore struct {
	fakePortContentStore
	byRepo             map[string][]EntityContent
	anyRepo            []EntityContent
	repoNameCalls      []string
	repoSourceCalls    []string
	anyRepoNameCalls   int
	anyRepoSourceCalls int
}

func (s *recordingCodeSearchContentStore) SearchEntitiesByName(
	_ context.Context,
	repoID string,
	_ string,
	_ string,
	limit int,
) ([]EntityContent, error) {
	s.repoNameCalls = append(s.repoNameCalls, repoID)
	return limitEntityContent(s.byRepo[repoID], limit), nil
}

func (s *recordingCodeSearchContentStore) SearchEntityContent(
	_ context.Context,
	repoID string,
	_ string,
	limit int,
) ([]EntityContent, error) {
	s.repoSourceCalls = append(s.repoSourceCalls, repoID)
	return limitEntityContent(s.byRepo[repoID], limit), nil
}

func (s *recordingCodeSearchContentStore) SearchEntitiesByNameAnyRepo(
	context.Context,
	string,
	string,
	int,
) ([]EntityContent, error) {
	s.anyRepoNameCalls++
	return append([]EntityContent(nil), s.anyRepo...), nil
}

func (s *recordingCodeSearchContentStore) SearchEntityContentAnyRepo(
	context.Context,
	string,
	int,
) ([]EntityContent, error) {
	s.anyRepoSourceCalls++
	return append([]EntityContent(nil), s.anyRepo...), nil
}

func limitEntityContent(rows []EntityContent, limit int) []EntityContent {
	if limit > 0 && limit < len(rows) {
		return append([]EntityContent(nil), rows[:limit]...)
	}
	return append([]EntityContent(nil), rows...)
}
