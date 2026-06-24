// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestDeployableUnitCorrelationHandleSplitsMultipleDockerfilesConservatively(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-monolith",
				"monolith",
				[]map[string]any{
					{
						"repo_id":       "repo-monolith",
						"language":      "dockerfile",
						"relative_path": "docker/api.Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
					{
						"repo_id":       "repo-monolith",
						"language":      "dockerfile",
						"relative_path": "docker/worker.Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
				},
			),
		},
		ResolvedLoader: &stubDeployableUnitResolvedLoader{
			resolved: []relationships.ResolvedRelationship{
				{
					SourceRepoID:     "repo-monolith",
					TargetRepoID:     "repo-deployments",
					RelationshipType: relationships.RelDeploysFrom,
					Confidence:       0.94,
					Details: map[string]any{
						"evidence_kinds": []string{
							string(relationships.EvidenceKindArgoCDAppSource),
						},
					},
				},
			},
		},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("monolith"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(got.EvidenceSummary, "evaluated 2 deployable unit candidate") {
		t.Fatalf("Handle().EvidenceSummary = %q, want two evaluated candidates", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "admitted=0") {
		t.Fatalf("Handle().EvidenceSummary = %q, want admitted=0 for ambiguous repo-level deploy evidence", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "rejected=2") {
		t.Fatalf("Handle().EvidenceSummary = %q, want rejected=2", got.EvidenceSummary)
	}
}

func TestDeployableUnitCorrelationHandleAdmitsJenkinsBackedServiceCandidate(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-service-jenkins",
				"service-jenkins",
				[]map[string]any{
					{
						"repo_id":       "repo-service-jenkins",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
					{
						"repo_id":       "repo-service-jenkins",
						"language":      "groovy",
						"relative_path": "Jenkinsfile",
						"parsed_file_data": map[string]any{
							"jenkins_pipeline_calls": []any{"deployShared"},
						},
					},
				},
			),
		},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("service-jenkins"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(got.EvidenceSummary, "admitted=1") {
		t.Fatalf("Handle().EvidenceSummary = %q, want admitted=1", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "rejected=0") {
		t.Fatalf("Handle().EvidenceSummary = %q, want rejected=0", got.EvidenceSummary)
	}
}

func TestDeployableUnitCorrelationHandleRejectsSecondaryDockerfileWithoutIndependentEvidence(t *testing.T) {
	t.Parallel()

	handler := DeployableUnitCorrelationHandler{
		FactLoader: &stubDeployableUnitFactLoader{
			envelopes: deployableUnitCorrelationEnvelopes(
				"repo-multi",
				"multi-dockerfile-repo",
				[]map[string]any{
					{
						"repo_id":       "repo-multi",
						"language":      "dockerfile",
						"relative_path": "Dockerfile",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
					{
						"repo_id":       "repo-multi",
						"language":      "dockerfile",
						"relative_path": "Dockerfile.test",
						"parsed_file_data": map[string]any{
							"dockerfile_stages": []any{
								map[string]any{"name": "runtime"},
							},
						},
					},
					{
						"repo_id": "repo-multi",
						"parsed_file_data": map[string]any{
							"k8s_resources": []any{
								map[string]any{
									"name":      "multi-dockerfile-repo",
									"kind":      "Deployment",
									"namespace": "production",
								},
							},
						},
					},
				},
			),
		},
	}

	got, err := handler.Handle(context.Background(), deployableUnitIntent("multi-dockerfile-repo"))
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if !strings.Contains(got.EvidenceSummary, "evaluated 2 deployable unit candidate") {
		t.Fatalf("Handle().EvidenceSummary = %q, want two evaluated candidates", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "admitted=1") {
		t.Fatalf("Handle().EvidenceSummary = %q, want admitted=1", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "rejected=1") {
		t.Fatalf("Handle().EvidenceSummary = %q, want rejected=1", got.EvidenceSummary)
	}
}

func TestDeployableUnitKeyFromPathPreservesExplicitUnitKeys(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		repoName     string
		relativePath string
		want         string
	}{
		{
			name:         "repo root dockerfile uses repo name",
			repoName:     "sample-service-api",
			relativePath: "Dockerfile",
			want:         "sample-service-api",
		},
		{
			name:         "named dockerfile remains distinct",
			repoName:     "multi-dockerfile-repo",
			relativePath: "Dockerfile.test",
			want:         "test",
		},
		{
			name:         "dot suffix dockerfile remains distinct",
			repoName:     "monolith",
			relativePath: "docker/api.Dockerfile",
			want:         "api",
		},
		{
			name:         "nested service dockerfile remains distinct",
			repoName:     "monolith",
			relativePath: "services/api/Dockerfile",
			want:         "api",
		},
		{
			name:         "support folder remote dockerfile collapses to repo service",
			repoName:     "sample-service-api",
			relativePath: "docker/remote/Dockerfile",
			want:         "sample-service-api",
		},
		{
			name:         "support folder local dockerfile collapses to repo service",
			repoName:     "sample-service-api",
			relativePath: "docker/local/Dockerfile",
			want:         "sample-service-api",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := deployableUnitKeyFromPath(tc.repoName, tc.relativePath); got != tc.want {
				t.Fatalf("deployableUnitKeyFromPath(%q, %q) = %q, want %q", tc.repoName, tc.relativePath, got, tc.want)
			}
		})
	}
}
