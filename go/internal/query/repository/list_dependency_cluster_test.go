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

// TestListRepositoriesGroupsByDependencyCluster proves the end-to-end handler
// path: given repos A->B, B->C and isolated D, the list response groups A/B/C
// under one dependency-cluster group_key and leaves D as missing_evidence.
func TestListRepositoriesGroupsByDependencyCluster(t *testing.T) {
	t.Parallel()

	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(4)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				// dependency-cluster edge pre-pass
				return dependencyEdgeRowsForRead(cypher, []map[string]any{
					{"source_id": "repository:a", "target_id": "repository:b"},
					{"source_id": "repository:b", "target_id": "repository:c"},
				}), nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				// page query
				return []map[string]any{
					{"id": "repository:a", "name": "a"},
					{"id": "repository:b", "name": "b"},
					{"id": "repository:c", "name": "c"},
					{"id": "repository:d", "name": "d"},
				}, nil
			default:
				return []map[string]any{{"total": 4}}, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}

	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data := envelope.Data.(map[string]any)
	repositories := data["repositories"].([]any)
	if len(repositories) != 4 {
		t.Fatalf("repositories len = %d, want 4", len(repositories))
	}

	groups := map[string]map[string]string{}
	for _, raw := range repositories {
		repo := raw.(map[string]any)
		groups[querycontract.StringVal(repo, "id")] = map[string]string{
			"source": querycontract.StringVal(repo, "group_source"),
			"key":    querycontract.StringVal(repo, "group_key"),
		}
	}

	for _, id := range []string{"repository:a", "repository:b", "repository:c"} {
		if groups[id]["source"] != repositoryGroupSourceDependencyCluster {
			t.Errorf("%s group_source = %q, want %q", id, groups[id]["source"], repositoryGroupSourceDependencyCluster)
		}
		if groups[id]["key"] != "repository:a" {
			t.Errorf("%s group_key = %q, want repository:a", id, groups[id]["key"])
		}
	}
	if groups["repository:d"]["source"] != repositoryGroupSourceMissing {
		t.Errorf("repository:d group_source = %q, want %q", groups["repository:d"]["source"], repositoryGroupSourceMissing)
	}
}

// TestListRepositoriesScopedDependencyClusterMembership proves a scoped caller
// only sees cluster membership computed from edges within their grant. The edge
// pre-pass query the handler issues must scope both endpoints, so a depender or
// dependency outside the grant cannot pull an in-grant repo into a cross-grant
// cluster.
func TestListRepositoriesScopedDependencyClusterMembership(t *testing.T) {
	t.Parallel()

	var capturedEdgeCypher string
	reader := graph.FakeRepoGraphReader{
		RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
			return map[string]any{"total": int64(1)}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
				capturedEdgeCypher = cypher
				return []map[string]any{}, nil
			case strings.Contains(cypher, "MATCH (r:Repository)"):
				return []map[string]any{{"id": "repository:a", "name": "a"}}, nil
			default:
				return []map[string]any{{"total": 1}}, nil
			}
		},
	}

	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories?limit=10", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		SubjectClass:         "team",
		SubjectIDHash:        "sha256:team-a",
		PolicyRevisionHash:   "sha256:policy",
		AllowedRepositoryIDs: []string{"repository:a"},
	}))
	rec := httptest.NewRecorder()

	handler.listRepositories(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if capturedEdgeCypher == "" {
		t.Fatal("dependency-cluster edge query was not issued for the scoped caller")
	}
	if !strings.Contains(capturedEdgeCypher, "s.id IN $allowed_repository_ids") ||
		!strings.Contains(capturedEdgeCypher, "t.id IN $allowed_repository_ids") {
		t.Fatalf("scoped edge query does not scope both endpoints to the grant:\n%s", capturedEdgeCypher)
	}
}
