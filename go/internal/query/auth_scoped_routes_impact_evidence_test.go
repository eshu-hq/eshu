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

// crossTenantEvidenceGraph resolves the orders-api workload (anchored on the
// grant-checked repo-a) and returns ONE cross-tenant deployment-evidence
// artifact from the incoming-direction graph traversal: an EvidenceArtifact
// whose non-anchor endpoint (source_repo_id) is repo-b, a DIFFERENT tenant's
// repository. This exercises the #5167 W3 P0 leak path
// (enrichServiceQueryContextWithOptions -> queryServiceGraphDeploymentEvidence
// -> queryRepoDeploymentEvidence graph traversal; ImpactHandler.Content stays
// nil so the read-model path is skipped and the graph path runs). Every other
// enrichment query returns no rows.
//
// The cross-tenant repo surfaces in the response through deployment_evidence
// AND its derivatives: deployment_config-influence's influencing_repositories
// (role configuration_artifact) and read_first_files (a
// get_file_lines(repo_id, path) suggestion naming repo-b + the file), and
// trace_deployment_chain's serialized deployment_evidence / artifact_lineage.
func crossTenantEvidenceGraph() fakeGraphReaderWithSingle {
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
			// Incoming deployment-evidence traversal: anchor repo-a is the target,
			// the cross-tenant repo-b is the source (non-anchor) endpoint.
			if strings.Contains(cypher, "(artifact:EvidenceArtifact)-[:EVIDENCES_REPOSITORY_RELATIONSHIP]->(r:Repository {id: $repo_id})") {
				return []map[string]any{{
					"direction":         "incoming",
					"artifact_id":       "artifact-xtenant",
					"name":              "prod-values.yaml",
					"domain":            "deployment",
					"path":              "deploy/prod/values.yaml",
					"evidence_kind":     "helm_values",
					"artifact_family":   "helm",
					"relationship_type": "DEPLOYS_FROM",
					"environment":       "prod",
					"matched_alias":     "orders-api",
					"matched_value":     "orders-api",
					"source_repo_id":    "repo-b",
					"source_repo_name":  "other-tenant-infra",
					"target_repo_id":    "repo-a",
					"target_repo_name":  "orders-api-repo",
				}}, nil
			}
			return nil, nil
		},
	}
}

const crossTenantEvidenceRepo = "repo-b"

// postImpactEvidence drives one impact route through Mount(mux) with the given
// auth context and returns the raw response body. A nil auth context leaves the
// request unauthenticated, which repositoryAccessFilterFromContext treats as
// all-scopes -- the "sees everything" control case.
func postImpactEvidence(t *testing.T, path, body string, auth *AuthContext) string {
	t.Helper()
	handler := &ImpactHandler{Neo4j: crossTenantEvidenceGraph(), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
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

// TestTraceDeploymentChainScopedFiltersCrossTenantDeploymentEvidence is the
// #5167 W3 P0 mutation-check: removing filterDeploymentEvidenceRowsForAccess in
// repository_deployment_evidence.go (or repository_deployment_evidence_read_model.go)
// makes trace_deployment_chain serialize repo-b's deployment_evidence artifact
// (and its artifact_lineage/provenance derivatives) to a caller granted only
// repo-a. The all-scope control proves the row genuinely flows when unfiltered,
// so a removed filter turns the scoped assertion red.
func TestTraceDeploymentChainScopedFiltersCrossTenantDeploymentEvidence(t *testing.T) {
	t.Parallel()

	const path = "/api/v0/impact/trace-deployment-chain"
	const body = `{"service_name":"orders-api","direct_only":true}`

	allScope := postImpactEvidence(t, path, body, nil)
	if !strings.Contains(allScope, crossTenantEvidenceRepo) {
		t.Fatalf("all-scope caller: expected cross-tenant %q present in unfiltered response, got: %s", crossTenantEvidenceRepo, allScope)
	}

	scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
	body2 := postImpactEvidence(t, path, body, &scoped)
	if strings.Contains(body2, crossTenantEvidenceRepo) || strings.Contains(body2, "other-tenant-infra") {
		t.Fatalf("scoped caller granted only repo-a saw cross-tenant repo-b deployment evidence: %s", body2)
	}
}

// TestInvestigateDeploymentConfigScopedFiltersCrossTenantEvidence is the same
// #5167 W3 P0 mutation-check for investigate_deployment_config, asserting the
// leak-prone derivatives specifically: influencing_repositories must not name
// repo-b, and read_first_files must not return a get_file_lines suggestion
// pointing at repo-b's file.
func TestInvestigateDeploymentConfigScopedFiltersCrossTenantEvidence(t *testing.T) {
	t.Parallel()

	const path = "/api/v0/impact/deployment-config-influence"
	const body = `{"service_name":"orders-api"}`

	allScope := postImpactEvidence(t, path, body, nil)
	if !strings.Contains(allScope, crossTenantEvidenceRepo) {
		t.Fatalf("all-scope caller: expected cross-tenant %q present in unfiltered response, got: %s", crossTenantEvidenceRepo, allScope)
	}

	scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
	bodyStr := postImpactEvidence(t, path, body, &scoped)
	if strings.Contains(bodyStr, crossTenantEvidenceRepo) || strings.Contains(bodyStr, "other-tenant-infra") {
		t.Fatalf("scoped caller granted only repo-a saw cross-tenant repo-b evidence in deployment-config-influence: %s", bodyStr)
	}
}

// TestServiceContextScopedFiltersCrossTenantDeploymentEvidence proves the same
// root fix closes the PRE-EXISTING leak through the already-allowlisted
// GET /api/v0/services/{service_name}/context (#5167 W3 P0: the reviewer found
// the unfiltered deployment_evidence was already reachable there before this
// PR opened the impact routes). Filtering at the shared source
// (queryRepoDeploymentEvidence) binds this route too.
func TestServiceContextScopedFiltersCrossTenantDeploymentEvidence(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{Neo4j: crossTenantEvidenceGraph(), Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	get := func(auth *AuthContext) string {
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
	if !strings.Contains(allScope, crossTenantEvidenceRepo) {
		t.Fatalf("all-scope caller: expected cross-tenant %q present in unfiltered service context, got: %s", crossTenantEvidenceRepo, allScope)
	}

	scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
	scopedBody := get(&scoped)
	if strings.Contains(scopedBody, crossTenantEvidenceRepo) || strings.Contains(scopedBody, "other-tenant-infra") {
		t.Fatalf("scoped caller granted only repo-a saw cross-tenant repo-b deployment evidence via /services/{name}/context: %s", scopedBody)
	}
}

// cloudFallbackGraph resolves the orders-api workload (repo-a) with NO
// materialized USES cloud resources, then returns a distinctively-named
// free-text CloudResource candidate from the given fallback query matcher --
// either fetchConfigDerivedCloudResources (`MATCH (c:CloudResource)`, reached
// inside fetchCloudResources) or loadUncorrelatedCloudResourceCandidates
// (`MATCH (n:CloudResource)`, the handler-level fallback). Both are free-text
// scans with no repo_id, so a scoped caller must skip them entirely (#5167 W3
// P2). candidateMatch selects which fallback returns the row.
func cloudFallbackGraph(candidateMatch, candidateName string) fakeGraphReaderWithSingle {
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
			if strings.Contains(cypher, candidateMatch) {
				return []map[string]any{{
					"id":       "cloud:queue:" + candidateName,
					"name":     candidateName,
					"kind":     "queue",
					"provider": "aws",
					"arn":      "arn:aws:sqs:us-east-1:999:" + candidateName,
				}}, nil
			}
			return nil, nil
		},
	}
}

// TestTraceDeploymentChainScopedSkipsFreeTextCloudFallbacks is the #5167 W3 P2
// mutation-check for the three free-text CloudResource fallback guards in the
// trace_deployment_chain path (fetchCloudResources' config-derived fallback and
// the handler-level config-derived/uncorrelated fallbacks, all gated on
// !access.scoped()). Each fallback is a name-similarity CloudResource scan with
// no repo_id, so a scoped caller must never see its candidate; an all-scope
// caller does. Removing any guard makes the scoped assertion red.
func TestTraceDeploymentChainScopedSkipsFreeTextCloudFallbacks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		candidateMatch string
		candidateName  string
	}{
		// fetchConfigDerivedCloudResources runs inside fetchCloudResources when
		// the materialized USES query returns zero rows.
		{"config_derived_via_fetchCloudResources", "MATCH (c:CloudResource)", "config-derived-queue"},
		// loadUncorrelatedCloudResourceCandidates is the handler-level fallback.
		{"uncorrelated", "MATCH (n:CloudResource)", "uncorrelated-queue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			run := func(auth *AuthContext) string {
				handler := &ImpactHandler{Neo4j: cloudFallbackGraph(tc.candidateMatch, tc.candidateName), Profile: ProfileLocalAuthoritative}
				mux := http.NewServeMux()
				handler.Mount(mux)
				req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", bytes.NewBufferString(`{"service_name":"orders-api","direct_only":true}`))
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

			allScope := run(nil)
			if !strings.Contains(allScope, tc.candidateName) {
				t.Fatalf("all-scope caller: expected free-text candidate %q present, got: %s", tc.candidateName, allScope)
			}

			scoped := scopedTestAuthContext("tenant-a", []string{"repo-a"})
			scopedBody := run(&scoped)
			if strings.Contains(scopedBody, tc.candidateName) {
				t.Fatalf("scoped caller saw free-text CloudResource fallback candidate %q (no repo_id to bind to a grant): %s", tc.candidateName, scopedBody)
			}
		})
	}
}
