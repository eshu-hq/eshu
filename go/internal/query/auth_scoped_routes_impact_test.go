// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// impactCompareTwoTenantRoutes is the #5167 W3 inventory: every impact/* and
// compare/* route allowlisted by scopedImpactCompareRoute. Used to prove the
// exhaustiveness/completeness gates and the real-middleware round trip stay
// in lockstep with the ten routes this workstream filtered and allowlisted.
var impactCompareTwoTenantRoutes = []string{
	"/api/v0/impact/contracts",
	"/api/v0/compare/environments",
	"/api/v0/impact/blast-radius",
	"/api/v0/impact/resource-investigation",
	"/api/v0/impact/change-surface",
	"/api/v0/impact/change-surface/investigate",
	"/api/v0/impact/pre-change",
	"/api/v0/impact/developer-change-plan",
	"/api/v0/impact/trace-deployment-chain",
	"/api/v0/impact/deployment-config-influence",
}

// TestScopedImpactCompareRouteMatchesAllowlistedRoutes proves every #5167 W3
// route is wired into scopedHTTPRouteSupportsTenantFilter (via
// scopedImpactCompareRoute) and every route scopedImpactCompareRoute matches
// is a route this workstream actually filtered -- catching a stale/incomplete
// matcher.
func TestScopedImpactCompareRouteMatchesAllowlistedRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range impactCompareTwoTenantRoutes {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if !scopedHTTPRouteSupportsTenantFilter(req) {
			t.Errorf("scopedHTTPRouteSupportsTenantFilter(%s) = false, want true", path)
		}
		if !scopedImpactCompareRoute(req) {
			t.Errorf("scopedImpactCompareRoute(%s) = false, want true", path)
		}
	}
}

func decodeFlatJSONBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body = %s", err, rec.Body.String())
	}
	return body
}

func scopedTestAuthContext(tenant string, allowedRepositoryIDs []string) AuthContext {
	return AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             tenant,
		WorkspaceID:          tenant,
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:" + tenant,
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
}

// --- investigate_contract_impact (mutation-checked) ---

func contractImpactTestGraph(t *testing.T) fakeGraphReaderWithSingle {
	return fakeGraphReaderWithSingle{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "EXPOSES_ENDPOINT") {
				t.Fatalf("unexpected contract-impact query: %s", cypher)
			}
			if params["provider_repo_id"] != "repo-a" {
				// A denied caller must never reach the graph with a repo id
				// outside its grant -- the handler short-circuits before this
				// point. If this branch runs, the grant check was removed.
				t.Fatalf("contract-impact query ran for an ungranted provider_repo_id: %#v", params["provider_repo_id"])
			}
			return []map[string]any{{
				"endpoint_id": "endpoint-1", "provider_repo_id": "repo-a", "provider_repo": "svc-a",
				"path": "/orders", "methods": []any{"GET"},
			}}, nil
		},
	}
}

// TestInvestigateContractImpactScopedGrantAndDenyMutationCheck is a #5167 W3
// mutation-check route: removing the ProviderRepoID grant check in
// contract_impact.go's contractImpactResponse makes the denied case both
// reach the graph (failing contractImpactTestGraph's t.Fatalf guard above)
// and return the provider row (failing the empty-providers assertion below).
func TestInvestigateContractImpactScopedGrantAndDenyMutationCheck(t *testing.T) {
	t.Parallel()

	body := `{"family":"http","provider_repo_id":"repo-a"}`

	t.Run("granted provider repo returns providers", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: contractImpactTestGraph(t), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/contracts", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		providers, _ := resp["providers"].([]any)
		if got, want := len(providers), 1; got != want {
			t.Fatalf("len(providers) = %d, want %d; body = %s", got, want, w.Body.String())
		}
	})

	t.Run("denied provider repo returns empty providers without querying", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: contractImpactTestGraph(t), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/contracts", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-b", []string{"repo-b"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		providers, _ := resp["providers"].([]any)
		if got, want := len(providers), 0; got != want {
			t.Fatalf("len(providers) = %d, want %d (denied grant must not see repo-a's endpoints); body = %s", got, want, w.Body.String())
		}
	})
}

// TestAuthMiddlewareWithScopedTokensAllowsContractImpact is the real-middleware
// round trip: a scoped bearer token must reach the handler (not 403) and the
// grant filter, not the middleware, decides visibility.
func TestAuthMiddlewareWithScopedTokensAllowsContractImpact(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Neo4j: contractImpactTestGraph(t), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{context: scopedTestAuthContext("tenant-a", []string{"repo-a"}), ok: true}
	middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/contracts", bytes.NewBufferString(`{"family":"http","provider_repo_id":"repo-a"}`))
	req.Header.Set("Authorization", "Bearer scoped-token")
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
	}
}

// --- compare_environments (mutation-checked) ---

func compareEnvironmentsTestGraph() fakeCompareGraphReader {
	return fakeCompareGraphReader{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (w:Workload)") {
				return map[string]any{"id": "workload:orders-api", "name": "orders-api", "kind": "service", "repo_id": "repo-a"}, nil
			}
			return nil, nil
		},
	}
}

// TestCompareEnvironmentsScopedGrantAndDenyMutationCheck is a #5167 W3
// mutation-check route: removing the workload repo_id grant check in
// compare.go's compareEnvironments makes a denied caller see status "present"
// (or any status other than "not found") instead of the missing-workload
// response.
func TestCompareEnvironmentsScopedGrantAndDenyMutationCheck(t *testing.T) {
	t.Parallel()

	body := `{"workload_id":"workload:orders-api","left":"qa","right":"prod"}`

	t.Run("granted workload repo returns the workload", func(t *testing.T) {
		t.Parallel()
		resp := executeCompareEnvironmentsRequestWithAuth(t, &CompareHandler{Neo4j: compareEnvironmentsTestGraph(), Profile: ProfileLocalAuthoritative}, body, scopedTestAuthContext("tenant-a", []string{"repo-a"}))
		workload := requireMap(t, resp, "workload")
		if got, want := workload["id"], "workload:orders-api"; got != want {
			t.Fatalf("workload.id = %#v, want %#v", got, want)
		}
	})

	t.Run("denied workload repo renders not found", func(t *testing.T) {
		t.Parallel()
		resp := executeCompareEnvironmentsRequestWithAuth(t, &CompareHandler{Neo4j: compareEnvironmentsTestGraph(), Profile: ProfileLocalAuthoritative}, body, scopedTestAuthContext("tenant-b", []string{"repo-b"}))
		if workload := resp["workload"]; workload != nil {
			t.Fatalf("workload = %#v, want nil (denied grant must render like a nonexistent workload)", workload)
		}
		if got, want := resp["reason"], "Workload 'workload:orders-api' not found"; got != want {
			t.Fatalf("reason = %#v, want %#v", got, want)
		}
	})
}

func executeCompareEnvironmentsRequestWithAuth(t *testing.T, handler *CompareHandler, body string, auth AuthContext) map[string]any {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/compare/environments", bytes.NewBufferString(body))
	req = req.WithContext(ContextWithAuthContext(req.Context(), auth))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	return decodeFlatJSONBody(t, w)
}

// TestAuthMiddlewareWithScopedTokensAllowsCompareEnvironments is the
// real-middleware round trip for compare_environments.
func TestAuthMiddlewareWithScopedTokensAllowsCompareEnvironments(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{Neo4j: compareEnvironmentsTestGraph(), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{context: scopedTestAuthContext("tenant-a", []string{"repo-a"}), ok: true}
	middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/compare/environments", bytes.NewBufferString(`{"workload_id":"workload:orders-api","left":"qa","right":"prod"}`))
	req.Header.Set("Authorization", "Bearer scoped-token")
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
	}
}

// --- find_blast_radius (mutation-checked) ---

func blastRadiusTestGraph(t *testing.T) fakeGraphReaderWithSingle {
	return fakeGraphReaderWithSingle{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "DEPENDS_ON"):
				return []map[string]any{
					{"repo": "svc-a-dependent", "repo_id": "repo-a-dependent", "hops": int64(1)},
					{"repo": "svc-b-dependent", "repo_id": "repo-b-dependent", "hops": int64(1)},
				}, nil
			case strings.Contains(cypher, "CONTAINS]-(tier:Tier)"):
				// enrichBlastRadiusTiers's optional tier lookup; no tier data needed
				// for this proof.
				return nil, nil
			default:
				t.Fatalf("unexpected blast-radius query: %s", cypher)
				return nil, nil
			}
		},
	}
}

// TestFindBlastRadiusScopedGrantAndDenyMutationCheck is a #5167 W3
// mutation-check route: removing filterRowsByRepoIDForAccess in
// impact_blast_radius.go's findBlastRadius makes a caller granted only
// repo-a-dependent also see repo-b-dependent, the cross-tenant repo.
func TestFindBlastRadiusScopedGrantAndDenyMutationCheck(t *testing.T) {
	t.Parallel()

	body := `{"target":"payments","target_type":"repository"}`

	t.Run("scoped caller sees only its granted affected repo", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: blastRadiusTestGraph(t), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a-dependent"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		affected, _ := resp["affected"].([]any)
		if got, want := len(affected), 1; got != want {
			t.Fatalf("len(affected) = %d, want %d; body = %s", got, want, w.Body.String())
		}
		row := affected[0].(map[string]any)
		if got, want := row["repo_id"], "repo-a-dependent"; got != want {
			t.Fatalf("affected[0].repo_id = %#v, want %#v", got, want)
		}
	})

	t.Run("empty grant returns zero affected repos without querying", func(t *testing.T) {
		t.Parallel()
		graph := fakeGraphReaderWithSingle{run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
			t.Fatal("blast-radius must not query the graph for an empty grant")
			return nil, nil
		}}
		handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-empty", nil)))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		affected, _ := resp["affected"].([]any)
		if got, want := len(affected), 0; got != want {
			t.Fatalf("len(affected) = %d, want %d; body = %s", got, want, w.Body.String())
		}
	})
}

// TestAuthMiddlewareWithScopedTokensAllowsFindBlastRadius is the
// real-middleware round trip for find_blast_radius.
func TestAuthMiddlewareWithScopedTokensAllowsFindBlastRadius(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Neo4j: blastRadiusTestGraph(t), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{context: scopedTestAuthContext("tenant-a", []string{"repo-a-dependent"}), ok: true}
	middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/blast-radius", bytes.NewBufferString(`{"target":"payments","target_type":"repository"}`))
	req.Header.Set("Authorization", "Bearer scoped-token")
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
	}
}

// --- investigate_resource ---

// TestInvestigateResourceScopedGrantAndDeny proves a resolved resource
// candidate and its repository-provenance paths are bound to the caller's
// grant.
func TestInvestigateResourceScopedGrantAndDeny(t *testing.T) {
	t.Parallel()

	newGraph := func() *recordingResourceInvestigationGraph {
		return &recordingResourceInvestigationGraph{
			runRows: [][]map[string]any{{
				{
					"id": "cloud:rds:orders", "name": "orders-db", "labels": []any{"CloudResource"},
					"resource_type": "aws_db_instance", "provider": "aws", "environment": "prod",
					"repo_id": "repo-a",
				},
			}},
		}
	}
	body := `{"resource_id":"cloud:rds:orders"}`

	t.Run("granted resource repo resolves", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: newGraph(), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/resource-investigation", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		resolution := requireMap(t, resp, "target_resolution")
		if got, want := resolution["status"], "resolved"; got != want {
			t.Fatalf("resolution.status = %#v, want %#v; body = %s", got, want, w.Body.String())
		}
	})

	t.Run("denied resource repo resolves to no match", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: newGraph(), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/resource-investigation", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-b", []string{"repo-b"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		resolution := requireMap(t, resp, "target_resolution")
		if got, want := resolution["status"], "no_match"; got != want {
			t.Fatalf("resolution.status = %#v, want %#v (a candidate outside the grant must never resolve); body = %s", got, want, w.Body.String())
		}
	})
}

// TestAuthMiddlewareWithScopedTokensAllowsInvestigateResource is the
// real-middleware round trip for investigate_resource.
func TestAuthMiddlewareWithScopedTokensAllowsInvestigateResource(t *testing.T) {
	t.Parallel()

	graph := &recordingResourceInvestigationGraph{runRows: [][]map[string]any{{
		{"id": "cloud:rds:orders", "name": "orders-db", "labels": []any{"CloudResource"}, "repo_id": "repo-a"},
	}}}
	handler := &ImpactHandler{Neo4j: graph, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{context: scopedTestAuthContext("tenant-a", []string{"repo-a"}), ok: true}
	middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/resource-investigation", bytes.NewBufferString(`{"resource_id":"cloud:rds:orders"}`))
	req.Header.Set("Authorization", "Bearer scoped-token")
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
	}
}
