// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

func TestWorkflowCoverageGoldenStoryReplay(t *testing.T) {
	t.Parallel()
	encoded, err := os.ReadFile("../../../testdata/golden/capped-workflow-evidence.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name            string         `json:"name"`
			Total           int            `json:"total_files"`
			WorkflowOrdinal int            `json:"workflow_ordinal"`
			Static          map[string]any `json:"static_workflow_artifacts"`
			SummaryReason   string         `json:"summary_reason"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(encoded, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 3 {
		t.Fatalf("golden cases = %d, want 3", len(fixture.Cases))
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			files := make([]FileContent, tc.Total)
			for i := range files {
				files[i] = FileContent{RepoID: "repo-coverage", RelativePath: fmt.Sprintf("src/file-%05d.go", i+1)}
			}
			if tc.WorkflowOrdinal > 0 {
				files[tc.WorkflowOrdinal-1].ArtifactType = "GitHub_Actions_Workflow"
				files[tc.WorkflowOrdinal-1].Content = "name: CI\n"
			}
			store := fakePortContentStore{repoFiles: files}
			correlations := &recordingCICDRunCorrelationStore{}
			// Repository story already owns the bounded, unfiltered file page.
			repoEvidence, err := artifacts.LoadRepositoryScopedCICDEvidenceFromFiles(t.Context(), store, correlations, "repo-coverage", files[:min(len(files), querycontract.RepositorySemanticEntityLimit)])
			if err != nil {
				t.Fatal(err)
			}
			repoStory := repository.BuildRepositoryStoryResponse(RepoRef{ID: "repo-coverage", Name: "fixture"}, tc.Total, nil, nil, nil, 0, map[string]any{"ci_cd_evidence": repoEvidence}, nil)
			assertWorkflowStoryGolden(t, testutil.MustMapField(t, repoStory, "ci_cd_evidence"), tc.Static, tc.SummaryReason)
			// Service story uses the independent listing loader before assembly.
			serviceEvidence, err := artifacts.LoadRepositoryScopedCICDEvidence(t.Context(), store, correlations, "repo-coverage")
			if err != nil {
				t.Fatal(err)
			}
			ctx := testutil.SampleServiceDossierContext()
			ctx["ci_cd_evidence"] = serviceEvidence
			serviceStory := buildServiceStoryResponse("fixture-service", ctx)
			assertWorkflowStoryGolden(t, testutil.MustMapField(t, serviceStory, "ci_cd_evidence"), tc.Static, tc.SummaryReason)
			trace := testutil.MustMapField(t, serviceStory, "code_to_runtime_trace")
			segment := testutil.SegmentByName(mapSliceValue(trace, "segments"), "ci_cd")
			if segment == nil {
				t.Fatal("service trace dropped ci_cd segment")
			}
			assertWorkflowStoryGolden(t, testutil.MustMapField(t, segment, "evidence_summary"), tc.Static, tc.SummaryReason)
		})
	}
}

func assertWorkflowStoryGolden(t *testing.T, evidence map[string]any, expected map[string]any, reason string) {
	t.Helper()
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		t.Fatal(err)
	}
	got := testutil.MustMapField(t, normalized, "static_workflow_artifacts")
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("static evidence = %#v, want committed golden %#v", got, expected)
	}
	if normalized["reason"] != reason {
		t.Fatalf("story reason = %#v, want %q", normalized["reason"], reason)
	}
	if expected["candidate_pool_status"] == nil {
		if _, exists := normalized["missing_evidence"]; exists {
			t.Fatal("uncapped story gained missing_evidence wire field")
		}
	} else if !reflect.DeepEqual(normalized["missing_evidence"], []any{"static_workflow_coverage_unknown"}) {
		t.Fatalf("capped story missing evidence = %#v", normalized["missing_evidence"])
	}
}
