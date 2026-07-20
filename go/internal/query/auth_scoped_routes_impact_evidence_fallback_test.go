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

// fallbackArtifactOverviewGraph resolves the orders-api workload (repo-a) with
// ZERO deployment-evidence EvidenceArtifact rows in either direction (both the
// outgoing and incoming HAS_DEPLOYMENT_EVIDENCE/EVIDENCES_REPOSITORY_RELATIONSHIP
// traversals fall to the default nil case), so queryServiceGraphDeploymentEvidence
// leaves workloadContext["deployment_evidence"] fully empty -- the exact
// production state ("the redacted evidence set is FULLY EMPTY") that makes
// loadServiceDeploymentEvidence fall through to loadDeploymentArtifactOverview.
// It resolves ONE related-repository artifact source via the
// DEPENDS_ON|USES_MODULE|... traversal in queryRelatedRepositoryArtifactSources:
// repo-b, a DIFFERENT tenant's repository. Every other enrichment query returns
// no rows.
func fallbackArtifactOverviewGraph() fakeGraphReaderWithSingle {
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
			if strings.Contains(cypher, "DEPENDS_ON|USES_MODULE") {
				return []map[string]any{{"repo_id": "repo-b", "repo_name": "other-tenant-infra"}}, nil
			}
			return nil, nil
		},
	}
}

// TestServiceContextFallbackArtifactOverviewScopedFiltersCrossTenantRepo is the
// #5167 W3 P0 (third round) mutation-check for the deployment-artifact-overview
// FALLBACK: loadServiceDeploymentEvidence (service_deployment_evidence.go) falls
// through to loadDeploymentArtifactOverview -> loadSharedRepositoryConfigArtifacts
// -> queryRelatedRepositoryArtifactSources exactly when the redacted graph
// deployment_evidence set is fully empty -- which happens whenever every
// artifact named an out-of-grant endpoint (or, as here, there simply is no
// EvidenceArtifact). queryRelatedRepositoryArtifactSources had NO access
// filter, so a scoped caller granted only repo-a still saw repo-b's config
// artifact (config_paths[].source_repo / shared_config_paths[].source_repo)
// merged into deployment_evidence, deployment_artifacts, and
// infrastructure_overview. Removing filterRepositoryArtifactSourcesForAccess (or
// its repositoryAccessFilterFromContext call) in queryRelatedRepositoryArtifactSources
// turns the scoped assertion red.
func TestServiceContextFallbackArtifactOverviewScopedFiltersCrossTenantRepo(t *testing.T) {
	t.Parallel()

	content := fakePortContentStore{repoFiles: []FileContent{{
		RepoID:       "repo-b",
		RelativePath: "env/prod.tfvars",
		Content:      "environment = \"prod\"\n",
	}}}

	get := func(auth *AuthContext) string {
		handler := &EntityHandler{Neo4j: fallbackArtifactOverviewGraph(), Content: content, Profile: ProfileLocalAuthoritative}
		mux := http.NewServeMux()
		handler.Mount(mux)
		req := httptest.NewRequest(http.MethodGet, "/api/v0/services/orders-api/context", nil)
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
	if !strings.Contains(allScope, "other-tenant-infra") {
		t.Fatalf("all-scope caller: expected cross-tenant repo-b artifact source present in unfiltered fallback overview, got: %s", allScope)
	}

	scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
	scopedBody := get(&scoped)
	if strings.Contains(scopedBody, "other-tenant-infra") {
		t.Fatalf("scoped caller granted only repo-a saw cross-tenant repo-b via the deployment-artifact-overview fallback: %s", scopedBody)
	}
}
