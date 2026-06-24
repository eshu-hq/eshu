// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuthMiddlewareWithScopedTokensAllowsInfraResourceAggregateRoutes proves
// the count and inventory routes pass the scoped-token route gate so a scoped
// caller reaches the tenant-filtered handler.
func TestAuthMiddlewareWithScopedTokensAllowsInfraResourceAggregateRoutes(t *testing.T) {
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
		"/api/v0/infra/resources/count?category=cloud",
		"/api/v0/infra/resources/inventory?group_by=provider&limit=10",
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

// TestInfraResourceAggregateScopedEmptyGrantReturnsEmptyWithoutStoreRead proves
// an authenticated scoped token with no granted repositories gets the bounded
// zero/empty shape without the graph aggregate store ever being called.
func TestInfraResourceAggregateScopedEmptyGrantReturnsEmptyWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &stubInfraResourceAggregateStore{}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, tc := range []struct {
		name   string
		target string
	}{
		{name: "count", target: "/api/v0/infra/resources/count?category=cloud"},
		{name: "inventory", target: "/api/v0/infra/resources/inventory?group_by=provider&limit=10"},
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
			payload := decodeInfraAggregateData(t, rec.Body.Bytes())
			switch tc.name {
			case "count":
				if got := payload["total_resources"]; got != float64(0) {
					t.Fatalf("total_resources = %v, want 0", got)
				}
			case "inventory":
				if got := payload["count"]; got != float64(0) {
					t.Fatalf("count = %v, want 0", got)
				}
				if buckets, ok := payload["buckets"].([]any); !ok || len(buckets) != 0 {
					t.Fatalf("buckets = %v, want empty", payload["buckets"])
				}
			}
		})
	}

	if store.countCalls != 0 || store.invCalls != 0 {
		t.Fatalf("aggregate store was called for empty scoped grants (count=%d inventory=%d)",
			store.countCalls, store.invCalls)
	}
}

// TestInfraResourceAggregateScopedGrantPropagatesToFilter proves the handler
// copies a scoped token's granted repository and scope ids into the store
// filter so the repository-anchored predicate is bound.
func TestInfraResourceAggregateScopedGrantPropagatesToFilter(t *testing.T) {
	t.Parallel()

	store := &stubInfraResourceAggregateStore{
		count:     InfraResourceAggregateCount{TotalResources: 3},
		inventory: []InfraResourceInventoryRow{{Dimension: InfraResourceInventoryByProvider, Value: "aws", Count: 3}},
	}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	auth := AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-team-a"},
		AllowedScopeIDs:      []string{"git-repository-scope:team-a"},
	}

	countReq := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/count", nil)
	countReq = countReq.WithContext(ContextWithAuthContext(countReq.Context(), auth))
	mux.ServeHTTP(httptest.NewRecorder(), countReq)

	if got := store.lastFilter.AllowedRepositoryIDs; len(got) != 1 || got[0] != "repo-team-a" {
		t.Fatalf("count filter AllowedRepositoryIDs = %v, want [repo-team-a]", got)
	}
	if got := store.lastFilter.AllowedScopeIDs; len(got) != 1 || got[0] != "git-repository-scope:team-a" {
		t.Fatalf("count filter AllowedScopeIDs = %v, want [git-repository-scope:team-a]", got)
	}

	invReq := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?group_by=provider&limit=10", nil)
	invReq = invReq.WithContext(ContextWithAuthContext(invReq.Context(), auth))
	mux.ServeHTTP(httptest.NewRecorder(), invReq)

	if got := store.lastFilter.AllowedRepositoryIDs; len(got) != 1 || got[0] != "repo-team-a" {
		t.Fatalf("inventory filter AllowedRepositoryIDs = %v, want [repo-team-a]", got)
	}
}

// TestInfraResourceScopePredicateRendersOnlyWhenScoped proves the
// repository-anchored predicate is appended for scoped filters and that the
// unscoped where clause is byte-identical to the pre-scoped query
// (no-regression for shared / admin / local callers).
func TestInfraResourceScopePredicateRendersOnlyWhenScoped(t *testing.T) {
	t.Parallel()

	labels := allInfraLabels

	unscoped := infraResourceAggregateWhereClause(labels, InfraResourceAggregateFilter{Category: ""})
	if strings.Contains(unscoped, "$allowed_repository_ids") {
		t.Fatalf("unscoped where clause must not bind grant arrays:\n%s", unscoped)
	}
	if strings.Contains(unscoped, "scopeRepo") {
		t.Fatalf("unscoped where clause must not traverse repositories:\n%s", unscoped)
	}

	scoped := infraResourceAggregateWhereClause(labels, InfraResourceAggregateFilter{
		AllowedRepositoryIDs: []string{"repo-team-a"},
	})
	for _, want := range []string{
		"n.repo_id IN $allowed_repository_ids",
		"n.repo_id IN $allowed_scope_ids",
		"EXISTS {",
		"(scopeRepo:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n)",
		"scopeRepo.id IN $allowed_repository_ids",
	} {
		if !strings.Contains(scoped, want) {
			t.Fatalf("scoped where clause missing %q:\n%s", want, scoped)
		}
	}

	// The predicate must come after the label predicate so the scan is still
	// anchored on the closed infra label set before the grant filter applies.
	labelIdx := strings.Index(scoped, infraLabelPredicate(labels))
	predIdx := strings.Index(scoped, "n.repo_id IN $allowed_repository_ids")
	if labelIdx < 0 || predIdx < 0 || predIdx < labelIdx {
		t.Fatalf("scope predicate must follow the label predicate:\n%s", scoped)
	}
}

// TestInfraResourceAggregateParamsBindGrantArraysWhenScoped proves both grant
// arrays are bound for scoped filters (so the predicate parameters resolve even
// when only one side is granted) and absent otherwise.
func TestInfraResourceAggregateParamsBindGrantArraysWhenScoped(t *testing.T) {
	t.Parallel()

	unscoped := infraResourceAggregateParams(InfraResourceAggregateFilter{Provider: "aws"})
	if _, ok := unscoped["allowed_repository_ids"]; ok {
		t.Fatalf("unscoped params must not bind allowed_repository_ids: %v", unscoped)
	}

	scoped := infraResourceAggregateParams(InfraResourceAggregateFilter{
		AllowedScopeIDs: []string{"git-repository-scope:team-a"},
	})
	repoIDs, ok := scoped["allowed_repository_ids"].([]string)
	if !ok || len(repoIDs) != 0 {
		t.Fatalf("scoped params allowed_repository_ids = %v, want empty []string", scoped["allowed_repository_ids"])
	}
	scopeIDs, ok := scoped["allowed_scope_ids"].([]string)
	if !ok || len(scopeIDs) != 1 || scopeIDs[0] != "git-repository-scope:team-a" {
		t.Fatalf("scoped params allowed_scope_ids = %v, want [git-repository-scope:team-a]", scoped["allowed_scope_ids"])
	}
}

// TestGraphInfraResourceAggregateScopedQueryBindsPredicate proves the graph
// store sends the repository-anchored predicate and grant parameters to the
// backend for a scoped filter.
func TestGraphInfraResourceAggregateScopedQueryBindsPredicate(t *testing.T) {
	t.Parallel()

	graph := &stubInfraGraphQuery{responses: map[string][]map[string]any{
		"RETURN count(n) AS total": {{"total": int64(5)}},
	}}
	store := NewGraphInfraResourceAggregateStore(graph)

	_, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{
		AllowedRepositoryIDs: []string{"repo-team-a"},
		AllowedScopeIDs:      []string{"git-repository-scope:team-a"},
	})
	if err != nil {
		t.Fatalf("CountInfraResources() error = %v", err)
	}

	if len(graph.calls) == 0 {
		t.Fatal("graph store made no calls")
	}
	for _, call := range graph.calls {
		if !strings.Contains(call.Cypher, "n.repo_id IN $allowed_repository_ids") {
			t.Fatalf("scoped Cypher missing grant predicate:\n%s", call.Cypher)
		}
		if _, ok := call.Params["allowed_repository_ids"]; !ok {
			t.Fatalf("scoped Cypher missing allowed_repository_ids param: %v", call.Params)
		}
		if _, ok := call.Params["allowed_scope_ids"]; !ok {
			t.Fatalf("scoped Cypher missing allowed_scope_ids param: %v", call.Params)
		}
	}
}

func decodeInfraAggregateData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	// The test requests omit the envelope Accept header, so WriteSuccess emits
	// the raw data map directly (no {"data": ...} wrapper).
	data := map[string]any{}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, body)
	}
	return data
}
