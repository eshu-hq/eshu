// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestLoadServiceDeploymentEvidenceUsesGraphEvidenceWithoutContentHydration(t *testing.T) {
	t.Parallel()

	content := failListRepoFilesContentStore{t: t}
	workloadContext := map[string]any{
		"repo_id":   "repo-service",
		"repo_name": "service-edge-api",
		"deployment_evidence": map[string]any{
			"truth_basis":       "graph",
			"artifact_count":    2,
			"artifact_families": []string{"github_actions", "helm"},
		},
	}

	got, err := loadServiceDeploymentEvidence(context.Background(), nil, content, workloadContext)
	if err != nil {
		t.Fatalf("loadServiceDeploymentEvidence() error = %v, want nil", err)
	}
	if got["truth_basis"] != "graph" {
		t.Fatalf("truth_basis = %#v, want graph", got["truth_basis"])
	}
	if got["artifact_count"] != 2 {
		t.Fatalf("artifact_count = %#v, want 2", got["artifact_count"])
	}
}

type failListRepoFilesContentStore struct {
	querycontract.ContentStore
	t *testing.T
}

func (s failListRepoFilesContentStore) ListRepoFiles(context.Context, string, int) ([]querycontract.FileContent, error) {
	s.t.Fatal("ListRepoFiles should not run when graph deployment evidence already exists")
	return nil, nil
}

func TestBuildServiceStoryResponseKeepsStoryFirstKeysWithDossier(t *testing.T) {
	t.Parallel()

	workloadContext := map[string]any{
		"name":  "service-edge-api",
		"story": "ignored by builder",
		"story_sections": []map[string]any{
			{"title": "deployment", "summary": "1 instance"},
		},
		"deployment_overview": map[string]any{
			"instance_count": 1,
		},
		"documentation_overview": map[string]any{
			"repo_slug": "example/service-edge-api",
		},
		"support_overview": map[string]any{
			"endpoint_count": 3,
		},
		"hostnames": []map[string]any{
			{"hostname": "service-edge-api.qa.example.test"},
		},
		"entrypoints": []map[string]any{
			{"type": "hostname", "target": "service-edge-api.qa.example.test"},
		},
		"network_paths": []map[string]any{
			{"path_type": "hostname_to_runtime"},
		},
		"api_surface": map[string]any{
			"endpoint_count": 3,
			"endpoints": []map[string]any{
				{"path": "/widgets"},
			},
		},
		"dependents": []map[string]any{
			{"repository": "deployment-helm"},
		},
		"consumer_repositories": []map[string]any{
			{"repository": "svc-saved-search"},
		},
		"provisioning_source_chains": []map[string]any{
			{"repository": "terraform-stack-staging"},
		},
		"deployment_evidence": map[string]any{
			"tool_families": []string{"github_actions", "helm"},
		},
	}

	got := BuildServiceStoryResponse("service-edge-api", workloadContext)

	for _, key := range []string{
		"service_name",
		"story",
		"story_sections",
		"deployment_overview",
		"documentation_overview",
		"support_overview",
		"service_identity",
		"api_surface",
		"deployment_lanes",
		"upstream_dependencies",
		"downstream_consumers",
		"evidence_graph",
		"result_limits",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("response missing required story-first key %q: %#v", key, got)
		}
	}

	for _, key := range []string{
		"hostnames",
		"entrypoints",
		"network_paths",
		"api_surface",
		"dependents",
		"consumer_repositories",
		"provisioning_source_chains",
		"deployment_evidence",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("response[%q] missing, want included in service dossier", key)
		}
	}
}
