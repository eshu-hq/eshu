// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5663: response-level proof that buildDeploymentFactSummary surfaces
// live_instance_count_truncated, split out of
// impact_trace_deployment_live_evidence_test.go to stay under the repo's
// 500-line-per-file cap. Shares that file's sampleServiceDossierContext
// fixture (service_story_dossier_test.go).

package query

import "testing"

// TestBuildDeploymentFactSummaryLiveInstanceCountTruncatedTrue is the #5663
// response-level proof: when the handler observed an anchor read hitting
// serviceStoryItemLimit (workloadContext["_live_instance_count_truncated"] =
// true), deployment_fact_summary.live_instance_count_truncated must surface
// that true value, disclosing the count as a lower bound.
func TestBuildDeploymentFactSummaryLiveInstanceCountTruncatedTrue(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["_live_instance_count"] = 50
	ctx["_live_instance_count_truncated"] = true
	instances, _ := ctx["instances"].([]map[string]any)
	summary := buildDeploymentFactSummary(
		ctx, instances, []string{"production"}, nil, []string{"eks-prod"},
		nil, nil, nil, nil, nil, "controller", true,
	)
	if count := summary["live_instance_count"]; count != 50 {
		t.Fatalf("live_instance_count = %v, want 50", count)
	}
	if truncated, ok := summary["live_instance_count_truncated"]; !ok {
		t.Fatal("live_instance_count_truncated missing, want present (true)")
	} else if truncated != true {
		t.Fatalf("live_instance_count_truncated = %v, want true", truncated)
	}
}
