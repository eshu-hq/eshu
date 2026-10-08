// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type failedCoverageStore struct {
	orderedCoverageStore
}

func (s *failedCoverageStore) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	s.listCalls++
	return nil, errors.New("synthetic content read failure")
}

func TestWorkflowCoveragePreservesFailureStates(t *testing.T) {
	t.Parallel()
	store := &orderedCoverageStore{files: coverageFiles(5000, 0)}
	if got := StaticWorkflowArtifactEvidence(t.Context(), store, ""); got.State != "not_checked" || got.Reason != "repository_scope_required" || got.CandidatePoolStatus != "" {
		t.Fatalf("no-scope evidence = %#v", got)
	}
	if store.listCalls != 0 {
		t.Fatal("missing repository scope caused a file read")
	}
	if got := StaticWorkflowArtifactEvidence(t.Context(), nil, coverageRepoID); got.State != "unavailable" || got.Reason != "content_store_unavailable" || got.CandidatePoolStatus != "" {
		t.Fatalf("nil-store evidence = %#v", got)
	}
	failed := &failedCoverageStore{}
	if got := StaticWorkflowArtifactEvidence(t.Context(), failed, coverageRepoID); got.State != "unavailable" || got.Reason != "workflow_artifact_read_failed" || got.CandidatePoolStatus != "" {
		t.Fatalf("failed-read evidence = %#v", got)
	}
	if failed.listCalls != 1 {
		t.Fatalf("failed read calls = %d, want one", failed.listCalls)
	}
}

func TestWorkflowCoverageSurvivesEveryLiveState(t *testing.T) {
	t.Parallel()
	for _, positive := range []bool{false, true} {
		for _, live := range []string{"missing", "present", "unavailable"} {
			t.Run(live+"/positive="+map[bool]string{false: "false", true: "true"}[positive], func(t *testing.T) {
				t.Parallel()
				ordinal := 0
				if positive {
					ordinal = 5000
				}
				static := StaticWorkflowArtifactEvidenceFromFiles(t.Context(), &orderedCoverageStore{}, coverageRepoID, coverageFiles(5000, ordinal), true)
				var rows []querycontract.CICDRunCorrelationResult
				if live == "present" {
					rows = []querycontract.CICDRunCorrelationResult{{Outcome: "exact", ImageRef: "registry.example.test/app:fixture"}}
				}
				summary := BuildCICDRunCorrelationEvidenceSummary(static, rows, false, live == "unavailable")
				if summary.LiveRunCorrelations.State != live || summary.StaticWorkflowArtifacts.CandidatePoolStatus != "unknown_at_limit" {
					t.Fatalf("live/coverage = %#v", summary)
				}
				if !slices.Contains(summary.MissingEvidence, "static_workflow_coverage_unknown") || slices.Contains(summary.MissingEvidence, "ci_cd_evidence_missing") {
					t.Fatalf("missing evidence = %v, want uncertainty without asserted absence", summary.MissingEvidence)
				}
				if live == "missing" && !positive && summary.Reason != "static_workflow_coverage_unknown" {
					t.Fatalf("empty capped summary reason = %q", summary.Reason)
				}
				if live == "present" && summary.Reason != "" {
					t.Fatalf("live-present reason = %q, want existing empty reason", summary.Reason)
				}
				if live == "unavailable" && summary.Reason != "run_correlation_read_model_unavailable" {
					t.Fatalf("unavailable reason = %q, want existing failure reason", summary.Reason)
				}
				story := cicdRunCorrelationEvidenceSummaryMap(summary)
				missing, ok := story["missing_evidence"].([]string)
				if !ok || !slices.Equal(missing, []string{"static_workflow_coverage_unknown"}) {
					t.Fatalf("story missing evidence = %#v, want scoped coverage entry", story["missing_evidence"])
				}
			})
		}
	}
}

type hydrationCoverageStore struct {
	orderedCoverageStore
	hydrated atomic.Int32
}

func (s *hydrationCoverageStore) GetFileContent(context.Context, string, string) (*querycontract.FileContent, error) {
	s.hydrated.Add(1)
	return nil, nil
}

func TestWorkflowCoveragePreservesDisplayAndHydrationLimits(t *testing.T) {
	t.Parallel()
	store := &hydrationCoverageStore{}
	files := coverageFiles(5000, 0)
	for i := range 60 {
		files[i].ArtifactType = "github_actions_workflow"
	}
	got := StaticWorkflowArtifactEvidenceFromFiles(t.Context(), store, coverageRepoID, files, true)
	if got.State != "present" || got.Count != 60 || len(got.Paths) != 20 || !got.Truncated || got.CandidatePoolStatus != "unknown_at_limit" {
		t.Fatalf("bounded workflow evidence = %#v", got)
	}
	if store.listCalls != 0 || store.hydrated.Load() != 50 {
		t.Fatalf("content reads = list %d hydrate %d, want zero/50", store.listCalls, store.hydrated.Load())
	}
	if !slices.IsSorted(got.Paths) {
		t.Fatalf("paths are not sorted: %v", got.Paths)
	}
}

// TestWorkflowCoverageClassificationIsExactMatch pins that only the defined
// unknown_at_limit status earns the coverage-unknown class, so a status added
// later falls to review instead of silently inheriting it.
func TestWorkflowCoverageClassificationIsExactMatch(t *testing.T) {
	t.Parallel()
	static := CicdStaticWorkflowArtifactEvidence{
		State:               "present",
		Count:               1,
		Paths:               []string{".github/workflows/ci.yml"},
		CandidatePoolStatus: "future_status",
	}
	summary := BuildCICDRunCorrelationEvidenceSummary(static, nil, false, false)
	if slices.Contains(summary.MissingEvidence, "static_workflow_coverage_unknown") {
		t.Fatalf("missing evidence = %v, undefined status inherited the coverage class", summary.MissingEvidence)
	}
	if story := cicdRunCorrelationEvidenceSummaryMap(summary); story["missing_evidence"] != nil {
		t.Fatalf("story missing evidence = %#v, undefined status inherited the coverage class", story["missing_evidence"])
	}
	known := static
	known.CandidatePoolStatus = candidatePoolUnknownAtLimit
	summary = BuildCICDRunCorrelationEvidenceSummary(known, nil, false, false)
	if !slices.Contains(summary.MissingEvidence, "static_workflow_coverage_unknown") {
		t.Fatalf("missing evidence = %v, want the coverage class for %q", summary.MissingEvidence, known.CandidatePoolStatus)
	}
}
