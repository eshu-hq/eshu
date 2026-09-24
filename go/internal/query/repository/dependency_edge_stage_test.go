// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
)

// TestDependencyEdgeStageCompletionAttributes proves GET /api/v0/catalog and
// GET /api/v0/repositories both wrap their dependency-edge read in the
// repository_query stage timer (stage=dependency_cluster_edges) with the same
// completion attributes: edge_count, truncated, error, edge_scan_skipped and
// edge_transfer_capped. The list route adds cluster_count; the catalog builds
// no clusters and omits it (#6786 review R2-F10). edge_transfer_capped tells
// an operator the probe could not prove the edges fit the bound, so the read
// paid the extra group-size statement to cap its transfer (#6786 review
// R3-F2).
func TestDependencyEdgeStageCompletionAttributes(t *testing.T) {
	t.Parallel()

	routes := []struct {
		operation   string
		clusterAttr bool
		serve       func(*Handler, http.ResponseWriter, *http.Request)
		path        string
	}{
		{operation: "catalog_list", serve: (*Handler).listCatalog, path: "/api/v0/catalog?limit=10"},
		{operation: "repository_list", clusterAttr: true, serve: (*Handler).listRepositories, path: "/api/v0/repositories?limit=10"},
	}
	cases := []struct {
		name     string
		probe    []map[string]any
		probeErr error
		want     []string
	}{
		{
			name:  "edges exist within the bound",
			probe: []map[string]any{{"edge_count": int64(1)}},
			want:  []string{"edge_count=1", "truncated=false", "error=false", "edge_scan_skipped=false", "edge_transfer_capped=false"},
		},
		{
			name:  "probe proves no edges",
			probe: []map[string]any{{"edge_count": int64(0)}},
			want:  []string{"edge_count=0", "truncated=false", "error=false", "edge_scan_skipped=true", "edge_transfer_capped=false"},
		},
		{
			name:     "probe fails so the transfer is capped",
			probeErr: errors.New("probe unavailable"),
			want:     []string{"edge_count=1", "truncated=false", "error=false", "edge_scan_skipped=false", "edge_transfer_capped=true"},
		},
	}
	for _, route := range routes {
		for _, tt := range cases {
			t.Run(route.operation+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				reader := graph.FakeRepoGraphReader{
					RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
						return map[string]any{"total": int64(1)}, nil
					},
					RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
						switch {
						case cypher == RepositoryDependencyEdgeCountCypher:
							return tt.probe, tt.probeErr
						case strings.Contains(cypher, "(s:Repository)-[:DEPENDS_ON]->(t:Repository)"):
							return dependencyEdgeRowsForRead(cypher, []map[string]any{
								{"source_id": "repository:app", "target_id": "repository:lib"},
							}), nil
						case strings.Contains(cypher, "MATCH (r:Repository)"):
							return []map[string]any{{"id": "repository:lib", "name": "lib"}}, nil
						default:
							return nil, nil
						}
					},
				}
				var buf bytes.Buffer
				handler := &Handler{
					Neo4j:   reader,
					Profile: querycontract.ProfileLocalAuthoritative,
					Logger:  slog.New(slog.NewTextHandler(&buf, nil)),
				}
				req := httptest.NewRequest(http.MethodGet, route.path, nil)
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
				route.serve(handler, httptest.NewRecorder(), req)

				completed := dependencyEdgeStageCompletion(t, buf.String(), route.operation)
				for _, want := range append([]string{"duration_seconds="}, tt.want...) {
					if !strings.Contains(completed, want) {
						t.Errorf("stage_completed = %q, want it to contain %q", completed, want)
					}
				}
				if got := strings.Contains(completed, "cluster_count="); got != route.clusterAttr {
					t.Errorf("stage_completed = %q, cluster_count present = %v, want %v", completed, got, route.clusterAttr)
				}
			})
		}
	}
}

// dependencyEdgeStageCompletion returns the stage_completed log line for
// operation's dependency_cluster_edges stage, failing if either stage event
// is missing.
func dependencyEdgeStageCompletion(t *testing.T, log, operation string) string {
	t.Helper()
	var started, completed string
	for _, line := range strings.Split(log, "\n") {
		if !strings.Contains(line, "operation="+operation) || !strings.Contains(line, "stage=dependency_cluster_edges") {
			continue
		}
		switch {
		case strings.Contains(line, "repository_query.stage_started"):
			started = line
		case strings.Contains(line, "repository_query.stage_completed"):
			completed = line
		}
	}
	if started == "" || completed == "" {
		t.Fatalf("%s dependency-edge stage events missing (started=%q completed=%q); log:\n%s", operation, started, completed, log)
	}
	return completed
}
