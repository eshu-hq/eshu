package query

import (
	"context"
	"strings"
	"testing"
)

func TestEnrichServiceQueryContextQueriesProvisioningCandidatesOnce(t *testing.T) {
	t.Parallel()

	graph := &countingProvisioningGraph{}
	workloadContext := map[string]any{
		"id":        "workload:service-edge-api",
		"name":      "service-edge-api",
		"kind":      "Deployment",
		"repo_id":   "repo-service-edge-api",
		"repo_name": "service-edge-api",
		"instances": []map[string]any{},
	}

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		graph,
		fakePortContentStore{},
		workloadContext,
		serviceQueryEnrichmentOptions{IncludeRelatedModuleUsage: true},
	)
	if err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}
	if graph.provisioningCandidateCalls != 1 {
		t.Fatalf("provisioning candidate graph calls = %d, want 1", graph.provisioningCandidateCalls)
	}
}

type countingProvisioningGraph struct {
	provisioningCandidateCalls int
}

func (g *countingProvisioningGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE") {
		g.provisioningCandidateCalls++
		if got, want := params["repo_id"], "repo-service-edge-api"; got != want {
			return nil, nil
		}
		return []map[string]any{
			{
				"repo_id":             "repo-terraform-stack",
				"repo_name":           "terraform-stack-staging",
				"relationship_type":   "PROVISIONS_DEPENDENCY_FOR",
				"relationship_reason": "terraform_provider_reference",
			},
		}, nil
	}
	return nil, nil
}

func (g *countingProvisioningGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}
