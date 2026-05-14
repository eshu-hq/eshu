package query

import (
	"context"
	"strings"
	"testing"
)

func TestQueryServiceDeploymentEvidenceUsesReadModelBeforeGraphFallback(t *testing.T) {
	t.Parallel()

	graph := fakeGraphReader{
		run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "EvidenceArtifact") {
				t.Fatalf("cypher = %q, want service deployment evidence read model before graph fallback", cypher)
			}
			return nil, nil
		},
	}
	content := fakePortContentStore{
		deploymentEvidence: repositoryDeploymentEvidenceReadModel{
			Available: true,
			Rows: []map[string]any{
				{
					"direction":         "incoming",
					"artifact_id":       "evidence-artifact:terraform:1",
					"name":              "environments/prod/ecs.tf",
					"domain":            "deployment",
					"path":              "environments/prod/ecs.tf",
					"evidence_kind":     "TERRAFORM_ECS_SERVICE",
					"artifact_family":   "terraform",
					"extractor":         "terraform-runtime-service-module",
					"relationship_type": "PROVISIONS_DEPENDENCY_FOR",
					"resolved_id":       "resolved-runtime",
					"generation_id":     "gen-runtime",
					"confidence":        0.96,
					"source_repo_id":    "repo-platform",
					"source_repo_name":  "runtime-platform",
					"target_repo_id":    "repo-service",
					"target_repo_name":  "checkout-service",
				},
			},
		},
	}

	got, err := queryServiceGraphDeploymentEvidence(context.Background(), graph, content, "repo-service")
	if err != nil {
		t.Fatalf("queryServiceGraphDeploymentEvidence() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("queryServiceGraphDeploymentEvidence() = nil, want read-model evidence")
	}
	artifacts := mapSliceValue(got, "artifacts")
	if len(artifacts) != 1 {
		t.Fatalf("len(artifacts) = %d, want 1", len(artifacts))
	}
	if got, want := StringVal(artifacts[0], "source_repo_id"), "repo-platform"; got != want {
		t.Fatalf("source_repo_id = %#v, want %#v", got, want)
	}
}
