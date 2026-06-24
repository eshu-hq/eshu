// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetWorkloadContextGraphAppliesScopedAuthBeforeReturn(t *testing.T) {
	t.Parallel()

	reader := fakeWorkloadGraphReader{
		runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (w:Workload)") && strings.Contains(cypher, "w.id = $workload_id") {
				requireScopedWorkloadPredicate(t, cypher, params)
				return map[string]any{
					"id":      "workload:payments",
					"name":    "payments",
					"kind":    "service",
					"repo_id": "repo-team-a",
				}, nil
			}
			if strings.Contains(cypher, "MATCH (r:Repository") {
				return map[string]any{"repo_name": "payments"}, nil
			}
			return nil, nil
		},
		runByMatch: map[string][]map[string]any{
			"INSTANCE_OF":                         {},
			"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
			"K8sResource OR":                      {},
		},
	}
	handler := &EntityHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload:payments/context", nil)
	req.SetPathValue("workload_id", "workload:payments")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.getWorkloadContext(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestGetWorkloadContextEmptyGrantReturnsNotFoundWithoutBackendCalls(t *testing.T) {
	t.Parallel()

	reader := &recordingServiceContextGraphReader{}
	content := &recordingServiceContextContentStore{}
	handler := &EntityHandler{Neo4j: reader, Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload:payments/context", nil)
	req.SetPathValue("workload_id", "workload:payments")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.getWorkloadContext(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if reader.runSingleCalls != 0 || reader.runCalls != 0 {
		t.Fatalf("graph calls = runSingle:%d run:%d, want none", reader.runSingleCalls, reader.runCalls)
	}
	if content.resolveRepositoryCalls != 0 || content.summaryCalls != 0 {
		t.Fatalf("content calls = resolve:%d summary:%d, want none", content.resolveRepositoryCalls, content.summaryCalls)
	}
}

func TestGetServiceStoryCandidateQueryAppliesScopedAuthBeforeAmbiguity(t *testing.T) {
	t.Parallel()

	reader := fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "w.name = $service_name") {
				requireScopedWorkloadPredicate(t, cypher, params)
				return []map[string]any{{
					"id":      "workload:payments",
					"name":    "payments",
					"kind":    "service",
					"repo_id": "repo-team-a",
				}}, nil
			}
			return nil, nil
		},
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "w.id = $workload_id") {
				return map[string]any{
					"id":      "workload:payments",
					"name":    "payments",
					"kind":    "service",
					"repo_id": "repo-team-a",
				}, nil
			}
			if strings.Contains(cypher, "MATCH (r:Repository") {
				return map[string]any{"repo_name": "payments"}, nil
			}
			return nil, nil
		},
	}
	handler := &EntityHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/payments/story", nil)
	req.SetPathValue("service_name", "payments")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.getServiceStory(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestGetServiceContextReadModelFallbackFiltersOutOfScopeRepository(t *testing.T) {
	t.Parallel()

	content := &recordingServiceContextContentStore{
		repo: &RepositoryCatalogEntry{ID: "repo-team-b", Name: "payments"},
		summary: repositoryReadModelSummary{
			Available:     true,
			WorkloadNames: []string{"payments"},
		},
	}
	handler := &EntityHandler{
		Neo4j:   fakeWorkloadGraphReader{},
		Content: content,
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/payments/context", nil)
	req.SetPathValue("service_name", "payments")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.getServiceContext(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if got, want := content.resolveRepositoryCalls, 1; got != want {
		t.Fatalf("resolveRepositoryCalls = %d, want %d", got, want)
	}
	if content.summaryCalls != 0 {
		t.Fatalf("summaryCalls = %d, want 0 before read-model hydration", content.summaryCalls)
	}
}

func TestInvestigateServiceCandidateQueryAppliesScopedAuthBeforeAmbiguity(t *testing.T) {
	t.Parallel()

	candidateQuerySeen := false
	reader := fakeWorkloadGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "w.name = $service_name") {
				candidateQuerySeen = true
				requireScopedWorkloadPredicate(t, cypher, params)
				return []map[string]any{{
					"id":      "workload:payments",
					"name":    "payments",
					"kind":    "service",
					"repo_id": "repo-team-a",
				}}, nil
			}
			return nil, nil
		},
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "w.id = $workload_id") {
				return map[string]any{
					"id":      "workload:payments",
					"name":    "payments",
					"kind":    "service",
					"repo_id": "repo-team-a",
				}, nil
			}
			if strings.Contains(cypher, "MATCH (r:Repository") {
				return map[string]any{"repo_name": "payments"}, nil
			}
			return nil, nil
		},
	}
	handler := &EntityHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/services/payments", nil)
	req.SetPathValue("service_name", "payments")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.investigateService(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !candidateQuerySeen {
		t.Fatal("service candidate query was not called")
	}
}

func TestInvestigateServiceEmptyGrantReturnsNotFoundWithoutBackendCalls(t *testing.T) {
	t.Parallel()

	reader := &recordingServiceContextGraphReader{}
	content := &recordingServiceContextContentStore{}
	handler := &EntityHandler{Neo4j: reader, Content: content, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/investigations/services/payments", nil)
	req.SetPathValue("service_name", "payments")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()

	handler.investigateService(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if reader.runSingleCalls != 0 || reader.runCalls != 0 {
		t.Fatalf("graph calls = runSingle:%d run:%d, want none", reader.runSingleCalls, reader.runCalls)
	}
	if content.resolveRepositoryCalls != 0 || content.summaryCalls != 0 {
		t.Fatalf("content calls = resolve:%d summary:%d, want none", content.resolveRepositoryCalls, content.summaryCalls)
	}
}

func requireScopedWorkloadPredicate(t *testing.T, cypher string, params map[string]any) {
	t.Helper()

	if !strings.Contains(cypher, "allowed_repository_ids") {
		t.Fatalf("query missing scoped repository predicate:\n%s", cypher)
	}
	allowed, ok := params["allowed_repository_ids"].([]string)
	if !ok || len(allowed) != 1 || allowed[0] != "repo-team-a" {
		t.Fatalf("allowed_repository_ids = %#v, want repo-team-a", params["allowed_repository_ids"])
	}
}

type recordingServiceContextGraphReader struct {
	runCalls       int
	runSingleCalls int
}

func (r *recordingServiceContextGraphReader) Run(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	r.runCalls++
	return nil, nil
}

func (r *recordingServiceContextGraphReader) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	r.runSingleCalls++
	return nil, nil
}

type recordingServiceContextContentStore struct {
	fakePortContentStore
	repo                   *RepositoryCatalogEntry
	summary                repositoryReadModelSummary
	resolveRepositoryCalls int
	summaryCalls           int
}

func (s *recordingServiceContextContentStore) ResolveRepository(
	context.Context,
	string,
) (*RepositoryCatalogEntry, error) {
	s.resolveRepositoryCalls++
	if s.repo == nil {
		return nil, nil
	}
	repo := *s.repo
	return &repo, nil
}

func (s *recordingServiceContextContentStore) repositoryReadModelSummary(
	context.Context,
	string,
) (repositoryReadModelSummary, error) {
	s.summaryCalls++
	return s.summary, nil
}
