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

// crossTenantProvisioningGraph resolves the orders-api workload anchored on the
// grant-checked repo-a and returns ONE cross-tenant provisioning candidate from
// queryProvisioningRepositoryCandidates: repo-b, a DIFFERENT tenant's repository
// that provisions/deploys/consumes repo-a. The candidate cypher anchors on
// (target:Repository {id:$repo_id}) -- the service's own repo -- and traverses
// to the FAR repo with NO grant predicate, so repo.id/repo.name name a
// cross-tenant repo. Those candidates feed workloadContext["dependents"],
// ["consumer_repositories"], and ["provisioning_source_chains"] unfiltered
// (service_query_enrichment.go, #5167 W3 P0 fifth vector). Every other
// enrichment query returns no rows.
func crossTenantProvisioningGraph() fakeGraphReaderWithSingle {
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
			if rows, ok := impactEvidenceWorkloadRepositoryRows(cypher); ok {
				return rows, nil
			}
			if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM]-(repo:Repository)") {
				return []map[string]any{{
					"repo_id":             "repo-b",
					"repo_name":           "other-tenant-infra",
					"relationship_type":   "PROVISIONS_DEPENDENCY_FOR",
					"relationship_reason": "provisions",
				}}, nil
			}
			return nil, nil
		},
	}
}

// TestW3RoutesScopedFilterCrossTenantProvisioningCandidates is the #5167 W3 P0
// (fifth vector) mutation-check for the provisioning-candidate leak. A scoped
// caller granted only repo-a must never see cross-tenant repo-b in
// dependents/consumer_repositories/provisioning_source_chains on any route that
// runs enrichServiceQueryContextWithOptions: GET /services/{name}/context,
// GET /workloads/{id}/context, and POST /impact/trace-deployment-chain.
//
// The Content store is wired (fakePortContentStore{}) on purpose: nil Content
// short-circuits enrichment before the provisioning block, which is exactly why
// prior graph-only W3 tests false-greened past this vector. Removing the
// candidate grant filter turns the scoped assertions red; the all-scope control
// proves the rows genuinely flow when unfiltered.
func TestW3RoutesScopedFilterCrossTenantProvisioningCandidates(t *testing.T) {
	t.Parallel()

	const crossTenant = "repo-b"
	const crossTenantName = "other-tenant-infra"

	newEntityMux := func() *http.ServeMux {
		handler := &EntityHandler{Neo4j: crossTenantProvisioningGraph(), Content: fakePortContentStore{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		return mux
	}
	newImpactMux := func() *http.ServeMux {
		handler := &ImpactHandler{Neo4j: crossTenantProvisioningGraph(), Content: fakePortContentStore{}, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		return mux
	}

	cases := []struct {
		name string
		mux  func() *http.ServeMux
		req  func() *http.Request
	}{
		{
			name: "service_context",
			mux:  newEntityMux,
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/api/v0/services/orders-api/context", nil)
			},
		},
		{
			name: "workload_context",
			mux:  newEntityMux,
			req: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload:orders-api/context", nil)
			},
		},
		{
			name: "impact_trace_deployment_chain",
			mux:  newImpactMux,
			req: func() *http.Request {
				body := `{"service_name":"orders-api","direct_only":false,"include_related_module_usage":true}`
				return httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", bytes.NewBufferString(body))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			serve := func(auth *AuthContext) string {
				req := tc.req()
				if auth != nil {
					req = req.WithContext(ContextWithAuthContext(req.Context(), *auth))
				}
				w := httptest.NewRecorder()
				tc.mux().ServeHTTP(w, req)
				if got, want := w.Code, http.StatusOK; got != want {
					t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
				}
				return w.Body.String()
			}

			allScope := serve(nil)
			if !strings.Contains(allScope, crossTenant) || !strings.Contains(allScope, crossTenantName) {
				t.Fatalf("all-scope caller: expected cross-tenant provisioning candidate present in unfiltered response, got: %s", allScope)
			}

			scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
			scopedBody := serve(&scoped)
			if strings.Contains(scopedBody, crossTenant) || strings.Contains(scopedBody, crossTenantName) {
				t.Fatalf("scoped caller granted only repo-a leaked cross-tenant %q via a provisioning-candidate field (dependents/consumer_repositories/provisioning_source_chains): %s", crossTenant, scopedBody)
			}
		})
	}
}
