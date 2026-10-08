// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

const workflowCoverageMCPRepo = "repo://coverage/mcp"

type workflowCoverageMCPStore struct {
	content.FakePortContentStore
	listCalls    int
	listRepo     string
	listLimit    int
	hydrateCalls int
}

func (s *workflowCoverageMCPStore) ListRepoFiles(ctx context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.listCalls++
	s.listRepo, s.listLimit = repoID, limit
	return s.FakePortContentStore.ListRepoFiles(ctx, repoID, limit)
}

func (s *workflowCoverageMCPStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	s.hydrateCalls++
	return nil, nil
}

func workflowCoverageMCPFiles(total, workflowOrdinal int) []querycontract.FileContent {
	files := make([]querycontract.FileContent, total)
	for i := range files {
		files[i] = querycontract.FileContent{RepoID: workflowCoverageMCPRepo, RelativePath: fmt.Sprintf("src/file-%05d.go", i+1)}
	}
	if workflowOrdinal > 0 {
		files[workflowOrdinal-1].ArtifactType = "GitHub_Actions_Workflow"
		files[workflowOrdinal-1].Content = "name: CI\n"
	}
	return files
}

func workflowCoverageMCPCases(t *testing.T) map[string]struct {
	Static map[string]any `json:"static_workflow_artifacts"`
	Reason string         `json:"summary_reason"`
} {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "golden", "capped-workflow-evidence.json")
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

func TestMCPWorkflowCoverageMatchesExpectedWireTruth(t *testing.T) {
	t.Parallel()
	wantCases := workflowCoverageMCPCases(t)
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
			store := &workflowCoverageMCPStore{}
			store.RepoFiles = workflowCoverageMCPFiles(tc.total, tc.workflowOrdinal)
			handler := &query.CICDHandler{Content: store, Correlations: mcpCICDRunCorrelationStore{}}
			mux := http.NewServeMux()
			handler.Mount(mux)
			result, err := dispatchTool(
				context.Background(), mux, "list_ci_cd_run_correlations",
				map[string]any{"repository_id": workflowCoverageMCPRepo, "limit": float64(10)},
				"", slog.New(slog.NewTextHandler(io.Discard, nil)),
			)
			if err != nil {
				t.Fatal(err)
			}
			if result.Envelope == nil {
				t.Fatal("MCP result missing canonical envelope")
			}
			data, ok := result.Envelope.Data.(map[string]any)
			if !ok {
				t.Fatalf("MCP data = %T, want map", result.Envelope.Data)
			}
			summary, ok := data["evidence_summary"].(map[string]any)
			if !ok {
				t.Fatalf("MCP evidence_summary = %T, want map", data["evidence_summary"])
			}
			static, ok := summary["static_workflow_artifacts"].(map[string]any)
			if !ok {
				t.Fatalf("MCP static_workflow_artifacts = %T, want map", summary["static_workflow_artifacts"])
			}
			if !reflect.DeepEqual(static, want.Static) {
				t.Errorf("MCP static_workflow_artifacts = %#v, want committed golden %#v", static, want.Static)
			}
			if _, expected := want.Static["candidate_pool_status"]; !expected {
				if _, present := static["candidate_pool_status"]; present {
					t.Errorf("uncapped MCP response added candidate_pool_status: %#v", static)
				}
			}
			if summary["reason"] != want.Reason {
				t.Errorf("MCP evidence_summary.reason = %#v, want %q", summary["reason"], want.Reason)
			}
			if store.listCalls != 1 || store.listRepo != workflowCoverageMCPRepo || store.listLimit != querycontract.RepositorySemanticEntityLimit+1 || store.hydrateCalls != 0 {
				t.Errorf("MCP content reads = list %d repo %q limit %d hydrate %d, want 1/%q/%d/0", store.listCalls, store.listRepo, store.listLimit, store.hydrateCalls, workflowCoverageMCPRepo, querycontract.RepositorySemanticEntityLimit+1)
			}
		})
	}
}
