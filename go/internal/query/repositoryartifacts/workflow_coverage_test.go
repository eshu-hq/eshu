// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	contenttest "github.com/eshu-hq/eshu/go/internal/query/testutil/content"
)

const coverageRepoID = "repo-coverage"

type orderedCoverageStore struct {
	contenttest.FakePortContentStore
	files        []querycontract.FileContent
	listCalls    int
	listRepo     string
	listLimit    int
	hydrateCalls int
}

func (s *orderedCoverageStore) ListRepoFiles(_ context.Context, repoID string, limit int) ([]querycontract.FileContent, error) {
	s.listCalls++
	s.listRepo, s.listLimit = repoID, limit
	files := append([]querycontract.FileContent(nil), s.files...)
	slices.SortFunc(files, func(a, b querycontract.FileContent) int {
		if a.RelativePath < b.RelativePath {
			return -1
		}
		if a.RelativePath > b.RelativePath {
			return 1
		}
		return 0
	})
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}

func (s *orderedCoverageStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	s.hydrateCalls++
	return nil, nil
}

type emptyCoverageCorrelations struct {
	calls  int
	filter querycontract.CICDRunCorrelationFilter
}

func (s *emptyCoverageCorrelations) ListCICDRunCorrelations(
	_ context.Context, filter querycontract.CICDRunCorrelationFilter,
) ([]querycontract.CICDRunCorrelationRow, error) {
	s.calls++
	s.filter = filter
	return nil, nil
}

func coverageFiles(total, workflowOrdinal int) []querycontract.FileContent {
	files := make([]querycontract.FileContent, total)
	for i := range files {
		files[i] = querycontract.FileContent{
			RepoID:       coverageRepoID,
			RelativePath: fmt.Sprintf("src/file-%05d.go", i+1),
		}
	}
	if workflowOrdinal > 0 {
		files[workflowOrdinal-1] = querycontract.FileContent{
			RepoID:       coverageRepoID,
			RelativePath: fmt.Sprintf("src/file-%05d.go", workflowOrdinal),
			ArtifactType: "GitHub_Actions_Workflow",
			Content:      "name: CI\n",
		}
	}
	return files
}

func workflowCoverageJSON(t *testing.T, value CicdStaticWorkflowArtifactEvidence) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestStaticWorkflowCoverageAtFilePageBoundary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		total           int
		workflowOrdinal int
		wantState       string
		wantStatus      string
		wantCount       int
		wantPath        string
		wantReason      string
	}{
		{name: "below_limit_empty", total: 4999, wantState: "absent"},
		{name: "exactly_limit_empty", total: 5000, wantState: "absent"},
		{name: "exactly_limit_present", total: 5000, workflowOrdinal: 5000, wantState: "present", wantCount: 1, wantPath: "src/file-05000.go"},
		{name: "past_limit_empty", total: 5001, workflowOrdinal: 5001, wantState: "unknown", wantStatus: "unknown_at_limit", wantReason: "repository_file_scan_limit_reached"},
		{name: "past_limit_present", total: 5001, workflowOrdinal: 5000, wantState: "present", wantStatus: "unknown_at_limit", wantCount: 1, wantPath: "src/file-05000.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &orderedCoverageStore{files: coverageFiles(tc.total, tc.workflowOrdinal)}
			got := StaticWorkflowArtifactEvidence(t.Context(), store, coverageRepoID)
			if store.listCalls != 1 || store.listRepo != coverageRepoID || store.listLimit != querycontract.RepositorySemanticEntityLimit+1 {
				t.Fatalf("ListRepoFiles calls/repo/limit = %d/%q/%d, want 1/%q/%d", store.listCalls, store.listRepo, store.listLimit, coverageRepoID, querycontract.RepositorySemanticEntityLimit+1)
			}
			if store.hydrateCalls != 0 {
				t.Fatalf("GetFileContent calls = %d, want 0", store.hydrateCalls)
			}
			if got.State != tc.wantState || got.Count != tc.wantCount || got.Reason != tc.wantReason {
				t.Errorf("state/count/reason = %q/%d/%q, want %q/%d/%q", got.State, got.Count, got.Reason, tc.wantState, tc.wantCount, tc.wantReason)
			}
			if status := workflowCoverageJSON(t, got)["candidate_pool_status"]; status != tc.wantStatus && (status != nil || tc.wantStatus != "") {
				t.Errorf("candidate_pool_status = %#v, want %q", status, tc.wantStatus)
			}
			if tc.wantPath != "" && !reflect.DeepEqual(got.Paths, []string{tc.wantPath}) {
				t.Errorf("paths = %v, want [%q]", got.Paths, tc.wantPath)
			}
		})
	}
}

func TestCappedEmptyWorkflowSummaryDoesNotClaimAbsence(t *testing.T) {
	t.Parallel()
	store := &orderedCoverageStore{files: coverageFiles(5001, 5001)}
	correlations := &emptyCoverageCorrelations{}
	got, err := LoadRepositoryScopedCICDEvidence(t.Context(), store, correlations, coverageRepoID)
	if err != nil {
		t.Fatal(err)
	}
	static, ok := got["static_workflow_artifacts"].(map[string]any)
	if !ok {
		t.Fatalf("static_workflow_artifacts = %T, want map", got["static_workflow_artifacts"])
	}
	if static["state"] != "unknown" || static["candidate_pool_status"] != "unknown_at_limit" {
		t.Errorf("static workflow coverage = %#v, want unknown at limit", static)
	}
	if got["reason"] == "no_ci_cd_evidence_found" {
		t.Errorf("summary reason = %q, claims absence beyond capped page", got["reason"])
	}
	if store.listCalls != 1 || store.listLimit != querycontract.RepositorySemanticEntityLimit+1 || store.hydrateCalls != 0 {
		t.Errorf("content reads = list %d limit %d hydrate %d, want 1/%d/0", store.listCalls, store.listLimit, store.hydrateCalls, querycontract.RepositorySemanticEntityLimit+1)
	}
	if correlations.calls != 1 || correlations.filter.RepositoryID != coverageRepoID {
		t.Errorf("correlation reads = %d with filter %#v, want one scoped read", correlations.calls, correlations.filter)
	}
}

// TestStaticWorkflowCoverageFromFilesKeysOnTruncationFlag pins #7619: the
// candidate pool is unknown exactly when the caller reports its file read
// returned the sentinel row, never because the clipped list is limit-sized.
func TestStaticWorkflowCoverageFromFilesKeysOnTruncationFlag(t *testing.T) {
	t.Parallel()
	store := &orderedCoverageStore{}
	files := coverageFiles(querycontract.RepositorySemanticEntityLimit, 0)

	complete := StaticWorkflowArtifactEvidenceFromFiles(t.Context(), store, coverageRepoID, files, false)
	if complete.State != "absent" || complete.Reason != "" || workflowCoverageJSON(t, complete)["candidate_pool_status"] != nil {
		t.Errorf("limit-sized complete list = %#v, want absent without marker", complete)
	}
	truncated := StaticWorkflowArtifactEvidenceFromFiles(t.Context(), store, coverageRepoID, files, true)
	if truncated.State != "unknown" || workflowCoverageJSON(t, truncated)["candidate_pool_status"] != "unknown_at_limit" {
		t.Errorf("truncated list = %#v, want unknown at limit", truncated)
	}
	if store.listCalls != 0 || store.hydrateCalls != 0 {
		t.Errorf("content reads = list %d hydrate %d, want zero", store.listCalls, store.hydrateCalls)
	}
}

func TestStaticWorkflowCoverageFileMembership(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		file querycontract.FileContent
		want bool
	}{
		{name: "mixed_case_artifact_type_override", file: querycontract.FileContent{RelativePath: "src/build.txt", ArtifactType: "GitHub_Actions_Workflow"}, want: true},
		{name: "trimmed_case_posix_path", file: querycontract.FileContent{RelativePath: "  .GITHUB/WORKFLOWS/CI.YAML  "}, want: true},
		{name: "nested_posix_path", file: querycontract.FileContent{RelativePath: "vendor/.github/workflows/ci.yml"}, want: true},
		{name: "non_workflow_yaml", file: querycontract.FileContent{RelativePath: ".github/ci.yaml"}},
		{name: "wrong_extension", file: querycontract.FileContent{RelativePath: ".github/workflows/ci.txt"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsGitHubActionsWorkflowFile(tc.file); got != tc.want {
				t.Errorf("IsGitHubActionsWorkflowFile(%#v) = %t, want %t", tc.file, got, tc.want)
			}
		})
	}
}

func BenchmarkStaticWorkflowCoverage(b *testing.B) {
	cases := []struct {
		name            string
		total           int
		workflowOrdinal int
	}{
		{name: "uncapped_empty", total: 4999},
		{name: "exactly_limit_empty", total: 5000},
		{name: "exactly_limit_present", total: 5000, workflowOrdinal: 5000},
	}
	for _, tc := range cases {
		files := coverageFiles(tc.total, tc.workflowOrdinal)
		store := &orderedCoverageStore{}
		b.Run(tc.name, func(b *testing.B) {
			for range b.N {
				StaticWorkflowArtifactEvidenceFromFiles(context.Background(), store, coverageRepoID, files, false)
			}
		})
	}
}
