package query

import (
	"context"
	"database/sql/driver"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestEnrichServiceQueryContextAddsServiceAndRelationshipSignals(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-service-edge-api", "deploy/staging/ingress.yaml", "sha-1", "", "hash-1", int64(8), "yaml", "yaml"},
				{"repo-service-edge-api", "specs/index.yaml", "sha-2", "", "hash-2", int64(15), "yaml", "yaml"},
			},
		},
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-service-edge-api", "deploy/staging/ingress.yaml", "sha-1", "spec:\n  rules:\n    - host: service-edge-api.staging.example.test\n", "hash-1", int64(8), "yaml", "yaml"},
			},
		},
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-service-edge-api", "specs/index.yaml", "sha-2", "openapi: 3.0.3\ninfo:\n  version: v1\nservers:\n  - url: https://service-edge-api.staging.example.test\npaths:\n  /widgets:\n    get:\n      operationId: listWidgets\n", "hash-2", int64(15), "yaml", "yaml"},
			},
		},
		// ListFrameworkRoutes query — no framework routes for this test.
		{
			columns: []string{"relative_path", "framework_semantics"},
			rows:    [][]driver.Value{},
		},
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-consumer-9", "configs/service.json", "sha-3", "", "hash-3", int64(3), "json", "json"},
			},
		},
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-consumer-9", "deploy/values.yaml", "sha-4", "", "hash-4", int64(5), "yaml", "yaml"},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"terraform-module-1", "repo-terraform-stack", "env/staging/main.tf", "TerraformModule", "service_module",
					int64(1), int64(10), "hcl", `module "service" { source = "../../modules/service" }`,
					[]byte(`{"source":"../../modules/service"}`),
				},
			},
		},
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-service-edge-api", ".github/workflows/deploy.yaml", "sha-5", "name: deploy\non:\n  workflow_dispatch:\njobs:\n  deploy:\n    steps:\n      - run: helm upgrade --install service-edge-api ./charts/service-edge-api\n", "hash-5", int64(12), "yaml", "github_actions_workflow"},
				{"repo-service-edge-api", "Jenkinsfile", "sha-6", "@Library('pipelines') _\npipelineDeploy()\nsh 'ansible-playbook deploy.yml -i inventory/staging.ini'\n", "hash-6", int64(16), "groovy", "groovy"},
				{"repo-service-edge-api", "charts/service-edge-api/values-staging.yaml", "sha-7", "service:\n  name: service-edge-api\n", "hash-7", int64(9), "yaml", "helm_values"},
				{"repo-service-edge-api", "group_vars/all.yml", "sha-8", "env: staging\n", "hash-8", int64(4), "yaml", "ansible_vars"},
				{"repo-service-edge-api", "inventory/staging.ini", "sha-9", "[staging]\nservice-edge-api\n", "hash-9", int64(4), "ini", "ansible_inventory"},
			},
		},
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-service-edge-api", "Jenkinsfile", "sha-6", "@Library('pipelines') _\npipelineDeploy()\nsh 'ansible-playbook deploy.yml -i inventory/staging.ini'\n", "hash-6", int64(16), "groovy", "groovy"},
			},
		},
		{
			columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"},
			rows: [][]driver.Value{
				{"repo-service-edge-api", ".github/workflows/deploy.yaml", "sha-5", "name: deploy\non:\n  workflow_dispatch:\njobs:\n  deploy:\n    steps:\n      - run: helm upgrade --install service-edge-api ./charts/service-edge-api\n", "hash-5", int64(12), "yaml", "github_actions_workflow"},
			},
		},
	})

	workloadContext := map[string]any{
		"id":        "workload:service-edge-api",
		"name":      "service-edge-api",
		"kind":      "Deployment",
		"repo_id":   "repo-service-edge-api",
		"repo_name": "service-edge-api",
		"instances": []map[string]any{
			{
				"instance_id":   "instance:staging",
				"platform_name": "staging-eks",
				"platform_kind": "eks",
				"environment":   "staging",
			},
		},
		"dependencies": []map[string]any{
			{"type": "DEPENDS_ON", "target_name": "shared-lib"},
		},
		"infrastructure": []map[string]any{
			{
				"type":      "HelmValues",
				"name":      "service-edge-api",
				"file_path": "charts/service-edge-api/values-staging.yaml",
			},
		},
	}

	err := enrichServiceQueryContext(context.Background(), fakeGraphReader{
		run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "RETURN related.id AS repo_id, related.name AS repo_name") {
				return nil, nil
			}
			if strings.Contains(cypher, "MATCH (r:Repository) WHERE r.id IN $repo_ids") {
				if got, want := params["repo_ids"], []string{"repo-consumer-9", "repo-terraform-stack"}; !reflect.DeepEqual(got, want) {
					t.Fatalf("repo name params[repo_ids] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":   "repo-consumer-9",
						"repo_name": "api-node-saved-search",
					},
					{
						"repo_id":   "repo-terraform-stack",
						"repo_name": "terraform-stack-staging",
					},
				}, nil
			}
			if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN") {
				if got, want := params["repo_id"], "repo-service-edge-api"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
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
		},
		runSingle: func(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (r:Repository {id: $repo_id}) RETURN") {
				if got, want := params["repo_id"], "repo-service-edge-api"; got != want {
					t.Fatalf("repo params[repo_id] = %#v, want %#v", got, want)
				}
				return map[string]any{
					"id":         "repo-service-edge-api",
					"name":       "service-edge-api",
					"path":       "/repos/service-edge-api",
					"local_path": "/repos/service-edge-api",
					"remote_url": "https://github.com/example/service-edge-api",
					"repo_slug":  "example/service-edge-api",
					"has_remote": true,
				}, nil
			}
			return nil, nil
		},
	}, NewContentReader(db), workloadContext)
	if err != nil {
		t.Fatalf("enrichServiceQueryContext() error = %v, want nil", err)
	}

	if got := len(mapSliceValue(workloadContext, "hostnames")); got != 1 {
		t.Fatalf("len(hostnames) = %d, want 1", got)
	}
	if got := len(mapSliceValue(workloadContext, "entrypoints")); got != 1 {
		t.Fatalf("len(entrypoints) = %d, want 1", got)
	}
	if got := len(mapSliceValue(workloadContext, "network_paths")); got != 1 {
		t.Fatalf("len(network_paths) = %d, want 1", got)
	}
	observedEnvironments := StringSliceVal(workloadContext, "observed_config_environments")
	if !containsString(observedEnvironments, "staging") {
		t.Fatalf("observed_config_environments = %#v, want to include %q", observedEnvironments, "staging")
	}

	apiSurface := mapValue(workloadContext, "api_surface")
	if got, want := IntVal(apiSurface, "endpoint_count"), 1; got != want {
		t.Fatalf("api_surface.endpoint_count = %d, want %d", got, want)
	}
	if got, want := IntVal(apiSurface, "spec_count"), 1; got != want {
		t.Fatalf("api_surface.spec_count = %d, want %d", got, want)
	}
	if got := len(mapSliceValue(apiSurface, "endpoints")); got != 1 {
		t.Fatalf("len(api_surface.endpoints) = %d, want 1", got)
	}

	dependents := mapSliceValue(workloadContext, "dependents")
	if len(dependents) != 1 {
		t.Fatalf("len(dependents) = %d, want 1", len(dependents))
	}

	consumers := mapSliceValue(workloadContext, "consumer_repositories")
	if len(consumers) != 2 {
		t.Fatalf("len(consumer_repositories) = %d, want 2", len(consumers))
	}
	if got, want := StringVal(consumers[0], "repository"), "terraform-stack-staging"; got != want {
		t.Fatalf("consumer_repositories[0].repository = %q, want %q", got, want)
	}
	if got, want := StringVal(consumers[1], "repository"), "api-node-saved-search"; got != want {
		t.Fatalf("consumer_repositories[1].repository = %q, want %q", got, want)
	}

	provisioningChains := mapSliceValue(workloadContext, "provisioning_source_chains")
	if len(provisioningChains) != 1 {
		t.Fatalf("len(provisioning_source_chains) = %d, want 1", len(provisioningChains))
	}
	if got, want := StringVal(provisioningChains[0], "repository"), "terraform-stack-staging"; got != want {
		t.Fatalf("provisioning_source_chains[0].repository = %q, want %q", got, want)
	}

	documentationOverview := mapValue(workloadContext, "documentation_overview")
	if got, want := documentationOverview["repo_slug"], "example/service-edge-api"; got != want {
		t.Fatalf("documentation_overview.repo_slug = %#v, want %#v", got, want)
	}

	supportOverview := mapValue(workloadContext, "support_overview")
	if got, want := supportOverview["consumer_repository_count"], 2; got != want {
		t.Fatalf("support_overview.consumer_repository_count = %#v, want %#v", got, want)
	}
	if got, want := supportOverview["dependent_count"], 1; got != want {
		t.Fatalf("support_overview.dependent_count = %#v, want %#v", got, want)
	}

	deploymentEvidence := mapValue(workloadContext, "deployment_evidence")
	if len(deploymentEvidence) == 0 {
		t.Fatal("deployment_evidence = nil, want repository-backed deployment evidence")
	}
	if got, want := StringSliceVal(deploymentEvidence, "tool_families"), []string{"ansible", "github_actions", "helm", "jenkins"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deployment_evidence.tool_families = %#v, want %#v", got, want)
	}
	deliveryPaths := mapSliceValue(deploymentEvidence, "delivery_paths")
	if len(deliveryPaths) < 2 {
		t.Fatalf("len(deployment_evidence.delivery_paths) = %d, want at least 2", len(deliveryPaths))
	}
	deliveryWorkflows := mapSliceValue(deploymentEvidence, "delivery_workflows")
	if len(deliveryWorkflows) != 1 {
		t.Fatalf("len(deployment_evidence.delivery_workflows) = %d, want 1", len(deliveryWorkflows))
	}
	if got, want := StringVal(deliveryWorkflows[0], "controller_kind"), "jenkins_pipeline"; got != want {
		t.Fatalf("deployment_evidence.delivery_workflows[0].controller_kind = %#v, want %#v", got, want)
	}

	deploymentOverview := mapValue(workloadContext, "deployment_overview")
	if got, want := deploymentOverview["delivery_path_count"], len(deliveryPaths); got != want {
		t.Fatalf("deployment_overview.delivery_path_count = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(deploymentOverview, "deployment_tool_families"), []string{"ansible", "github_actions", "helm", "jenkins"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deployment_overview.deployment_tool_families = %#v, want %#v", got, want)
	}
}

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
	fakePortContentStore
	t *testing.T
}

func (s failListRepoFilesContentStore) ListRepoFiles(context.Context, string, int) ([]FileContent, error) {
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
			{"repository": "api-node-saved-search"},
		},
		"provisioning_source_chains": []map[string]any{
			{"repository": "terraform-stack-staging"},
		},
		"deployment_evidence": map[string]any{
			"tool_families": []string{"github_actions", "helm"},
		},
	}

	got := buildServiceStoryResponse("service-edge-api", workloadContext)

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

func TestLoadConsumerRepositoryEnrichmentWithoutTraceLimitUsesBoundedDefaultSearchLimit(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"}, rows: [][]driver.Value{}},
		{columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"}, rows: [][]driver.Value{}},
		{columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"}, rows: [][]driver.Value{}},
		{columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"}, rows: [][]driver.Value{}},
		{columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"}, rows: [][]driver.Value{}},
		{columns: []string{"repo_id", "relative_path", "commit_sha", "content", "content_hash", "line_count", "language", "artifact_type"}, rows: [][]driver.Value{}},
	})

	_, err := loadConsumerRepositoryEnrichment(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("cypher = %q, want bounded default limit in enrichment path", cypher)
				}
				return nil, nil
			},
		},
		NewContentReader(db),
		"repo-sample-service-api",
		"sample-service-api",
		[]string{
			" sample-service.prod.example.test ",
			"sample-service.qa.example.test",
			"sample-service.stage.example.test",
			"sample-service.dev.example.test",
			"sample-service.extra.example.test",
			"sample-service.qa.example.test",
		},
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichment() error = %v, want nil", err)
	}

	gotSearchTerms := make([]string, 0, len(recorder.args))
	for i, query := range recorder.queries {
		if (!strings.Contains(query, "content ILIKE '%' || $1 || '%'") &&
			!strings.Contains(query, "content LIKE '%' || $1 || '%'")) || len(recorder.args[i]) < 2 {
			continue
		}
		term, ok := recorder.args[i][0].(string)
		if !ok {
			t.Fatalf("recorder.args[%d][0] type = %T, want string", i, recorder.args[i][0])
		}
		if term == "sample-service-api" && !strings.Contains(query, "content LIKE '%' || $1 || '%'") {
			t.Fatalf("service-name query = %q, want exact-case LIKE for lower-case service token", query)
		}
		if term != "sample-service-api" && !strings.Contains(query, "content LIKE '%' || $1 || '%'") {
			t.Fatalf("hostname query for %q = %q, want case-sensitive LIKE", term, query)
		}
		gotSearchTerms = append(gotSearchTerms, term)
		if got, want := numericDriverValue(t, recorder.args[i][1]), int64(25); got != want {
			t.Fatalf("search limit = %d, want %d for default any-repo search", got, want)
		}
	}

	wantSearchTerms := []string{
		"sample-service-api",
		"sample-service.dev.example.test",
		"sample-service.extra.example.test",
		"sample-service.prod.example.test",
		"sample-service.qa.example.test",
	}
	sort.Strings(gotSearchTerms)
	if !reflect.DeepEqual(gotSearchTerms, wantSearchTerms) {
		t.Fatalf("search terms = %#v, want %#v", gotSearchTerms, wantSearchTerms)
	}
}
