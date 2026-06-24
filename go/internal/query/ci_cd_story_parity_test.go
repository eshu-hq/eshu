// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func TestBuildRepositoryStoryResponsePreservesCICDEvidenceSummary(t *testing.T) {
	t.Parallel()

	ciCDEvidence := testCICDEvidenceSummaryMap()
	got := buildRepositoryStoryResponse(
		RepoRef{ID: "repo://example/api", Name: "api"},
		12,
		[]string{"go", "yaml"},
		nil,
		[]string{"github_actions"},
		0,
		map[string]any{
			"families":       []string{"github_actions"},
			"ci_cd_evidence": ciCDEvidence,
		},
		nil,
	)

	summary := mustMapField(t, got, "ci_cd_evidence")
	bridge := mustMapField(t, summary, "run_artifact_evidence")
	if got, want := bridge["reason"], "artifact_digest_present"; got != want {
		t.Fatalf("ci_cd_evidence.run_artifact_evidence.reason = %#v, want %#v", got, want)
	}
	if !storySectionsContainTitle(got, "ci_cd") {
		t.Fatalf("story_sections = %#v, want ci_cd section", got["story_sections"])
	}
}

func TestBuildServiceStoryResponsePreservesCICDEvidenceSummaryInTrace(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["ci_cd_evidence"] = testCICDEvidenceSummaryMap()

	got := buildServiceStoryResponse("sample-service-api", ctx)
	summary := mustMapField(t, got, "ci_cd_evidence")
	bridge := mustMapField(t, summary, "run_artifact_evidence")
	if got, want := bridge["state"], "present"; got != want {
		t.Fatalf("ci_cd_evidence.run_artifact_evidence.state = %#v, want %#v", got, want)
	}

	trace := mustMapField(t, got, "code_to_runtime_trace")
	segment := segmentByName(mapSliceValue(trace, "segments"), "ci_cd")
	if segment == nil {
		t.Fatalf("code_to_runtime_trace missing ci_cd segment: %#v", trace)
	}
	if got, want := StringVal(segment, "basis"), "ci_cd_run_correlation_readback"; got != want {
		t.Fatalf("ci_cd segment basis = %q, want %q", got, want)
	}
	segmentSummary := mustMapField(t, segment, "evidence_summary")
	if got, want := mustMapField(t, segmentSummary, "run_artifact_evidence")["reason"], "artifact_digest_present"; got != want {
		t.Fatalf("ci_cd segment run_artifact_evidence.reason = %#v, want %#v", got, want)
	}
}

func testCICDEvidenceSummaryMap() map[string]any {
	return map[string]any{
		"static_workflow_artifacts": map[string]any{
			"state": "present",
			"count": 1,
			"paths": []string{".github/workflows/deploy.yml"},
		},
		"live_run_correlations": map[string]any{
			"state": "present",
			"count": 1,
		},
		"run_artifact_evidence": map[string]any{
			"state":                 "present",
			"count":                 1,
			"artifact_digest_count": 1,
			"image_ref_count":       0,
			"ambiguous_count":       0,
			"reason":                "artifact_digest_present",
		},
	}
}

func storySectionsContainTitle(response map[string]any, title string) bool {
	for _, section := range mapSliceValue(response, "story_sections") {
		if StringVal(section, "title") == title {
			return true
		}
	}
	return false
}
