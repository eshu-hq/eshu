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

// crossTenantDependencyGraph resolves the orders-api workload anchored on the
// grant-checked repo-a, and returns ONE cross-tenant repository relationship
// from queryRepoDependencies: a DEPENDS_ON edge whose target repository is
// repo-b (a DIFFERENT tenant). queryRepoDependencies anchors only on
// (r:Repository {id:$repo_id}) -- the anchor repo-a -- and applies NO grant
// predicate to the target repository, so target.name/target.id name a
// cross-tenant repo. This exercises the #5167 W3 P0 third-vector leak
// (entity_workload_context.go:fetchWorkloadContextForOperation ->
// queryRepoDependencies), which feeds dependencies[] on both
// GET /services/{name}/context and GET /workloads/{id}/context. Every other
// enrichment query returns no rows.
func crossTenantDependencyGraph() fakeGraphReaderWithSingle {
	return fakeGraphReaderWithSingle{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload) WHERE"):
				return map[string]any{"id": "workload:orders-api", "name": "orders-api", "kind": "service", "repo_id": "repo-a"}, nil
			case strings.Contains(cypher, "RETURN r.name as repo_name"):
				return map[string]any{"repo_name": "orders-api-repo"}, nil
			default:
				return nil, nil
			}
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			// #5390 resolves the workload's owning repo via a DEFINES traversal;
			// stub it so the all-scope control populates the repo-keyed enrichment.
			if strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)") {
				return []map[string]any{{"repo_id": "repo-a", "repo_name": "orders-api-repo"}}, nil
			}
			if strings.Contains(cypher, "RETURN type(rel) AS type, target.name AS target_name") {
				return []map[string]any{{
					"type":        "DEPENDS_ON",
					"target_name": "other-tenant-infra",
					"target_id":   "repo-b",
				}}, nil
			}
			return nil, nil
		},
	}
}

const crossTenantDependencyRepo = "repo-b"

// TestServiceAndWorkloadContextScopedFiltersCrossTenantDependency is the #5167
// W3 P0 (third-vector) mutation-check for the repository-relationship helper
// leak on the /services/{name}/context and /workloads/{id}/context routes: a
// scoped caller granted only repo-a, whose service is backed by repo-a, must
// never see cross-tenant repo-b's name/id in dependencies[]. Removing the grant
// filter from queryRepoDependencies (repository_context_helpers.go) turns the
// scoped assertions red; the all-scope control proves the row genuinely flows
// when unfiltered.
func TestServiceAndWorkloadContextScopedFiltersCrossTenantDependency(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
	}{
		{"service_context", "/api/v0/services/orders-api/context"},
		{"workload_context", "/api/v0/workloads/workload:orders-api/context"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			get := func(auth *AuthContext) string {
				handler := &EntityHandler{Neo4j: crossTenantDependencyGraph(), Profile: ProfileLocalAuthoritative}
				mux := http.NewServeMux()
				handler.Mount(mux)
				req := httptest.NewRequest(http.MethodGet, tc.path, nil)
				if auth != nil {
					req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				if got, want := w.Code, http.StatusOK; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
				}
				return w.Body.String()
			}

			allScope := get(nil)
			if !strings.Contains(allScope, crossTenantDependencyRepo) || !strings.Contains(allScope, "other-tenant-infra") {
				t.Fatalf("all-scope caller: expected cross-tenant dependency present in unfiltered response, got: %s", allScope)
			}

			scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
			scopedBody := get(&scoped)
			if strings.Contains(scopedBody, crossTenantDependencyRepo) || strings.Contains(scopedBody, "other-tenant-infra") {
				t.Fatalf("scoped caller granted only repo-a saw cross-tenant repo-b in dependencies[]: %s", scopedBody)
			}
		})
	}
}
