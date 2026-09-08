// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
)

func TestDeploymentTraceProvenanceIncludesRuntimeArtifactFamilies(t *testing.T) {
	t.Parallel()

	deploymentEvidence := map[string]any{
		"delivery_paths": []map[string]any{
			{
				"kind":          "runtime_artifact",
				"artifact_type": "cloudformation_serverless",
				"path":          "template.yml",
			},
			{
				"kind":            "controller_artifact",
				"controller_kind": "jenkins_pipeline",
				"path":            "Jenkinsfile",
			},
		},
	}

	got := impacttrace.BuildDeploymentTraceProvenanceOverview(nil, nil, deploymentEvidence, nil)
	families := StringSliceVal(got, "families")
	if !slices.Contains(families, "cloudformation") {
		t.Fatalf("families = %#v, want cloudformation", families)
	}
	if !slices.Contains(families, "jenkins") {
		t.Fatalf("families = %#v, want jenkins", families)
	}
	if got, want := IntVal(got, "runtime_artifact_count"), 1; got != want {
		t.Fatalf("runtime_artifact_count = %d, want %d", got, want)
	}

	story := impacttrace.BuildDeploymentTraceWorkflowProvenanceStory(deploymentEvidence)
	if !strings.Contains(story, "Runtime provenance: template.yml.") {
		t.Fatalf("story = %q, want runtime provenance line", story)
	}
}
