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

// crossTenantRelationshipReadModel is the read-model (Postgres resolved_relationships)
// shape GET /repositories/{id}/context prefers over the graph helpers. Anchored
// on repo-a, it carries an OUTGOING DEPENDS_ON to cross-tenant repo-b (leaks via
// relationships[].target_id/target_name and relationship_overview) and an
// INCOMING DEPENDS_ON from repo-b (leaks via consumers[].id/name and
// relationship_overview). Neither the read-model SQL nor the emit sites in
// repository_context.go filter the related endpoint by grant (#5167 W3 P0,
// fourth vector).
func crossTenantRelationshipReadModel() repositoryRelationshipReadModel {
	return repositoryRelationshipReadModel{
		Available: true,
		Relationships: []map[string]any{
			{
				"direction":     "outgoing",
				"type":          "DEPENDS_ON",
				"source_name":   "orders-api-repo",
				"source_id":     "repo-a",
				"target_name":   "other-tenant-infra",
				"target_id":     "repo-b",
				"resolved_id":   "resolved-out",
				"generation_id": "g1",
			},
			{
				"direction":     "incoming",
				"type":          "DEPENDS_ON",
				"source_name":   "other-tenant-infra",
				"source_id":     "repo-b",
				"target_name":   "orders-api-repo",
				"target_id":     "repo-a",
				"resolved_id":   "resolved-in",
				"generation_id": "g2",
			},
		},
		Consumers: []map[string]any{
			{"id": "repo-b", "name": "other-tenant-infra"},
		},
	}
}

// crossTenantDeployableUnitSupplementGraph resolves the repo-a base row and
// returns ONE cross-tenant CORRELATES_DEPLOYABLE_UNIT supplement edge (repo-a ->
// repo-c) from queryRepoDeployableUnitRelationshipOverview, which calls the
// UNFILTERED inner queryRepoRelationshipOverviewDirection and is merged into the
// read model at repository_context.go. Every other graph query returns no rows.
func crossTenantDeployableUnitSupplementGraph(t *testing.T) fakeRepoGraphReader {
	t.Helper()
	return fakeRepoGraphReader{
		runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id})") {
				return map[string]any{
					"id":         "repo-a",
					"name":       "orders-api-repo",
					"path":       "/repos/orders-api",
					"local_path": "/repos/orders-api",
					"has_remote": false,
				}, nil
			}
			return nil, nil
		},
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if !strings.Contains(cypher, "[rel:CORRELATES_DEPLOYABLE_UNIT]") || !strings.Contains(cypher, "'outgoing' AS direction") {
				return nil, nil
			}
			return []map[string]any{{
				"direction":     "outgoing",
				"type":          "CORRELATES_DEPLOYABLE_UNIT",
				"source_name":   "orders-api-repo",
				"source_id":     "repo-a",
				"target_name":   "other-tenant-deploy",
				"target_id":     "repo-c",
				"resolved_id":   "deployable-unit-correlation:g3:repo-a:repo-c",
				"generation_id": "g3",
			}}, nil
		},
	}
}

// TestRepositoryContextReadModelScopedFiltersCrossTenantRelationships is the
// #5167 W3 P0 (fourth vector) mutation-check for the production-PRIMARY
// read-model path of GET /repositories/{id}/context. A scoped caller granted
// only repo-a, whose anchor passes selector resolution, must never see
// cross-tenant repo-b (via relationships/consumers/relationship_overview) or the
// unfiltered deployable-unit merge's repo-c. Removing the read-model grant
// filter turns the scoped assertions red; the all-scope control proves the rows
// genuinely flow when unfiltered.
func TestRepositoryContextReadModelScopedFiltersCrossTenantRelationships(t *testing.T) {
	t.Parallel()

	get := func(auth *AuthContext) string {
		handler := &RepositoryHandler{
			Neo4j: crossTenantDeployableUnitSupplementGraph(t),
			Content: fakePortContentStore{
				relationshipReadModel: crossTenantRelationshipReadModel(),
			},
			Profile: ProfileLocalAuthoritative,
		}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-a/context", nil)
		req.SetPathValue("repo_id", "repo-a")
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

	leaked := []string{"repo-b", "other-tenant-infra", "repo-c", "other-tenant-deploy"}

	allScope := get(nil)
	for _, needle := range leaked {
		if !strings.Contains(allScope, needle) {
			t.Fatalf("all-scope caller: expected cross-tenant %q present in unfiltered read-model response, got: %s", needle, allScope)
		}
	}

	scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
	scopedBody := get(&scoped)
	for _, needle := range leaked {
		if strings.Contains(scopedBody, needle) {
			t.Fatalf("scoped caller granted only repo-a leaked cross-tenant %q via the read-model relationship path: %s", needle, scopedBody)
		}
	}
}
