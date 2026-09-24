// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
)

func TestRepositoryListGraphAppliesScopedAuthBeforePagination(t *testing.T) {
	t.Parallel()

	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			if !strings.Contains(cypher, "allowed_repository_ids") {
				t.Fatalf("repository count query missing scoped repository predicate:\n%s", cypher)
			}
			if got, want := params["allowed_repository_ids"], []string{"repo-team-a"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("allowed_repository_ids = %#v, want %#v", got, want)
			}
			return map[string]any{"total": int64(1)}, nil
		},
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "allowed_repository_ids") {
				t.Fatalf("repository list query missing scoped repository predicate:\n%s", cypher)
			}
			allowed, ok := params["allowed_repository_ids"].([]string)
			if !ok || len(allowed) != 1 || allowed[0] != "repo-team-a" {
				t.Fatalf("allowed_repository_ids = %#v, want repo-team-a", params["allowed_repository_ids"])
			}
			return []map[string]any{{
				"id":   "repo-team-a",
				"name": "payments",
			}}, nil
		},
	}
	handler := &Handler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=1", nil)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	repositories := body["repositories"].([]any)
	if got, want := len(repositories), 1; got != want {
		t.Fatalf("len(repositories) = %d, want %d", got, want)
	}
}

func TestRepositoryListExposesSourceBackedGroupEvidence(t *testing.T) {
	t.Parallel()

	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(3)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			// is_dependency (and dependency-cluster membership) are both
			// derived in Go from the same dependency-edge pre-pass, not
			// read off the page row (issue #6786 defect 1): repo-library is
			// the target of an inbound DEPENDS_ON edge from a depender that
			// is not itself in this page (repo-consumer), so repo-service
			// stays untouched by clustering and keeps exercising the
			// independent slug-namespace evidence tier below.
			if strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)") {
				return dependencyEdgeRowsForRead(cypher, []map[string]any{
					{"source_id": "repo-consumer", "target_id": "repo-library"},
				}), nil
			}
			return []map[string]any{
				{
					"id":        "repo-service",
					"name":      "payments-api",
					"repo_slug": "platform/payments-api",
				},
				{
					"id":        "repo-library",
					"name":      "shared-lib",
					"repo_slug": "platform/shared-lib",
				},
				{
					"id":   "repo-unattributed",
					"name": "unattributed",
				},
			}, nil
		},
	}
	handler := &Handler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	repositories := body["repositories"].([]any)
	if got, want := len(repositories), 3; got != want {
		t.Fatalf("len(repositories) = %d, want %d", got, want)
	}
	service := repositories[0].(map[string]any)
	if got, want := service["group_key"], "Platform"; got != want {
		t.Fatalf("service group_key = %#v, want %#v", got, want)
	}
	if got, want := service["group_source"], "repo_slug_namespace"; got != want {
		t.Fatalf("service group_source = %#v, want %#v", got, want)
	}
	if got, want := service["group_truth"], "derived"; got != want {
		t.Fatalf("service group_truth = %#v, want %#v", got, want)
	}
	if got, want := service["group_kind"], "source"; got != want {
		t.Fatalf("service group_kind = %#v, want %#v", got, want)
	}

	// repo-library is the target of an inbound DEPENDS_ON edge, so it is
	// grouped by the dependency-cluster tier -- the primary grouping signal
	// (issue #3504) that also feeds is_dependency -- keyed by the
	// lexicographically smallest id in its component (repo-consumer).
	library := repositories[1].(map[string]any)
	if got, want := library["group_key"], "repo-consumer"; got != want {
		t.Fatalf("library group_key = %#v, want %#v", got, want)
	}
	if got, want := library["group_source"], repositoryGroupSourceDependencyCluster; got != want {
		t.Fatalf("library group_source = %#v, want %#v", got, want)
	}
	if got, want := library["group_kind"], "cluster"; got != want {
		t.Fatalf("library group_kind = %#v, want %#v", got, want)
	}
	if got, want := querycontract.BoolVal(library, "is_dependency"), true; got != want {
		t.Fatalf("library is_dependency = %v, want %v", got, want)
	}

	unattributed := repositories[2].(map[string]any)
	if got, want := unattributed["group_source"], "missing_evidence"; got != want {
		t.Fatalf("unattributed group_source = %#v, want %#v", got, want)
	}
	if got, want := unattributed["group_truth"], "missing_evidence"; got != want {
		t.Fatalf("unattributed group_truth = %#v, want %#v", got, want)
	}
	reasons := querytestutil.RequireStringAnySlice(t, body, "partial_reasons")
	if !querytestutil.AnySliceContains(reasons, "repository_group_evidence_missing") {
		t.Fatalf("partial_reasons = %#v, want repository_group_evidence_missing", reasons)
	}
}

func TestRepositoryListContentAppliesScopedAuthBeforeMetadata(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: querytestutil.TenantAuthzRepositories()},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=2", nil)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	repositories := body["repositories"].([]any)
	if got, want := len(repositories), 1; got != want {
		t.Fatalf("len(repositories) = %d, want %d", got, want)
	}
	repo := repositories[0].(map[string]any)
	if got, want := repo["id"], "repo-team-a"; got != want {
		t.Fatalf("repository id = %#v, want %#v", got, want)
	}
	if got, want := body["count"], float64(1); got != want {
		t.Fatalf("count = %#v, want %#v", got, want)
	}
	if got := body["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
	limits := body["result_limits"].(map[string]any)
	if got, want := limits["repository_count"], float64(1); got != want {
		t.Fatalf("result_limits.repository_count = %#v, want %#v", got, want)
	}
}

func TestResolveRepositorySelectorAppliesScopedAuthBeforeAmbiguity(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: querytestutil.TenantAuthzRepositories()},
	}
	ctx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
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

func TestResolveRepositorySelectorDeniesOutOfScopeCanonicalID(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: querytestutil.TenantAuthzRepositories()},
	}
	ctx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repo-team-a"},
	})

	_, err := handler.resolveRepositorySelector(ctx, "repo-team-b")
	if err == nil {
		t.Fatal("resolveRepositorySelector() error = nil, want not found for out-of-scope repo")
	}
	var notFound selector.NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("error = %T %v, want selector.NotFoundError", err, err)
	}
}

func TestRepositoryListSharedAuthKeepsExistingScope(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: querytestutil.TenantAuthzRepositories()},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=3", nil)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:      queryauth.AuthModeShared,
		AllScopes: true,
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	repositories := body["repositories"].([]any)
	if got, want := len(repositories), 3; got != want {
		t.Fatalf("len(repositories) = %d, want %d", got, want)
	}
}

func TestRepositoryListAllScopeAdminKeepsExistingScope(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Content: content.FakePortContentStore{Repositories: querytestutil.TenantAuthzRepositories()},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=3", nil)
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:        queryauth.AuthModeScoped,
		TenantID:    "tenant-admin",
		WorkspaceID: "workspace-admin",
		AllScopes:   true,
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	body := decodeRepositoryAuthzBody(t, rec)
	repositories := body["repositories"].([]any)
	if got, want := len(repositories), 3; got != want {
		t.Fatalf("len(repositories) = %d, want %d", got, want)
	}
}

func decodeRepositoryAuthzBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	return body
}
