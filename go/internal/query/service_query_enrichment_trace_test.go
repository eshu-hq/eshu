package query

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
)

func TestTraceDeploymentChainSkipsIndirectEvidenceWhenDirectOnly(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{"entity_id", "repo_id", "relative_path", "entity_type", "entity_name", "start_line", "end_line", "language", "source_cache", "metadata"},
			rows:    [][]driver.Value{},
		},
	})

	workloadContext := map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-sample-service-api",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "instance:sample-service-api:prod",
				"platform_name": "sample-argocd",
				"platform_kind": "argocd_application",
				"environment":   "production",
			},
		},
	}

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN") {
					return nil, fmt.Errorf("unexpected indirect enrichment query with params %#v", params)
				}
				return nil, nil
			},
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id}) RETURN") {
					return nil, nil
				}
				return nil, nil
			},
		},
		NewContentReader(db),
		workloadContext,
		serviceQueryEnrichmentOptions{
			DirectOnly:                true,
			IncludeRelatedModuleUsage: false,
			MaxDepth:                  2,
		},
	)
	if err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}

	if _, exists := workloadContext["consumer_repositories"]; exists {
		t.Fatalf("consumer_repositories = %#v, want omitted for direct_only trace", workloadContext["consumer_repositories"])
	}
	if _, exists := workloadContext["provisioning_source_chains"]; exists {
		t.Fatalf("provisioning_source_chains = %#v, want omitted when related module usage is disabled", workloadContext["provisioning_source_chains"])
	}
}

func TestTraceDeploymentChainBoundsCrossRepoSearchByMaxDepth(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
	})

	workloadContext := map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repo-sample-service-api",
		"repo_name": "sample-service-api",
		"instances": []map[string]any{
			{
				"instance_id":   "instance:sample-service-api:prod",
				"platform_name": "sample-argocd",
				"platform_kind": "argocd_application",
				"environment":   "production",
			},
		},
	}

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN") {
					return []map[string]any{
						{
							"repo_id":             "repo-consumer-1",
							"repo_name":           "api-node-saved-search",
							"relationship_type":   "PROVISIONS_DEPENDENCY_FOR",
							"relationship_reason": "terraform_provider_reference",
						},
					}, nil
				}
				return nil, nil
			},
			runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id}) RETURN") {
					return map[string]any{
						"id":         "repo-sample-service-api",
						"name":       "sample-service-api",
						"path":       "/repos/sample-service-api",
						"local_path": "/repos/sample-service-api",
						"remote_url": "https://github.com/example/sample-service-api",
						"repo_slug":  "example/sample-service-api",
						"has_remote": true,
					}, nil
				}
				return nil, nil
			},
		},
		NewContentReader(db),
		workloadContext,
		serviceQueryEnrichmentOptions{
			DirectOnly:                false,
			IncludeRelatedModuleUsage: true,
			MaxDepth:                  2,
		},
	)
	if err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}

	if _, exists := workloadContext["consumer_repositories"]; !exists {
		t.Fatal("consumer_repositories missing, want cross-repo consumer evidence when direct_only is false")
	}
	if _, exists := workloadContext["provisioning_source_chains"]; !exists {
		t.Fatal("provisioning_source_chains missing, want related module evidence when include_related_module_usage is true")
	}

	var searchLimit int64
	foundAnyRepoSearch := false
	for i, query := range recorder.queries {
		if !strings.Contains(query, "content ILIKE '%' || $1 || '%'") &&
			!strings.Contains(query, "content LIKE '%' || $1 || '%'") {
			continue
		}
		if strings.Contains(query, "repo_id = $1") {
			continue
		}
		foundAnyRepoSearch = true
		if got, ok := recorder.args[i][1].(int64); ok {
			searchLimit = got
		} else {
			t.Fatalf("search limit type = %T, want int64", recorder.args[i][1])
		}
		break
	}
	if !foundAnyRepoSearch {
		t.Fatal("did not observe any cross-repo consumer search query")
	}
	if got, want := searchLimit, int64(20); got != want {
		t.Fatalf("cross-repo search limit = %d, want %d for max_depth=2", got, want)
	}
}
