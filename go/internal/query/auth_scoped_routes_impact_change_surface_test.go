// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// auth_scoped_routes_impact_change_surface_test.go continues the #5167 W3
// two-tenant proof suite from auth_scoped_routes_impact_test.go (split to stay
// under the file-length cap): the change-surface family, the pre-change /
// developer-change-plan family, and the deployment-trace family.
// --- find_change_surface / investigate_change_surface (legacy + investigate) ---

func changeSurfaceRepositoryTargetGraph(t *testing.T) fakeGraphReaderWithSingle {
	return fakeGraphReaderWithSingle{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (n:Repository {id: $target})"):
				return []map[string]any{{"id": "repo-a", "name": "repo-a", "labels": []any{"Repository"}, "repo_id": "repo-a"}}, nil
			case strings.Contains(cypher, "->(impacted)"):
				// The legacy findChangeSurfaceImpactRows path unwinds one output row
				// per relationships(path) edge, so each impacted node needs at least
				// one edge to surface in the response.
				edge := []any{map[string]any{"type": "DEPENDS_ON", "properties": map[string]any{}}}
				return []map[string]any{
					{"id": "repo-a-impacted", "name": "repo-a-impacted", "labels": []any{"Repository"}, "repo_id": "repo-a-impacted", "depth": int64(1), "rels": edge},
					{"id": "repo-b-impacted", "name": "repo-b-impacted", "labels": []any{"Repository"}, "repo_id": "repo-b-impacted", "depth": int64(1), "rels": edge},
				}, nil
			default:
				return nil, nil
			}
		},
	}
}

// TestFindChangeSurfaceScopedGrantAndDeny proves both the resolved target and
// every impacted row are bound to the grant for the legacy
// POST /api/v0/impact/change-surface route.
func TestFindChangeSurfaceScopedGrantAndDeny(t *testing.T) {
	t.Parallel()
	body := `{"target":"repo-a","kind":"repository"}`

	t.Run("granted target sees only its own granted impacted repos", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: changeSurfaceRepositoryTargetGraph(t), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a", "repo-a-impacted"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		impacted, _ := resp["impacted"].([]any)
		if got, want := len(impacted), 1; got != want {
			t.Fatalf("len(impacted) = %d, want %d (must exclude repo-b-impacted); body = %s", got, want, w.Body.String())
		}
		row := impacted[0].(map[string]any)
		if got, want := row["id"], "repo-a-impacted"; got != want {
			t.Fatalf("impacted[0].id = %#v, want %#v", got, want)
		}
	})

	t.Run("target outside grant never resolves", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: changeSurfaceRepositoryTargetGraph(t), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-c", []string{"repo-c"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		impacted, _ := resp["impacted"].([]any)
		if got, want := len(impacted), 0; got != want {
			t.Fatalf("len(impacted) = %d, want %d (target outside grant must not resolve); body = %s", got, want, w.Body.String())
		}
	})
}

// TestInvestigateChangeSurfaceScopedGrantAndDeny is the same proof for
// POST /api/v0/impact/change-surface/investigate.
func TestInvestigateChangeSurfaceScopedGrantAndDeny(t *testing.T) {
	t.Parallel()
	body := `{"target":"repo-a","target_type":"repository"}`

	t.Run("granted target sees only its own granted impacted repos", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: changeSurfaceRepositoryTargetGraph(t), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a", "repo-a-impacted"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		direct, _ := resp["direct_impact"].([]any)
		if got, want := len(direct), 1; got != want {
			t.Fatalf("len(direct_impact) = %d, want %d (must exclude repo-b-impacted); body = %s", got, want, w.Body.String())
		}
	})

	t.Run("target outside grant never resolves", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: changeSurfaceRepositoryTargetGraph(t), Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-c", []string{"repo-c"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		resp := decodeFlatJSONBody(t, w)
		resolution := requireMap(t, resp, "target_resolution")
		if got, want := resolution["status"], "no_match"; got != want {
			t.Fatalf("target_resolution.status = %#v, want %#v; body = %s", got, want, w.Body.String())
		}
	})
}

// TestAuthMiddlewareWithScopedTokensAllowsChangeSurfaceFamily is the
// real-middleware round trip for both change-surface routes.
func TestAuthMiddlewareWithScopedTokensAllowsChangeSurfaceFamily(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/api/v0/impact/change-surface", "/api/v0/impact/change-surface/investigate"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			key := "target"
			body := `{"` + key + `":"repo-a","target_type":"repository","kind":"repository"}`
			handler := &ImpactHandler{Neo4j: changeSurfaceRepositoryTargetGraph(t), Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)
			resolver := &fakeScopedTokenResolver{context: scopedTestAuthContext("tenant-a", []string{"repo-a"}), ok: true}
			middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
			}
		})
	}
}

// fakeChangeSurfaceTopicContentStore embeds fakePortContentStore (the
// existing full-interface ContentStore fake in ports_test.go) and adds
// investigateCodeTopic so it also satisfies codeTopicContentInvestigator --
// the real production type assertion path
// (changeSurfaceCodeSurface -> changeSurfaceTopicRows -> h.Content.(codeTopicContentInvestigator))
// a *ContentReader (the real Postgres-backed content store, per
// cmd/api/wiring.go) also satisfies. investigateCodeTopic itself has no repo
// grant filtering of its own (a different #5167 "code/*" workstream), so this
// fake returns rows spanning two repositories, and the test proves
// filterCodeTopicRowsForAccess (impact_access_filter.go) -- not the
// underlying content-store query -- is what removes the cross-tenant row.
type fakeChangeSurfaceTopicContentStore struct {
	fakePortContentStore
	topicRows []codeTopicEvidenceRow
}

func (f fakeChangeSurfaceTopicContentStore) investigateCodeTopic(
	_ context.Context,
	_ codeTopicInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	return f.topicRows, nil
}

// TestInvestigateChangeSurfaceScopedFiltersCrossTenantTopicEvidence is a
// #5167 W3 mutation-check route: removing filterCodeTopicRowsForAccess in
// impact_change_surface_response.go's changeSurfaceCodeSurface makes a
// caller granted only repo-a also see repo-b's code-topic evidence (touched
// symbols and matched files), because investigateCodeTopic itself performs a
// corpus-wide search with no repo predicate. This drives the real content-store
// type-assertion path (h.Content.(codeTopicContentInvestigator)), the same
// path production's *ContentReader satisfies -- not a Neo4j-only fake.
func TestInvestigateChangeSurfaceScopedFiltersCrossTenantTopicEvidence(t *testing.T) {
	t.Parallel()

	content := fakeChangeSurfaceTopicContentStore{topicRows: []codeTopicEvidenceRow{
		{SourceKind: "entity", RepoID: "repo-a", RelativePath: "handlers/auth.go", EntityID: "entity-a", EntityName: "Authenticate"},
		{SourceKind: "entity", RepoID: "repo-b", RelativePath: "handlers/auth.go", EntityID: "entity-b", EntityName: "AuthenticateOther"},
	}}
	handler := &ImpactHandler{Neo4j: fakeGraphReaderWithSingle{}, Content: content, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/change-surface/investigate", bytes.NewBufferString(`{"topic":"auth"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := decodeFlatJSONBody(t, w)
	codeSurface := requireMap(t, resp, "code_surface")
	symbols, _ := codeSurface["touched_symbols"].([]any)
	if got, want := len(symbols), 1; got != want {
		t.Fatalf("len(touched_symbols) = %d, want %d (must exclude repo-b's evidence); body = %s", got, want, w.Body.String())
	}
	symbol := symbols[0].(map[string]any)
	if got, want := symbol["entity_id"], "entity-a"; got != want {
		t.Fatalf("touched_symbols[0].entity_id = %#v, want %#v", got, want)
	}
}

// --- analyze_pre_change_impact / plan_developer_change ---

// TestAnalyzePreChangeImpactScopedRepoGrantAndDeny proves the required
// repo_id (changed_paths cannot be scoped otherwise) is bound to the grant.
func TestAnalyzePreChangeImpactScopedRepoGrantAndDeny(t *testing.T) {
	t.Parallel()
	body := `{"repo_id":"repo-a","changed_paths":["main.go"]}`

	t.Run("granted repo_id succeeds", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: fakeGraphReaderWithSingle{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
		}
	})

	t.Run("denied repo_id renders not found", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: fakeGraphReaderWithSingle{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-b", []string{"repo-b"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusNotFound; got != want {
			t.Fatalf("status = %d, want %d (repo_id outside the grant must render not-found, never a cross-tenant read); body = %s", got, want, w.Body.String())
		}
	})
}

// TestPlanDeveloperChangeScopedRepoGrantAndDeny proves plan_developer_change
// (a thin wrapper over the same preChangeImpactResponse pipeline) inherits the
// same repo_id grant check.
func TestPlanDeveloperChangeScopedRepoGrantAndDeny(t *testing.T) {
	t.Parallel()
	body := `{"repo_id":"repo-a","changed_paths":["main.go"]}`

	t.Run("granted repo_id succeeds", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: fakeGraphReaderWithSingle{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/developer-change-plan", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusOK; got != want {
			t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
		}
	})

	t.Run("denied repo_id renders not found", func(t *testing.T) {
		t.Parallel()
		handler := &ImpactHandler{Neo4j: fakeGraphReaderWithSingle{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/developer-change-plan", bytes.NewBufferString(body))
		req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-b", []string{"repo-b"})))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if got, want := w.Code, http.StatusNotFound; got != want {
			t.Fatalf("status = %d, want %d (repo_id outside the grant must render not-found); body = %s", got, want, w.Body.String())
		}
	})
}

// TestAuthMiddlewareWithScopedTokensAllowsPreChangeFamily is the
// real-middleware round trip for analyze_pre_change_impact and
// plan_developer_change.
func TestAuthMiddlewareWithScopedTokensAllowsPreChangeFamily(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/api/v0/impact/pre-change", "/api/v0/impact/developer-change-plan"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Neo4j: fakeGraphReaderWithSingle{}, Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)
			resolver := &fakeScopedTokenResolver{context: scopedTestAuthContext("tenant-a", []string{"repo-a"}), ok: true}
			middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"repo_id":"repo-a","changed_paths":["main.go"]}`))
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
			}
		})
	}
}

// --- trace_deployment_chain / investigate_deployment_config ---

// deploymentTraceTestGraph returns a fake Neo4j reader that resolves a single
// workload (repo-a) and its deployment sources, including one cross-tenant
// deployment-source repository (repo-b), to prove
// #5167 W3 filters that row out for a scoped caller. Every other query used
// by the enrichment pipeline (instances, dependencies, infrastructure, cloud
// resources) safely returns no rows.
func deploymentTraceTestGraph() fakeGraphReaderWithSingle {
	return fakeGraphReaderWithSingle{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload) WHERE"):
				return map[string]any{"id": "workload:orders-api", "name": "orders-api", "kind": "service", "repo_id": "repo-a"}, nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return map[string]any{"name": "orders-api-repo"}, nil
			default:
				return nil, nil
			}
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "DEPLOYMENT_SOURCE") {
				return []map[string]any{
					{"repo_id": "repo-a", "repo_name": "orders-api-repo", "confidence": 1.0, "reason": "canonical"},
					{"repo_id": "repo-b", "repo_name": "other-tenant-repo", "confidence": 1.0, "reason": "canonical"},
				}, nil
			}
			return nil, nil
		},
	}
}

// TestTraceDeploymentChainScopedFiltersCrossTenantDeploymentSource is a
// #5167 W3 mutation-check route: removing filterRowsByRepoIDForAccess in
// impact_trace_deployment.go's traceDeploymentChain makes repo-b (the
// cross-tenant deployment source) appear in deployment_sources even though
// the caller is granted only repo-a.
func TestTraceDeploymentChainScopedFiltersCrossTenantDeploymentSource(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Neo4j: deploymentTraceTestGraph(), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", bytes.NewBufferString(`{"service_name":"orders-api","direct_only":true}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := decodeFlatJSONBody(t, w)
	sources, _ := resp["deployment_sources"].([]any)
	for _, row := range sources {
		source := row.(map[string]any)
		if source["repo_id"] == "repo-b" {
			t.Fatalf("deployment_sources contains cross-tenant repo-b: %#v", sources)
		}
	}
}

// TestInvestigateDeploymentConfigScopedFiltersCrossTenantDeploymentSource is
// the same proof for investigate_deployment_config.
func TestInvestigateDeploymentConfigScopedFiltersCrossTenantDeploymentSource(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Neo4j: deploymentTraceTestGraph(), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/deployment-config-influence", bytes.NewBufferString(`{"service_name":"orders-api"}`))
	req = req.WithContext(ContextWithAuthContext(req.Context(), scopedTestAuthContext("tenant-a", []string{"repo-a"})))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	resp := decodeFlatJSONBody(t, w)
	influencing, _ := resp["influencing_repositories"].([]any)
	for _, row := range influencing {
		repo := row.(map[string]any)
		if repo["repo_id"] == "repo-b" {
			t.Fatalf("influencing_repositories contains cross-tenant repo-b: %#v", influencing)
		}
	}
}

// TestAuthMiddlewareWithScopedTokensAllowsDeploymentTraceFamily is the
// real-middleware round trip for trace_deployment_chain and
// investigate_deployment_config.
func TestAuthMiddlewareWithScopedTokensAllowsDeploymentTraceFamily(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path string
		body string
	}{
		{"/api/v0/impact/trace-deployment-chain", `{"service_name":"orders-api","direct_only":true}`},
		{"/api/v0/impact/deployment-config-influence", `{"service_name":"orders-api"}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			handler := &ImpactHandler{Neo4j: deploymentTraceTestGraph(), Profile: ProfileLocalAuthoritative}
			mux := http.NewServeMux()
			handler.Mount(mux)
			resolver := &fakeScopedTokenResolver{context: scopedTestAuthContext("tenant-a", []string{"repo-a"}), ok: true}
			middleware := AuthMiddlewareWithScopedTokens("", resolver, mux)

			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer scoped-token")
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d (middleware must not 403 a granted scoped caller); body = %s", got, want, w.Body.String())
			}
		})
	}
}
