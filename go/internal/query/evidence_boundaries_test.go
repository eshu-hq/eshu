// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"testing"
)

// evidenceBoundariesFromMap extracts []PostgresOnlyBoundary from a response
// map. Returns nil when the key is absent or the value has the wrong type.
func evidenceBoundariesFromMap(data map[string]any) []PostgresOnlyBoundary {
	if data == nil {
		return nil
	}
	boundaries, _ := data["evidence_boundaries"].([]PostgresOnlyBoundary)
	return boundaries
}

func TestEvidenceBoundariesForServiceStory(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("get_service_story")
	if len(got) != 2 {
		t.Fatalf("len(evidence_boundaries) = %d, want 2: %#v", len(got), got)
	}

	wantDomains := []string{"ci_cd_run_correlation", "container_image_identity"}
	for i, b := range got {
		if b.Domain != wantDomains[i] {
			t.Errorf("boundary[%d].domain = %q, want %q", i, b.Domain, wantDomains[i])
		}
		if b.ReadSurface != "get_service_story" {
			t.Errorf("boundary[%d].read_surface = %q, want get_service_story", i, b.ReadSurface)
		}
		if b.Reason != boundaryReasonPostgresOnly {
			t.Errorf("boundary[%d].reason = %q, want %q", i, b.Reason, boundaryReasonPostgresOnly)
		}
	}
}

func TestEvidenceBoundariesForWorkloadStory(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("get_workload_story")
	if len(got) != 3 {
		t.Fatalf("len(evidence_boundaries) = %d, want 3: %#v", len(got), got)
	}

	wantDomains := []string{"ci_cd_run_correlation", "container_image_identity", "package_correlation"}
	for i, b := range got {
		if b.Domain != wantDomains[i] {
			t.Errorf("boundary[%d].domain = %q, want %q", i, b.Domain, wantDomains[i])
		}
		if b.ReadSurface != "get_workload_story" {
			t.Errorf("boundary[%d].read_surface = %q, want get_workload_story", i, b.ReadSurface)
		}
		if b.Reason != boundaryReasonPostgresOnly {
			t.Errorf("boundary[%d].reason = %q, want %q", i, b.Reason, boundaryReasonPostgresOnly)
		}
	}
}

func TestEvidenceBoundariesForRepoStory(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("get_repo_story")
	if len(got) != 3 {
		t.Fatalf("len(evidence_boundaries) = %d, want 3: %#v", len(got), got)
	}

	wantDomains := []string{"container_image_identity", "package_correlation_ownership", "package_correlation_publication"}
	for i, b := range got {
		if b.Domain != wantDomains[i] {
			t.Errorf("boundary[%d].domain = %q, want %q", i, b.Domain, wantDomains[i])
		}
		if b.ReadSurface != "get_repo_story" {
			t.Errorf("boundary[%d].read_surface = %q, want get_repo_story", i, b.ReadSurface)
		}
		if b.Reason != boundaryReasonPostgresOnly {
			t.Errorf("boundary[%d].reason = %q, want %q", i, b.Reason, boundaryReasonPostgresOnly)
		}
	}
}

func TestEvidenceBoundariesForTraceDeployment(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("trace_deployment_chain")
	if len(got) != 2 {
		t.Fatalf("len(evidence_boundaries) = %d, want 2: %#v", len(got), got)
	}

	wantDomains := []string{"ci_cd_run_correlation", "container_image_identity"}
	for i, b := range got {
		if b.Domain != wantDomains[i] {
			t.Errorf("boundary[%d].domain = %q, want %q", i, b.Domain, wantDomains[i])
		}
		if b.ReadSurface != "trace_deployment_chain" {
			t.Errorf("boundary[%d].read_surface = %q, want trace_deployment_chain", i, b.ReadSurface)
		}
		if b.Reason != boundaryReasonPostgresOnly {
			t.Errorf("boundary[%d].reason = %q, want %q", i, b.Reason, boundaryReasonPostgresOnly)
		}
	}
}

func TestEvidenceBoundariesNilForUnknownSurface(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("unknown_surface")
	if got != nil {
		t.Fatalf("evidence_boundaries = %#v, want nil for unknown surface", got)
	}
}

func TestEvidenceBoundariesDeterministicOrder(t *testing.T) {
	t.Parallel()

	// Run twice and confirm identical output; stable sort is required for
	// golden-assertion compatibility.
	first := evidenceBoundariesFor("get_workload_story")
	second := evidenceBoundariesFor("get_workload_story")
	if len(first) != len(second) {
		t.Fatalf("length mismatch: %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Domain != second[i].Domain {
			t.Fatalf("boundary[%d].domain = %q vs %q, order not deterministic", i, first[i].Domain, second[i].Domain)
		}
	}
}

func TestBuildServiceStoryResponseIncludesEvidenceBoundaries(t *testing.T) {
	t.Parallel()

	workloadContext := sampleServiceDossierContext()
	got := buildServiceStoryResponse("workload:sample-service-api", workloadContext)

	boundaries := evidenceBoundariesFromMap(got)
	if len(boundaries) != 2 {
		t.Fatalf("len(evidence_boundaries) = %d, want 2: %#v", len(boundaries), got["evidence_boundaries"])
	}

	wantDomains := []string{"ci_cd_run_correlation", "container_image_identity"}
	for i, b := range boundaries {
		if b.Domain != wantDomains[i] {
			t.Errorf("boundary[%d].domain = %q, want %q", i, b.Domain, wantDomains[i])
		}
		if b.ReadSurface != "get_service_story" {
			t.Errorf("boundary[%d].read_surface = %q, want get_service_story", i, b.ReadSurface)
		}
		if b.Reason != boundaryReasonPostgresOnly {
			t.Errorf("boundary[%d].reason = %q, want %q", i, b.Reason, boundaryReasonPostgresOnly)
		}
	}
}

func TestBuildWorkloadStoryResponseIncludesEvidenceBoundaries(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()

	// The handler builds the response map directly.
	response := map[string]any{
		"workload_id":     "workload:sample-service-api",
		"name":            "sample-service-api",
		"story":           buildWorkloadStory(ctx),
		"result_limits":   workloadContextResultLimits(ctx, "workload:sample-service-api", "story"),
		"partial_reasons": contextPartialReasons(ctx),
	}
	attachEvidenceBoundaries(response, "get_workload_story")

	boundaries := evidenceBoundariesFromMap(response)
	if len(boundaries) != 3 {
		t.Fatalf("len(evidence_boundaries) = %d, want 3: %#v", len(boundaries), response["evidence_boundaries"])
	}
}

func TestBuildRepositoryStoryResponseIncludesEvidenceBoundaries(t *testing.T) {
	t.Parallel()

	repo := RepoRef{ID: "repository:test-repo", Name: "test-repo"}
	got := buildRepositoryStoryResponse(
		repo,
		42,
		[]string{"go"},
		[]string{"test-api"},
		[]string{"kubernetes"},
		2,
		nil,
		nil,
	)

	// evidence_boundaries are attached by the handler, not the builder.
	attachEvidenceBoundaries(got, "get_repo_story")

	boundaries := evidenceBoundariesFromMap(got)
	if len(boundaries) != 3 {
		t.Fatalf("len(evidence_boundaries) = %d, want 3: %#v", len(boundaries), got["evidence_boundaries"])
	}
}

func TestBuildDeploymentTraceResponseIncludesEvidenceBoundaries(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	got := buildDeploymentTraceResponse("sample-service-api", ctx)

	// The handler attaches after buildDeploymentTraceResponse.
	attachEvidenceBoundaries(got, "trace_deployment_chain")

	boundaries := evidenceBoundariesFromMap(got)
	if len(boundaries) != 2 {
		t.Fatalf("len(evidence_boundaries) = %d, want 2: %#v", len(boundaries), got["evidence_boundaries"])
	}

	wantDomains := []string{"ci_cd_run_correlation", "container_image_identity"}
	for i, b := range boundaries {
		if b.Domain != wantDomains[i] {
			t.Errorf("boundary[%d].domain = %q, want %q", i, b.Domain, wantDomains[i])
		}
	}
}

func TestAttachEvidenceBoundariesNilWhenEmpty(t *testing.T) {
	t.Parallel()

	response := map[string]any{"key": "value"}
	attachEvidenceBoundaries(response, "unknown_surface")

	if _, exists := response["evidence_boundaries"]; exists {
		t.Fatalf("evidence_boundaries set for unknown surface; want absent: %#v", response["evidence_boundaries"])
	}
}

func TestAttachEvidenceBoundariesSliceUsesPostgresOnlyBoundary(t *testing.T) {
	t.Parallel()

	response := map[string]any{"key": "value"}
	attachEvidenceBoundaries(response, "get_service_story")

	boundaries, ok := response["evidence_boundaries"].([]PostgresOnlyBoundary)
	if !ok {
		t.Fatalf("evidence_boundaries type = %T, want []PostgresOnlyBoundary", response["evidence_boundaries"])
	}
	if len(boundaries) != 2 {
		t.Fatalf("len(evidence_boundaries) = %d, want 2", len(boundaries))
	}
	for _, b := range boundaries {
		if b.Reason != boundaryReasonPostgresOnly {
			t.Errorf("boundary %q reason = %q, want %q", b.Domain, b.Reason, boundaryReasonPostgresOnly)
		}
		if !slices.Contains([]string{"ci_cd_run_correlation", "container_image_identity"}, b.Domain) {
			t.Errorf("unexpected domain %q in get_service_story boundaries", b.Domain)
		}
		if b.ReadSurface != "get_service_story" {
			t.Errorf("boundary %q read_surface = %q, want get_service_story", b.Domain, b.ReadSurface)
		}
	}
}
