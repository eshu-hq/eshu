// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/searchembedruntime"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
)

// oneLeftoverGraph reads as a graph with one repository and one name-only
// Workload node that has a repository-scoped twin and no instance: the
// pre-#5385 leftover shape the readiness check counts.
type oneLeftoverGraph struct{}

func (oneLeftoverGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"count": 1}, nil
}

func (oneLeftoverGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	const name = "svc"
	const repoID = "repository:svc"
	row := map[string]any{"id": "workload:" + name, "name": name, "repo_id": repoID}
	switch {
	case strings.Contains(cypher, "w.id > $after"):
		if after, _ := params["after"].(string); row["id"].(string) > after {
			return []map[string]any{row}, nil
		}
	case strings.Contains(cypher, "$twin_ids"):
		return []map[string]any{{"id": legacyworkload.TwinID(repoID, name), "repo_id": repoID}}, nil
	}
	return nil, nil
}

// TestMCPHostedReadinessReportsLeftoversLikeTheAPI proves the standalone MCP
// server hands the graph backend to its StatusHandler, so get_hosted_readiness
// reports the workload_identity_leftovers check the way the API route does: a
// fail on Neo4j with a leftover, and unknown (never pass) off Neo4j (#8795).
// Dropping GraphBackend from the MCP wiring silently turns the Neo4j result
// into unknown.
func TestMCPHostedReadinessReportsLeftoversLikeTheAPI(t *testing.T) {
	t.Parallel()

	snapshot := statuspkg.RawSnapshot{
		AsOf:          time.Date(2026, 6, 9, 12, 15, 0, 0, time.UTC),
		ScopeActivity: statuspkg.ScopeActivitySnapshot{Active: 1},
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []collector.InstanceSummary{
				{InstanceID: "collector:git", CollectorKind: "git", Enabled: true, ClaimsEnabled: true},
			},
		},
	}
	cases := []struct {
		name    string
		backend query.GraphBackend
		want    string
	}{
		{name: "neo4j reports the leftover", backend: query.GraphBackendNeo4j, want: "fail"},
		{name: "other backend stays unknown", backend: query.GraphBackendNornicDB, want: "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			router := newMCPQueryRouterWithSemanticEmbedding(
				nil, oneLeftoverGraph{}, nil,
				testutil.FakeStatusReader{Snapshot: snapshot},
				query.ProfileLocalAuthoritative,
				tc.backend,
				slog.Default(),
				nil,
				searchembedruntime.Config{},
				"",
				component.Policy{},
				query.GovernanceStatusConfig{},
				nil,
				false,
			)
			mux := http.NewServeMux()
			router.Mount(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/status/hosted-readiness", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			var payload struct {
				Checks []struct {
					Name  string `json:"name"`
					State string `json:"state"`
				} `json:"checks"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("json.Unmarshal() error = %v; body=%s", err, rec.Body.String())
			}
			for _, c := range payload.Checks {
				if c.Name == "workload_identity_leftovers" {
					if c.State != tc.want {
						t.Fatalf("workload_identity_leftovers = %q, want %q (GraphBackend not passed to the MCP StatusHandler?)", c.State, tc.want)
					}
					return
				}
			}
			t.Fatalf("no workload_identity_leftovers check in %s", rec.Body.String())
		})
	}
}
