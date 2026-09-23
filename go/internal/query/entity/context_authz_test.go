// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestGetEntityContextGraphAppliesScopedAuthBeforeReturn(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			// #7006: the scoped grant is no longer embedded in this query's
			// Cypher (chaining a second hop onto the file/repo OPTIONAL MATCH
			// to reach a Repository node to filter is itself live-proven
			// catastrophic on the pinned NornicDB build). The real,
			// unchanged security boundary is the Go-side
			// access.AllowsRepositoryID(repo_id) check after this read
			// returns, covered by TestGetEntityContextGrantDeniedEmitsTelemetry
			// and the not-found path it drives.
			graph.AssertCypherHasNoBrokenAndOr(t, cypher)
			return map[string]any{
				"id":            "entity-a",
				"labels":        []any{"Function"},
				"name":          "HandlePayment",
				"file_path":     "payments/handler.go",
				"repo_id":       "repo-team-a",
				"repo_name":     "payments",
				"language":      "go",
				"start_line":    int64(12),
				"end_line":      int64(20),
				"relationships": []any{},
			}, nil
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestGetEntityContextEmptyGrantReturnsNotFoundWithoutBackendCalls(t *testing.T) {
	t.Parallel()

	reader := &recordingEntityContextGraphReader{}
	content := &recordingEntityContextContentStore{}
	handler := &Handler{Neo4j: reader, Content: content, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:        auth.AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if reader.runSingleCalls != 0 || reader.runCalls != 0 {
		t.Fatalf("graph calls = runSingle:%d run:%d, want none", reader.runSingleCalls, reader.runCalls)
	}
	if content.getEntityCalls != 0 || content.listRepoEntitiesCalls != 0 {
		t.Fatalf("content calls = get:%d list:%d, want none", content.getEntityCalls, content.listRepoEntitiesCalls)
	}
}

func TestGetEntityContextContentFallbackFiltersOutOfScopeEntity(t *testing.T) {
	t.Parallel()

	content := &recordingEntityContextContentStore{
		entity: &querycontract.EntityContent{
			RepoID:       "repo-team-b",
			EntityID:     "entity-b",
			EntityName:   "HandlePayment",
			EntityType:   "Function",
			RelativePath: "payments/handler.go",
			Language:     "go",
		},
	}
	handler := &Handler{Content: content, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-b/context", nil)
	req.SetPathValue("entity_id", "entity-b")
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := content.getEntityCalls, 1; got != want {
		t.Fatalf("getEntityCalls = %d, want %d", got, want)
	}
	if content.listRepoEntitiesCalls != 0 {
		t.Fatalf("listRepoEntitiesCalls = %d, want 0 before relationship hydration", content.listRepoEntitiesCalls)
	}
}

type recordingEntityContextGraphReader struct {
	runCalls       int
	runSingleCalls int
}

func (r *recordingEntityContextGraphReader) Run(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	r.runCalls++
	return nil, nil
}

func (r *recordingEntityContextGraphReader) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	r.runSingleCalls++
	return nil, nil
}

type recordingEntityContextContentStore struct {
	content.FakePortContentStore
	entity                *querycontract.EntityContent
	getEntityCalls        int
	listRepoEntitiesCalls int
}

func (s *recordingEntityContextContentStore) GetEntityContent(
	context.Context,
	string,
) (*querycontract.EntityContent, error) {
	s.getEntityCalls++
	if s.entity == nil {
		return nil, nil
	}
	entity := *s.entity
	return &entity, nil
}

func (s *recordingEntityContextContentStore) ListRepoEntities(
	context.Context,
	string,
	int,
) ([]querycontract.EntityContent, error) {
	s.listRepoEntitiesCalls++
	return nil, nil
}
