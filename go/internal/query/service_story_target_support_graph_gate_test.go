// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

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
)

// getServiceStoryWithDefines drives the service-story handler with a graph
// double that answers the #7138 DEFINES read as scripted and returns the
// filter the target-support loader handed the content store.
func getServiceStoryWithDefines(
	t *testing.T,
	defines *serviceStoryDefinesRead,
	model serviceStoryTargetSupportReadModel,
	logs *bytes.Buffer,
) (serviceStoryTargetSupportFilter, map[string]any) {
	t.Helper()

	var captured serviceStoryTargetSupportFilter
	handler := &EntityHandler{
		Neo4j: serviceStoryExternalDocsGraphReader{t: t, repoID: "repo-payments-api", definesRead: defines},
		Content: fakePortContentStore{
			targetSupportModel:  model,
			targetSupportFilter: &captured,
		},
		Profile: querycontract.ProfileProduction,
	}
	if logs != nil {
		handler.Logger = slog.New(slog.NewJSONHandler(logs, nil))
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/services/payments-api/story?service_id=workload%3Apayments-api",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("service_name", "payments-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	return captured, serviceStoryEnvelopeData(t, w.Body.Bytes())
}

// TestServiceStoryTargetSupportGateReadsRepositoryDefines proves the loader
// asks the canonical graph which Workloads the repository DEFINES, once per
// story with a bounded read anchored on the repository id, and hands the
// outcome to the content store as two explicit filter fields (#7138).
func TestServiceStoryTargetSupportGateReadsRepositoryDefines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ids         []string
		err         error
		wantCount   int
		wantDefines bool
	}{
		{name: "sole workload is the target", ids: []string{"workload:payments-api"}, wantCount: 1, wantDefines: true},
		{name: "several workloads including the target", ids: []string{"workload:payments-api", "workload:payments-worker"}, wantCount: 2, wantDefines: true},
		{name: "sole workload is another", ids: []string{"workload:payments-worker"}, wantCount: 1, wantDefines: false},
		{name: "several workloads, target missing", ids: []string{"workload:a", "workload:b", "workload:c"}, wantCount: 3, wantDefines: false},
		{name: "repository defines none", ids: nil, wantCount: 0, wantDefines: false},
		{name: "graph read fails", err: errors.New("bolt: connection reset"), wantCount: 0, wantDefines: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defines := &serviceStoryDefinesRead{ids: tt.ids, err: tt.err}
			filter, _ := getServiceStoryWithDefines(t, defines, serviceStoryTargetSupportReadModel{}, nil)

			if defines.calls != 1 {
				t.Fatalf("DEFINES reads = %d, want exactly 1 per story", defines.calls)
			}
			if got := defines.params["repo_id"]; got != "repo-payments-api" {
				t.Fatalf("DEFINES read repo_id = %#v, want repo-payments-api", got)
			}
			if got := defines.params["workload_id"]; got != "workload:payments-api" {
				t.Fatalf("DEFINES read workload_id = %#v, want the target so it sorts first inside the limit", got)
			}
			for _, want := range []string{
				"MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)",
				"ORDER BY CASE WHEN id = $workload_id THEN 0 ELSE 1 END, id",
				"LIMIT 3",
			} {
				if !strings.Contains(defines.cypher, want) {
					t.Fatalf("DEFINES read missing %q:\n%s", want, defines.cypher)
				}
			}
			if strings.Contains(strings.ToLower(defines.cypher), "count(") {
				t.Fatalf("DEFINES read must not aggregate:\n%s", defines.cypher)
			}
			if filter.RepositoryWorkloadCount != tt.wantCount || filter.RepositoryDefinesTarget != tt.wantDefines {
				t.Fatalf("filter (count, defines) = (%d, %v), want (%d, %v)",
					filter.RepositoryWorkloadCount, filter.RepositoryDefinesTarget, tt.wantCount, tt.wantDefines)
			}
			if filter.Repository != "repo-payments-api" || filter.TargetKind != "service" || filter.TargetID != "workload:payments-api" {
				t.Fatalf("filter target = %#v, want the service target on its repository", filter)
			}
		})
	}
}

// TestServiceStoryTargetSupportGateSkipsGraphWithoutAWorkloadNode covers the
// paths that must fail closed without asking the graph: no graph, and an
// identity-only context, where the story is built from the repository read
// model and the graph has no Workload to define.
func TestServiceStoryTargetSupportGateSkipsGraphWithoutAWorkloadNode(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		graph querycontract.GraphQuery
		wc    map[string]any
	}{
		"graph unavailable": {
			graph: nil,
			wc:    map[string]any{"id": "workload:payments-api", "repo_id": "repo-payments-api"},
		},
		"identity only": {
			graph: &definesCountingGraph{},
			wc: map[string]any{
				"id": "workload:payments-api", "repo_id": "repo-payments-api",
				"materialization_status": "identity_only", "query_basis": "repository_read_model",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var captured serviceStoryTargetSupportFilter
			store := fakePortContentStore{
				targetSupportModel:  serviceStoryTargetSupportReadModel{Support: map[string]any{"evidence_count": 0}},
				targetSupportFilter: &captured,
			}
			if _, err := loadServiceStoryTargetSupport(context.Background(), tc.graph, store, tc.wc); err != nil {
				t.Fatalf("loadServiceStoryTargetSupport() error = %v, want nil (fail closed, not an error)", err)
			}
			if captured.TargetKind != "service" {
				t.Fatalf("the content store was not asked; filter = %#v", captured)
			}
			if captured.RepositoryWorkloadCount != 0 || captured.RepositoryDefinesTarget {
				t.Fatalf("filter (count, defines) = (%d, %v), want (0, false)", captured.RepositoryWorkloadCount, captured.RepositoryDefinesTarget)
			}
			if g, ok := tc.graph.(*definesCountingGraph); ok && g.calls != 0 {
				t.Fatalf("graph calls = %d, want 0 for an identity-only context", g.calls)
			}
		})
	}
}

// definesCountingGraph records every read so a test can prove the graph was skipped.
type definesCountingGraph struct{ calls int }

func (g *definesCountingGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	g.calls++
	return nil, nil
}

func (g *definesCountingGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	g.calls++
	return nil, nil
}

// TestServiceStoryTargetSupportStageEventExplainsAnEmptyOrAmbiguousBlock proves
// an operator can read why a story shows no support from the
// support_target_evidence stage event alone (#7138).
func TestServiceStoryTargetSupportStageEventExplainsAnEmptyOrAmbiguousBlock(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	model := serviceStoryTargetSupportReadModel{Support: map[string]any{
		"evidence_count":  0,
		"ambiguous_count": 2,
		"missing_evidence": []map[string]any{{
			"reason": "support_correlation_ambiguous",
		}},
	}}
	defines := &serviceStoryDefinesRead{ids: []string{"workload:payments-api", "workload:payments-worker"}}
	getServiceStoryWithDefines(t, defines, model, &logs)

	completed := ""
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"stage":"support_target_evidence"`) && strings.Contains(line, "stage_completed") {
			completed = line
		}
	}
	if completed == "" {
		t.Fatalf("no support_target_evidence stage_completed event in logs:\n%s", logs.String())
	}
	for _, want := range []string{
		`"repository_workload_count":2`,
		`"repository_defines_target":true`,
		`"target_support_ambiguous_count":2`,
		`"target_support_missing_reason":"support_correlation_ambiguous"`,
	} {
		if !strings.Contains(completed, want) {
			t.Fatalf("stage event missing %s:\n%s", want, completed)
		}
	}
}

// TestServiceStoryTargetSupportStageEventReportsGraphReadFailure keeps a failed
// DEFINES read from failing the story while still making it visible.
func TestServiceStoryTargetSupportStageEventReportsGraphReadFailure(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	defines := &serviceStoryDefinesRead{err: errors.New("bolt: connection reset")}
	getServiceStoryWithDefines(t, defines, serviceStoryTargetSupportReadModel{}, &logs)

	if !strings.Contains(logs.String(), `"repository_defines_error":"bolt: connection reset"`) {
		t.Fatalf("stage event missing repository_defines_error:\n%s", logs.String())
	}
}

// TestRepositoryStoryTargetSupportStageEventExplainsAnEmptyBlock gives the
// repository story the same operator signal: the target_support stage event
// carries the ambiguous count and the first missing-evidence reason (#7138).
func TestRepositoryStoryTargetSupportStageEventExplainsAnEmptyBlock(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: repositoryStoryExternalDocsGraphReader{t: t, repoID: "repo-payments-api"},
		Content: fakePortContentStore{
			targetSupportModel: serviceStoryTargetSupportReadModel{Support: map[string]any{
				"evidence_count":   0,
				"ambiguous_count":  0,
				"missing_evidence": []map[string]any{{"reason": "support_source_only_not_target_linked"}},
			}},
		},
		Profile: querycontract.ProfileProduction,
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-payments-api/story", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.SetPathValue("repo_id", "repo-payments-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	completed := ""
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"stage":"target_support"`) && strings.Contains(line, "stage_completed") {
			completed = line
		}
	}
	if completed == "" {
		t.Fatalf("no target_support stage_completed event in logs:\n%s", logs.String())
	}
	for _, want := range []string{
		`"target_support_ambiguous_count":0`,
		`"target_support_missing_reason":"support_source_only_not_target_linked"`,
	} {
		if !strings.Contains(completed, want) {
			t.Fatalf("stage event missing %s:\n%s", want, completed)
		}
	}
}
