// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

const workflowCoverageHTTPRepo = "repo://coverage/http"

type workflowCoverageHTTPStore struct {
	content.FakePortContentStore
	listCalls    int
	listRepo     string
	listLimit    int
	hydrateCalls int
}

func (s *workflowCoverageHTTPStore) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.listCalls++
	s.listRepo, s.listLimit = repoID, limit
	return s.FakePortContentStore.ListRepoFiles(ctx, repoID, limit)
}

func (s *workflowCoverageHTTPStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	s.hydrateCalls++
	return nil, nil
}

func workflowCoverageHTTPFiles(total, workflowOrdinal int) []querycontract.FileContent {
	files := make([]querycontract.FileContent, total)
	for i := range files {
		files[i] = querycontract.FileContent{RepoID: workflowCoverageHTTPRepo, RelativePath: fmt.Sprintf("src/file-%05d.go", i+1)}
	}
	if workflowOrdinal > 0 {
		files[workflowOrdinal-1].ArtifactType = "GitHub_Actions_Workflow"
		files[workflowOrdinal-1].Content = "name: CI\n"
	}
	return files
}

func workflowCoverageHTTPCases(t *testing.T) map[string]struct {
	Static map[string]any `json:"static_workflow_artifacts"`
	Reason string         `json:"summary_reason"`
} {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "testdata", "golden", "capped-workflow-evidence.json")
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name   string         `json:"name"`
			Static map[string]any `json:"static_workflow_artifacts"`
			Reason string         `json:"summary_reason"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(encoded, &fixture); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]struct {
		Static map[string]any `json:"static_workflow_artifacts"`
		Reason string         `json:"summary_reason"`
	}, len(fixture.Cases))
	for _, item := range fixture.Cases {
		out[item.Name] = struct {
			Static map[string]any `json:"static_workflow_artifacts"`
			Reason string         `json:"summary_reason"`
		}{Static: item.Static, Reason: item.Reason}
	}
	return out
}

func TestCICDHTTPWorkflowCoverageAtFilePageBoundary(t *testing.T) {
	t.Parallel()
	wantCases := workflowCoverageHTTPCases(t)
	cases := []struct {
		name            string
		total           int
		workflowOrdinal int
	}{
		{name: "uncapped_empty", total: 4999},
		{name: "exactly_limit_empty", total: 5000},
		{name: "exactly_limit_present", total: 5000, workflowOrdinal: 5000},
		{name: "capped_empty", total: 5001, workflowOrdinal: 5001},
		{name: "capped_present", total: 5001, workflowOrdinal: 5000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want, ok := wantCases[tc.name]
			if !ok {
				t.Fatalf("golden case %q missing", tc.name)
			}
			store := &workflowCoverageHTTPStore{}
			store.RepoFiles = workflowCoverageHTTPFiles(tc.total, tc.workflowOrdinal)
			handler := &Handler{Content: store, Correlations: &recordingRunCorrelationStore{}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations?repository_id=repo://coverage/http&limit=10", nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, want 200; body = %s", w.Code, w.Body.String())
			}
			var body struct {
				EvidenceSummary struct {
					Static  map[string]any `json:"static_workflow_artifacts"`
					Reason  string         `json:"reason"`
					Missing []string       `json:"missing_evidence"`
				} `json:"evidence_summary"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(body.EvidenceSummary.Static, want.Static) {
				t.Errorf("static_workflow_artifacts = %#v, want committed golden %#v", body.EvidenceSummary.Static, want.Static)
			}
			if _, expected := want.Static["candidate_pool_status"]; !expected {
				if _, present := body.EvidenceSummary.Static["candidate_pool_status"]; present {
					t.Errorf("uncapped response added candidate_pool_status: %#v", body.EvidenceSummary.Static)
				}
			}
			if body.EvidenceSummary.Reason != want.Reason {
				t.Errorf("evidence_summary.reason = %q, want %q", body.EvidenceSummary.Reason, want.Reason)
			}
			if tc.name == "capped_empty" {
				found := false
				for _, item := range body.EvidenceSummary.Missing {
					found = found || item == "static_workflow_coverage_unknown"
				}
				if !found {
					t.Errorf("missing_evidence = %v, want static_workflow_coverage_unknown", body.EvidenceSummary.Missing)
				}
			}
			if store.listCalls != 1 || store.listRepo != workflowCoverageHTTPRepo || store.listLimit != querycontract.RepositorySemanticEntityLimit+1 || store.hydrateCalls != 0 {
				t.Errorf("content reads = list %d repo %q limit %d hydrate %d, want 1/%q/%d/0", store.listCalls, store.listRepo, store.listLimit, store.hydrateCalls, workflowCoverageHTTPRepo, querycontract.RepositorySemanticEntityLimit+1)
			}
		})
	}
}
