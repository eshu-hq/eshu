// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"slices"
	"testing"
)

// TestDeploymentArtifactBoundReasonsAreTheWireValues pins the three reason
// strings. They reach clients on limitations and partial_reasons and are
// listed in the WorkloadContext OpenAPI enum, so a rename is a wire change.
func TestDeploymentArtifactBoundReasonsAreTheWireValues(t *testing.T) {
	t.Parallel()

	want := []string{
		"deployment_artifacts_candidates_budget_reached",
		"deployment_artifacts_related_file_list_truncated_at_5000",
		"deployment_artifacts_anchor_file_list_truncated_at_5000",
	}
	got := DeploymentArtifactBoundReasons()
	if !slices.Equal(got, want) {
		t.Fatalf("DeploymentArtifactBoundReasons() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if DeploymentArtifactBoundReasons()[0] != want[0] {
		t.Fatal("DeploymentArtifactBoundReasons() returned a shared slice; callers can corrupt the list")
	}
}

// TestWorkloadContextResultLimitsFoldsDeploymentArtifactBounds is the
// false-complete guard for the deployment-artifact candidate bounds (#7951).
// Each bound reason on ctx["limitations"] makes the config rows a lower
// bound, so result_limits.truncated must be true beside the partial_reasons
// that carry it. The context and story surfaces share the fold; both are
// driven, and a payload with no bound reason or an unrelated one stays false.
func TestWorkloadContextResultLimitsFoldsDeploymentArtifactBounds(t *testing.T) {
	t.Parallel()

	for _, surface := range []string{"context", "story"} {
		for _, reason := range DeploymentArtifactBoundReasons() {
			t.Run(surface+"/"+reason, func(t *testing.T) {
				t.Parallel()
				ctx := map[string]any{"limitations": []string{reason}}

				limits := WorkloadContextResultLimits(ctx, "workload:svc", surface)

				if !BoolVal(limits, "truncated") {
					t.Fatalf("result_limits.truncated = false next to limitations %v, want true", ctx["limitations"])
				}
				if reasons := ContextPartialReasons(ctx); !slices.Contains(reasons, reason) {
					t.Fatalf("partial_reasons = %v, want %q", reasons, reason)
				}
			})
		}
		t.Run(surface+"/no_bound", func(t *testing.T) {
			t.Parallel()
			ctx := map[string]any{"limitations": []string{"api_surface_read_degraded"}}

			limits := WorkloadContextResultLimits(ctx, "workload:svc", surface)

			if BoolVal(limits, "truncated") {
				t.Fatal("result_limits.truncated = true with no bound reason and no cut list, want false")
			}
		})
	}
	t.Run("absent_limitations", func(t *testing.T) {
		t.Parallel()
		if BoolVal(WorkloadContextResultLimits(map[string]any{}, "workload:svc", "context"), "truncated") {
			t.Fatal("result_limits.truncated = true on an empty payload, want false")
		}
	})
}

// TestHasDeploymentArtifactBoundMatchesTheReasonList keeps the helper and the
// list one source: every listed reason is a hit, other reasons are not.
func TestHasDeploymentArtifactBoundMatchesTheReasonList(t *testing.T) {
	t.Parallel()

	for _, reason := range DeploymentArtifactBoundReasons() {
		if !HasDeploymentArtifactBound([]string{"infrastructure_truncated", reason}) {
			t.Errorf("HasDeploymentArtifactBound missed %q", reason)
		}
	}
	if HasDeploymentArtifactBound([]string{"infrastructure_truncated", "hostnames_truncated"}) {
		t.Error("HasDeploymentArtifactBound = true for unrelated reasons")
	}
	if HasDeploymentArtifactBound(nil) {
		t.Error("HasDeploymentArtifactBound = true for nil limitations")
	}
}
