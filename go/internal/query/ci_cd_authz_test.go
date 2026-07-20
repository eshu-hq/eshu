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

func TestAuthMiddlewareWithScopedTokensAllowsCICDRunCorrelationRoutes(t *testing.T) {
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

	for _, target := range []string{
		"/api/v0/ci-cd/run-correlations?repository_id=repo-team-a&limit=10",
		"/api/v0/ci-cd/run-correlations/count?repository_id=repo-team-a",
		"/api/v0/ci-cd/run-correlations/inventory?repository_id=repo-team-a&limit=10",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.Header.Set("Authorization", "Bearer scoped-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusNoContent; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
		})
	}
}

func TestCICDRunCorrelationScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	correlations := &failingCICDRunCorrelationStore{}
	aggregates := &failingCICDRunCorrelationAggregateStore{}
	handler := &CICDHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: correlations,
		Aggregates:   aggregates,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name   string
		target string
	}{
		{
			name:   "list",
			target: "/api/v0/ci-cd/run-correlations?commit_sha=abc123&limit=10",
		},
		{
			name:   "count",
			target: "/api/v0/ci-cd/run-correlations/count?image_ref=registry.example.com/team/api:prod",
		},
		{
			name:   "inventory",
			target: "/api/v0/ci-cd/run-correlations/inventory?image_ref=registry.example.com/team/api:prod&limit=10",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
				Mode:        AuthModeScoped,
				TenantID:    "tenant-a",
				WorkspaceID: "workspace-a",
			}))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if tc.name == "count" {
				assertZeroCICDCountResponse(t, rec.Body.Bytes())
			}
			for _, leaked := range []string{"abc123", "registry.example.com/team/api:prod", "repo://example/api", "payments-api"} {
				if strings.Contains(rec.Body.String(), leaked) {
					t.Fatalf("empty scoped response leaked %q: %s", leaked, rec.Body.String())
				}
			}
		})
	}
	if correlations.called {
		t.Fatal("correlation store was called for empty scoped grants")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for empty scoped grants (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestCICDRunCorrelationScopedRepositorySelectorDeniesOutOfGrantWithoutStoreRead(t *testing.T) {
	t.Parallel()

	correlations := &failingCICDRunCorrelationStore{}
	aggregates := &failingCICDRunCorrelationAggregateStore{}
	handler := &CICDHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: correlations,
		Aggregates:   aggregates,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/ci-cd/run-correlations?repository_id=payments-api&limit=10",
		"/api/v0/ci-cd/run-correlations/count?repository_id=payments-api",
		"/api/v0/ci-cd/run-correlations/inventory?repository_id=payments-api&limit=10",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
				Mode:                 AuthModeScoped,
				TenantID:             "tenant-a",
				WorkspaceID:          "workspace-a",
				AllowedRepositoryIDs: []string{"repo://example/other"},
			}))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if got, want := rec.Code, http.StatusNotFound; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			for _, leaked := range []string{"repo://example/api", "workflow"} {
				if strings.Contains(rec.Body.String(), leaked) {
					t.Fatalf("out-of-grant response leaked %q: %s", leaked, rec.Body.String())
				}
			}
		})
	}
	if correlations.called {
		t.Fatal("correlation store was called for out-of-grant repository selector")
	}
	if aggregates.countCalled || aggregates.inventoryCalled {
		t.Fatalf("aggregate store was called for out-of-grant repository selector (count=%v inventory=%v)",
			aggregates.countCalled, aggregates.inventoryCalled)
	}
}

func TestCICDRunCorrelationHandlerPassesScopedGrants(t *testing.T) {
	t.Parallel()

	correlations := &recordingCICDRunCorrelationStore{
		rows: []CICDRunCorrelationRow{{
			CorrelationID: "cicd-correlation-1",
			RepositoryID:  "repo://example/api",
			Outcome:       "exact",
		}},
	}
	aggregates := &stubCICDRunCorrelationAggregateStore{
		count: CICDRunCorrelationAggregateCount{
			ByOutcome:     map[string]int{},
			ByEnvironment: map[string]int{},
			ByProvider:    map[string]int{},
		},
	}
	handler := &CICDHandler{
		Content:      repositorySelectorReadModelContentStore(),
		Correlations: correlations,
		Aggregates:   aggregates,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	auth := AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://example/api"},
		AllowedScopeIDs:      []string{"git-repository-scope:example/api"},
	}

	listReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=payments-api&commit_sha=abc123&limit=10",
		nil,
	)
	listReq = listReq.WithContext(ContextWithAuthContext(listReq.Context(), auth))
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("list status = %d, want %d; body = %s", got, want, listRec.Body.String())
	}
	if got, want := correlations.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("list RepositoryID = %q, want %q", got, want)
	}
	if got, want := correlations.lastFilter.AllowedRepositoryIDs, []string{"repo://example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("list AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := correlations.lastFilter.AllowedScopeIDs, []string{"git-repository-scope:example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("list AllowedScopeIDs = %#v, want %#v", got, want)
	}

	countReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations/count?repository_id=payments-api&commit_sha=abc123",
		nil,
	)
	countReq = countReq.WithContext(ContextWithAuthContext(countReq.Context(), auth))
	countRec := httptest.NewRecorder()
	mux.ServeHTTP(countRec, countReq)
	if got, want := countRec.Code, http.StatusOK; got != want {
		t.Fatalf("count status = %d, want %d; body = %s", got, want, countRec.Body.String())
	}
	if got, want := aggregates.lastFilter.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("aggregate RepositoryID = %q, want %q", got, want)
	}
	if got, want := aggregates.lastFilter.AllowedRepositoryIDs, []string{"repo://example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("aggregate AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := aggregates.lastFilter.AllowedScopeIDs, []string{"git-repository-scope:example/api"}; !equalPacketStringSlices(got, want) {
		t.Fatalf("aggregate AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

func TestCICDRunCorrelationSQLAppliesScopedAuthorizationBeforeOrderAndGrouping(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		query      string
		beforeText string
		repoParam  string
		scopeParam string
	}{
		{
			name:       "list",
			query:      listCICDRunCorrelationsQuery,
			beforeText: "ORDER BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($13::text[])",
			scopeParam: "fact.scope_id = ANY($14::text[])",
		},
		{
			name:       "total",
			query:      cicdRunCorrelationAggregateTotalQuery,
			beforeText: ";",
			repoParam:  "fact.payload->>'repository_id' = ANY($9::text[])",
			scopeParam: "fact.scope_id = ANY($10::text[])",
		},
		{
			name:       "group",
			query:      cicdRunCorrelationAggregateGroupQueryTemplate,
			beforeText: "GROUP BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($9::text[])",
			scopeParam: "fact.scope_id = ANY($10::text[])",
		},
		{
			name:       "inventory",
			query:      cicdRunCorrelationInventoryQueryTemplate,
			beforeText: "GROUP BY",
			repoParam:  "fact.payload->>'repository_id' = ANY($11::text[])",
			scopeParam: "fact.scope_id = ANY($12::text[])",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{tc.repoParam, tc.scopeParam} {
				if !strings.Contains(tc.query, want) {
					t.Fatalf("query missing %q:\n%s", want, tc.query)
				}
				if strings.Index(tc.query, want) > strings.Index(tc.query, tc.beforeText) {
					t.Fatalf("authorization predicate %q appears after %s:\n%s", want, tc.beforeText, tc.query)
				}
			}
		})
	}
}

// TestAuthMiddlewareWithScopedTokensRejectsAdjacentCorrelationRoutes proves
// scopedCICDRunCorrelationRoute's exact-path match doesn't over-match
// prefix/typo-adjacent paths. GET /api/v0/kubernetes/correlations and
// GET /api/v0/observability/coverage/correlations were dropped from this
// negative list: the #5167 F-6 W6 cloud/aws family workstream gave both
// routes their own real grant-filtered allowlist matchers
// (scopedKubernetesCorrelationsRoute, scopedObservabilityCoverageCorrelationsRoute,
// auth_scoped_routes_cloud.go), so asserting a 403 for them here would now be
// asserting a regression, not a boundary proof. See
// TestKubernetesListCorrelationsScopedGrantHitsRealStoreAndReturnsRowData and
// TestObservabilityCoverageListCorrelationsScopedGrantHitsRealStoreAndReturnsRowData
// for their own scoped-token proof.
func TestAuthMiddlewareWithScopedTokensRejectsAdjacentCorrelationRoutes(t *testing.T) {
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
		"/api/v0/ci-cd/run-correlation?limit=10",        // singular typo, not the real route
		"/api/v0/ci-cd/run-correlations/extra?limit=10", // extra path segment
		"/api/v0/ci-cd/run-correlations-other?limit=10", // prefix-adjacent, not a path segment boundary
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

type failingCICDRunCorrelationStore struct {
	called bool
}

func (s *failingCICDRunCorrelationStore) ListCICDRunCorrelations(
	context.Context,
	CICDRunCorrelationFilter,
) ([]CICDRunCorrelationRow, error) {
	s.called = true
	return nil, errors.New("broad ci/cd run correlation read")
}

type failingCICDRunCorrelationAggregateStore struct {
	countCalled     bool
	inventoryCalled bool
}

func (s *failingCICDRunCorrelationAggregateStore) CountCICDRunCorrelations(
	context.Context,
	CICDRunCorrelationAggregateFilter,
) (CICDRunCorrelationAggregateCount, error) {
	s.countCalled = true
	return CICDRunCorrelationAggregateCount{}, errors.New("broad ci/cd run correlation count read")
}

func (s *failingCICDRunCorrelationAggregateStore) CICDRunCorrelationInventory(
	context.Context,
	CICDRunCorrelationAggregateFilter,
	CICDRunCorrelationInventoryDimension,
	int,
	int,
) ([]CICDRunCorrelationInventoryRow, error) {
	s.inventoryCalled = true
	return nil, errors.New("broad ci/cd run correlation inventory read")
}

func assertZeroCICDCountResponse(t *testing.T, body []byte) {
	t.Helper()

	var resp struct {
		TotalCorrelations int `json:"total_correlations"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode count response: %v; body = %s", err, string(body))
	}
	if got, want := resp.TotalCorrelations, 0; got != want {
		t.Fatalf("total_correlations = %d, want %d", got, want)
	}
}
