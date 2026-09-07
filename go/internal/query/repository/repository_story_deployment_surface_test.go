// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestBuildRepositoryStoryResponseDoesNotMarkDeploymentUnknownWhenWorkloadHasDeliveryEvidence(t *testing.T) {
	t.Parallel()

	repo := querycontract.RepoRef{ID: "repository:payments", Name: "payments"}
	got := buildRepositoryStoryResponse(
		repo,
		42,
		[]string{"go", "yaml"},
		[]string{"payments-api"},
		nil,
		2,
		map[string]any{
			"families": []string{"argocd", "helm"},
			"deployment_artifacts": map[string]any{
				"controller_artifacts": []map[string]any{
					{
						"path":            "Jenkinsfile",
						"controller_kind": "jenkins_pipeline",
						"entry_points":    []string{"dist/api.js"},
					},
				},
			},
		},
		nil,
	)

	limitations, ok := got["limitations"].([]string)
	if !ok {
		t.Fatalf("limitations type = %T, want []string", got["limitations"])
	}
	if querycontract.ContainsString(limitations, "deployment_surface_unknown") {
		t.Fatalf("limitations = %#v, must not claim deployment surface unknown when workload delivery evidence exists", limitations)
	}
}

func TestBuildRepositoryStoryResponseDoesNotMarkDeploymentUnknownWhenEvidenceCountExists(t *testing.T) {
	t.Parallel()

	got := buildRepositoryStoryResponse(
		querycontract.RepoRef{ID: "repository:deploy", Name: "deploy"},
		3,
		[]string{"yaml"},
		nil,
		nil,
		0,
		map[string]any{
			"deployment_evidence": map[string]any{
				"artifact_count":     1,
				"artifact_families":  []string{"terraform"},
				"relationship_types": []string{"PROVISIONS_DEPENDENCY_FOR"},
			},
		},
		nil,
	)

	limitations, ok := got["limitations"].([]string)
	if !ok {
		t.Fatalf("limitations type = %T, want []string", got["limitations"])
	}
	if querycontract.ContainsString(limitations, "deployment_surface_unknown") {
		t.Fatalf("limitations = %#v, must not claim deployment surface unknown when deployment evidence count exists", limitations)
	}
	if !querycontract.ContainsString(limitations, "workload_surface_unknown") {
		t.Fatalf("limitations = %#v, want workload surface still marked unknown", limitations)
	}
}
