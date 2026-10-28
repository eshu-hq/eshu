// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"testing"
)

// TestCov7EvidenceBoundariesFor proves the static boundary table: workload
// story and deployment-chain surfaces disclose their Postgres-only gaps in
// domain order, while served surfaces disclose nothing.
func TestCov7EvidenceBoundariesFor(t *testing.T) {
	t.Parallel()

	if got := EvidenceBoundariesFor("get_repo_story"); got != nil {
		t.Fatalf("repo story boundaries = %#v, want nil", got)
	}
	if got := EvidenceBoundariesFor("get_service_story"); got != nil {
		t.Fatalf("service story boundaries = %#v, want nil", got)
	}
	if got := EvidenceBoundariesFor("unknown_surface"); got != nil {
		t.Fatalf("unknown surface boundaries = %#v, want nil", got)
	}
	workload := EvidenceBoundariesFor("get_workload_story")
	if len(workload) != 2 {
		t.Fatalf("workload boundaries = %#v, want 2 entries", workload)
	}
	if workload[0].Domain != "ci_cd_run_correlation" || workload[1].Domain != "package_correlation_consumption" {
		t.Fatalf("workload boundaries = %#v, want domain-sorted pair", workload)
	}
	for _, boundary := range workload {
		if boundary.ReadSurface != "get_workload_story" || boundary.Reason != BoundaryReasonPostgresOnly {
			t.Fatalf("boundary = %#v, want surface and postgres-only reason", boundary)
		}
	}
	chain := EvidenceBoundariesFor("trace_deployment_chain")
	if len(chain) != 2 || chain[0].Domain != "ci_cd_run_correlation" || chain[1].Domain != "container_image_identity" {
		t.Fatalf("chain boundaries = %#v, want sorted correlation/identity pair", chain)
	}
}

// TestCov7AttachEvidenceBoundaries proves the attach helper writes the field
// only when boundaries exist and never touches a nil map.
func TestCov7AttachEvidenceBoundaries(t *testing.T) {
	t.Parallel()

	AttachEvidenceBoundaries(nil, "trace_deployment_chain")
	quiet := map[string]any{"a": 1}
	AttachEvidenceBoundaries(quiet, "get_service_story")
	if _, ok := quiet["evidence_boundaries"]; ok {
		t.Fatalf("served surface gained boundaries: %#v", quiet)
	}
	disclosed := map[string]any{}
	AttachEvidenceBoundaries(disclosed, "trace_deployment_chain")
	boundaries, ok := disclosed["evidence_boundaries"].([]PostgresOnlyBoundary)
	if !ok || len(boundaries) != 2 {
		t.Fatalf("attached = %#v, want typed boundary pair", disclosed["evidence_boundaries"])
	}
}

// TestCov7EvidenceCitationHandleKey proves the dedup key ignores the
// presentation-only confidence while separating distinct citations.
func TestCov7EvidenceCitationHandleKey(t *testing.T) {
	t.Parallel()

	base := EvidenceCitationHandle{Kind: "file", RepoID: "r1", RelativePath: "a.go", StartLine: 1, EndLine: 2, Confidence: 0.3}
	same := base
	same.Confidence = 0.9
	if base.EvidenceCitationHandleKey() != same.EvidenceCitationHandleKey() {
		t.Fatalf("confidence change altered the dedup key")
	}
	other := base
	other.EndLine = 3
	if base.EvidenceCitationHandleKey() == other.EvidenceCitationHandleKey() {
		t.Fatalf("distinct citations share a dedup key")
	}
}
