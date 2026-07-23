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

// TestEvidenceBoundariesForServiceStoryIsEmpty asserts get_service_story has
// no declared Postgres-only boundaries at all: both candidate domains it once
// claimed (ci_cd_run_correlation, container_image_identity) are served
// through sibling top-level response fields (ci_cd_evidence and
// code_to_runtime_trace's image_package segment, respectively), so there is
// nothing left to disclose. See the audit note atop evidenceBoundariesFor.
func TestEvidenceBoundariesForServiceStoryIsEmpty(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("get_service_story")
	if got != nil {
		t.Fatalf("evidence_boundaries = %#v, want nil for get_service_story", got)
	}
}

// TestEvidenceBoundariesForWorkloadStory asserts get_workload_story's
// container_image_identity boundary is gone (#5457 projects BUILT_FROM), the
// former blanket package_correlation entry has narrowed to
// package_correlation_consumption (the only package sub-domain that still
// stays Postgres-only), and ci_cd_run_correlation (unimplemented here, #5428)
// remains disclosed.
func TestEvidenceBoundariesForWorkloadStory(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("get_workload_story")
	if len(got) != 2 {
		t.Fatalf("len(evidence_boundaries) = %d, want 2: %#v", len(got), got)
	}

	wantDomains := []string{"ci_cd_run_correlation", "package_correlation_consumption"}
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

// TestEvidenceBoundariesForRepoStoryIsEmpty asserts get_repo_story has no
// declared Postgres-only boundaries: all three domains it once claimed
// (container_image_identity, package_correlation_ownership,
// package_correlation_publication) now project canonical graph edges
// (BUILT_FROM, PUBLISHES) per issue #5457, so there is nothing left to
// disclose for this surface.
func TestEvidenceBoundariesForRepoStoryIsEmpty(t *testing.T) {
	t.Parallel()

	got := evidenceBoundariesFor("get_repo_story")
	if got != nil {
		t.Fatalf("evidence_boundaries = %#v, want nil for get_repo_story", got)
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

// TestBuildServiceStoryResponseOmitsEvidenceBoundariesField asserts the
// get_service_story response has no evidence_boundaries key at all (not an
// empty array — the field is omitted, matching attachEvidenceBoundaries'
// nil/omitted contract) once every candidate domain is genuinely absent.
func TestBuildServiceStoryResponseOmitsEvidenceBoundariesField(t *testing.T) {
	t.Parallel()

	workloadContext := sampleServiceDossierContext()
	got := buildServiceStoryResponse("workload:sample-service-api", workloadContext)

	if _, exists := got["evidence_boundaries"]; exists {
		t.Fatalf("evidence_boundaries present for get_service_story; want field omitted: %#v", got["evidence_boundaries"])
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
	if len(boundaries) != 2 {
		t.Fatalf("len(evidence_boundaries) = %d, want 2: %#v", len(boundaries), response["evidence_boundaries"])
	}
}

func TestBuildRepositoryStoryResponseOmitsEvidenceBoundaries(t *testing.T) {
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

	// evidence_boundaries are attached by the handler, not the builder. All
	// three domains get_repo_story once claimed now project canonical graph
	// edges (#5457), so the field is omitted entirely.
	attachEvidenceBoundaries(got, "get_repo_story")

	if _, ok := got["evidence_boundaries"]; ok {
		t.Fatalf("evidence_boundaries = %#v, want omitted for get_repo_story", got["evidence_boundaries"])
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
	attachEvidenceBoundaries(response, "trace_deployment_chain")

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
			t.Errorf("unexpected domain %q in trace_deployment_chain boundaries", b.Domain)
		}
		if b.ReadSurface != "trace_deployment_chain" {
			t.Errorf("boundary %q read_surface = %q, want trace_deployment_chain", b.Domain, b.ReadSurface)
		}
	}
}

// TestBuildServiceStoryResponseOmitsBoundaryForFieldAlreadyServed is the
// regression test for the arbitrated #5472 review finding: get_service_story
// wrongly disclosed a ci_cd_run_correlation boundary even though the response
// already serves that domain through the top-level ci_cd_evidence field. The
// pre-fix declaration in evidenceBoundariesFor contradicted the response it
// described. sampleServiceDossierContext() never set ci_cd_evidence, which is
// why TestBuildServiceStoryResponseIncludesEvidenceBoundaries above did not
// catch the contradiction; this test populates it explicitly.
func TestBuildServiceStoryResponseOmitsBoundaryForFieldAlreadyServed(t *testing.T) {
	t.Parallel()

	workloadContext := sampleServiceDossierContext()
	workloadContext["ci_cd_evidence"] = map[string]any{
		"static_workflow_artifacts": map[string]any{"state": "materialized"},
	}

	got := buildServiceStoryResponse("workload:sample-service-api", workloadContext)

	if ciCDEvidence, ok := got["ci_cd_evidence"]; !ok || ciCDEvidence == nil {
		t.Fatalf("response missing ci_cd_evidence sibling field; test setup invalid: %#v", got["ci_cd_evidence"])
	}

	boundaries := evidenceBoundariesFromMap(got)
	for _, b := range boundaries {
		if b.Domain == "ci_cd_run_correlation" {
			t.Fatalf(
				"evidence_boundaries contains ci_cd_run_correlation while response[\"ci_cd_evidence\"] is populated; "+
					"a sibling field already serves this domain, so the boundary disclosure is false: %#v",
				boundaries,
			)
		}
	}
}

// TestBuildServiceStoryResponseOmitsContainerImageIdentityBoundaryForFieldAlreadyServed
// is the round-2 regression test for the same defect class the eshu-code-review
// gate caught after TestBuildServiceStoryResponseOmitsBoundaryForFieldAlreadyServed
// landed: get_service_story's remaining container_image_identity boundary was
// ALSO false. response["code_to_runtime_trace"] (service_story_overview.go:22,
// buildServiceCodeToRuntimeTrace) includes an image_package segment
// (service_story_trace_path.go:94-121, serviceTraceImagePackageSegment) that
// embeds container-image-identity evidence — repository_id, identity_id,
// identity_outcome, identity_strength, identity_evidence_fact_ids — read back
// from workloadContext["supply_chain_evidence"]["image_package"]
// (service_story_supply_chain.go:314-347). sampleServiceDossierContext() never
// sets supply_chain_evidence, the same vacuous-coverage shape that let the
// original ci_cd_run_correlation contradiction through; this test populates it
// explicitly.
func TestBuildServiceStoryResponseOmitsContainerImageIdentityBoundaryForFieldAlreadyServed(t *testing.T) {
	t.Parallel()

	workloadContext := sampleServiceDossierContext()
	workloadContext["supply_chain_evidence"] = map[string]any{
		"image_package": map[string]any{
			"evidence": []map[string]any{
				{
					"source":                     "supply_chain_read_model",
					"image_ref":                  "registry.example.com/sample-service-api:v1",
					"digest":                     "sha256:deadbeefcafefeed",
					"repository_id":              "repo-sample-service-api",
					"identity_id":                "identity-sample-service-api",
					"identity_outcome":           "exact_digest",
					"identity_strength":          "exact",
					"identity_evidence_fact_ids": []string{"fact-1"},
				},
			},
			"missing_evidence": []string{},
		},
	}

	got := buildServiceStoryResponse("workload:sample-service-api", workloadContext)

	trace := mapValue(got, "code_to_runtime_trace")
	var imagePackageSegment map[string]any
	for _, segment := range mapSliceValue(trace, "segments") {
		if StringVal(segment, "name") == "image_package" {
			imagePackageSegment = segment
			break
		}
	}
	if imagePackageSegment == nil || IntVal(imagePackageSegment, "evidence_count") == 0 {
		t.Fatalf("code_to_runtime_trace missing a populated image_package segment; test setup invalid: %#v", trace)
	}

	boundaries := evidenceBoundariesFromMap(got)
	for _, b := range boundaries {
		if b.Domain == "container_image_identity" {
			t.Fatalf(
				"evidence_boundaries contains container_image_identity while "+
					"code_to_runtime_trace's image_package segment already serves it; "+
					"the boundary disclosure is false: %#v",
				boundaries,
			)
		}
	}
}

// TestBuildServiceStoryResponseOmitsEvidenceBoundariesWhenFullyServed proves
// the two prior regressions together: when both ci_cd_evidence and
// supply_chain_evidence are populated, get_service_story's correct boundary
// set is empty and the field is omitted entirely, not emitted as `[]`.
func TestBuildServiceStoryResponseOmitsEvidenceBoundariesWhenFullyServed(t *testing.T) {
	t.Parallel()

	workloadContext := sampleServiceDossierContext()
	workloadContext["ci_cd_evidence"] = map[string]any{
		"static_workflow_artifacts": map[string]any{"state": "materialized"},
	}
	workloadContext["supply_chain_evidence"] = map[string]any{
		"image_package": map[string]any{
			"evidence": []map[string]any{
				{
					"source":           "supply_chain_read_model",
					"image_ref":        "registry.example.com/sample-service-api:v1",
					"digest":           "sha256:deadbeefcafefeed",
					"repository_id":    "repo-sample-service-api",
					"identity_id":      "identity-sample-service-api",
					"identity_outcome": "exact_digest",
				},
			},
		},
	}

	got := buildServiceStoryResponse("workload:sample-service-api", workloadContext)

	if _, exists := got["evidence_boundaries"]; exists {
		t.Fatalf("evidence_boundaries present when all candidate domains are served; want field omitted: %#v", got["evidence_boundaries"])
	}
}
