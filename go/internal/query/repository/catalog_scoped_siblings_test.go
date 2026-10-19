// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// T1 row of the #5385 migration test table: a token granted only alpha
// reading /catalog for a workload name defined in two repositories sees the
// alpha row with same_name_siblings = 0 (Decision D2: the edge crosses
// tenants, so a scoped read counts admitted siblings only) and no beta
// repo_id anywhere in the response body.
func TestCatalogScopedTokenSeesNoSibling(t *testing.T) {
	t.Parallel()

	reader := graph.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{
					{"id": "repository:r_alpha", "name": "alpha"},
					{"id": "repository:r_beta", "name": "beta"},
				}, nil
			case strings.Contains(cypher, "SAME_NAME"):
				return []map[string]any{
					{"id": "workload:repository:r_alpha:checkout", "sibling_id": "workload:repository:r_beta:checkout", "sibling_repo_id": "repository:r_beta"},
					{"id": "workload:repository:r_beta:checkout", "sibling_id": "workload:repository:r_alpha:checkout", "sibling_repo_id": "repository:r_alpha"},
				}, nil
			case strings.Contains(cypher, "(w:Workload)<-[:DEFINES]-(repo:Repository)"):
				return []map[string]any{
					{"id": "workload:repository:r_alpha:checkout", "repo_id": "repository:r_alpha", "repo_name": "alpha"},
					{"id": "workload:repository:r_beta:checkout", "repo_id": "repository:r_beta", "repo_name": "beta"},
				}, nil
			case strings.Contains(cypher, "MATCH (inst:WorkloadInstance)"):
				return nil, nil
			case strings.Contains(cypher, "EVIDENCES_REPOSITORY_RELATIONSHIP"):
				return nil, nil
			case strings.Contains(cypher, "MATCH (w:Workload)"):
				return []map[string]any{
					{"id": "workload:repository:r_alpha:checkout", "name": "checkout", "kind": "service"},
					{"id": "workload:repository:r_beta:checkout", "name": "checkout", "kind": "service"},
				}, nil
			default:
				return nil, nil
			}
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-alpha",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-alpha",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repository:r_alpha"},
	}))
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "repository:r_beta") {
		t.Fatalf("scoped catalog body leaks the ungranted repository: %s", rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	workloads, ok := body["workloads"].([]any)
	if !ok || len(workloads) != 1 {
		t.Fatalf("workloads = %#v, want exactly the alpha row", body["workloads"])
	}
	row := workloads[0].(map[string]any)
	if got, want := row["id"], "workload:repository:r_alpha:checkout"; got != want {
		t.Fatalf("workload id = %#v, want %#v", got, want)
	}
	if got, want := row["same_name_siblings"], float64(0); got != want {
		t.Fatalf("same_name_siblings = %#v, want %#v (admitted siblings only)", got, want)
	}
}

// The same corpus under an unscoped caller keeps both split siblings, each
// counting the other: the grant filter is scoped-only, so shared/admin/local
// callers (and the golden-corpus gate) see the pre-existing full listing.
func TestCatalogUnscopedCallerSeesBothSiblings(t *testing.T) {
	t.Parallel()

	reader := graph.FakeRepoGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{
					{"id": "repository:r_alpha", "name": "alpha"},
					{"id": "repository:r_beta", "name": "beta"},
				}, nil
			case strings.Contains(cypher, "SAME_NAME"):
				return []map[string]any{
					{"id": "workload:repository:r_alpha:checkout", "sibling_id": "workload:repository:r_beta:checkout", "sibling_repo_id": "repository:r_beta"},
					{"id": "workload:repository:r_beta:checkout", "sibling_id": "workload:repository:r_alpha:checkout", "sibling_repo_id": "repository:r_alpha"},
				}, nil
			case strings.Contains(cypher, "(w:Workload)<-[:DEFINES]-(repo:Repository)"):
				return []map[string]any{
					{"id": "workload:repository:r_alpha:checkout", "repo_id": "repository:r_alpha", "repo_name": "alpha"},
					{"id": "workload:repository:r_beta:checkout", "repo_id": "repository:r_beta", "repo_name": "beta"},
				}, nil
			case strings.Contains(cypher, "MATCH (w:Workload)"):
				return []map[string]any{
					{"id": "workload:repository:r_alpha:checkout", "name": "checkout", "kind": "service"},
					{"id": "workload:repository:r_beta:checkout", "name": "checkout", "kind": "service"},
				}, nil
			default:
				return nil, nil
			}
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}

	req := httptest.NewRequest(http.MethodGet, "/api/v0/catalog?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.listCatalog(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	workloads, ok := body["workloads"].([]any)
	if !ok || len(workloads) != 2 {
		t.Fatalf("workloads = %#v, want both split siblings", body["workloads"])
	}
	for _, raw := range workloads {
		row := raw.(map[string]any)
		if got, want := row["same_name_siblings"], float64(1); got != want {
			t.Fatalf("%v same_name_siblings = %#v, want %#v", row["id"], got, want)
		}
	}
}
