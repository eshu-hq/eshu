package query

import (
	"context"
	"database/sql/driver"
	"reflect"
	"strings"
	"testing"
)

func TestLoadProvisioningSourceChainsBuildsCompactTerraformEvidence(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"terraform-module-1", "repo-terraform-stack", "env/prod/main.tf", "TerraformModule", "service_module",
					int64(1), int64(10), "hcl", `module "service" { source = "../../modules/service" }`,
					[]byte(`{"source":"../../modules/service"}`),
				},
				{
					"terragrunt-config-1", "repo-terraform-stack", "env/prod/terragrunt.hcl", "TerragruntConfig", "prod",
					int64(1), int64(24), "hcl", "terraform { source = \"../../modules/service\" }",
					[]byte(`{"terraform_source":"../../modules/service","include_paths":["../root.hcl"],"read_config_paths":["../env.hcl"]}`),
				},
				{
					"terragrunt-dependency-1", "repo-terraform-stack", "env/prod/dependency.hcl", "TerragruntDependency", "network",
					int64(1), int64(8), "hcl", `dependency "network" { config_path = "../network" }`,
					[]byte(`{"config_path":"../network"}`),
				},
			},
		},
	})

	got, err := loadProvisioningSourceChains(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN") {
					t.Fatalf("cypher = %q, want provisioning relationship filter", cypher)
				}
				if got, want := params["repo_id"], "repo-sample-service-api"; got != want {
					t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
				}
				return []map[string]any{
					{
						"repo_id":             "repo-terraform-stack",
						"repo_name":           "terraform-stack-prod",
						"relationship_type":   "PROVISIONS_DEPENDENCY_FOR",
						"relationship_reason": "terraform_provider_reference",
					},
					{
						"repo_id":             "repo-terraform-stack",
						"repo_name":           "terraform-stack-prod",
						"relationship_type":   "USES_MODULE",
						"relationship_reason": "terraform_module_source_path",
					},
				}, nil
			},
		},
		NewContentReader(db),
		"repo-sample-service-api",
	)
	if err != nil {
		t.Fatalf("loadProvisioningSourceChains() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(loadProvisioningSourceChains()) = %d, want 1", len(got))
	}

	chain := got[0]
	if got, want := chain["repository"], "terraform-stack-prod"; got != want {
		t.Fatalf("chain[repository] = %#v, want %#v", got, want)
	}
	if got, want := chain["repo_id"], "repo-terraform-stack"; got != want {
		t.Fatalf("chain[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(chain, "relationship_types"), []string{"PROVISIONS_DEPENDENCY_FOR", "USES_MODULE"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chain[relationship_types] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(chain, "evidence_kinds"), []string{
		"terraform_module_source",
		"terraform_module_source_path",
		"terraform_provider_reference",
		"terragrunt_dependency_config_path",
		"terragrunt_include_path",
		"terragrunt_read_config",
		"terragrunt_terraform_source",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chain[evidence_kinds] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(chain, "sample_paths"), []string{
		"env/prod/dependency.hcl",
		"env/prod/main.tf",
		"env/prod/terragrunt.hcl",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chain[sample_paths] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(chain, "modules"), []string{"../../modules/service"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chain[modules] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(chain, "config_paths"), []string{"../env.hcl", "../network", "../root.hcl"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chain[config_paths] = %#v, want %#v", got, want)
	}
}

func TestLoadConsumerRepositoryEnrichmentPreservesDualViews(t *testing.T) {
	t.Parallel()

	content := patternConsumerSearchContentStore{
		fileRows: map[string][]FileContent{
			"sample-service-api": {
				{RepoID: "repo-consumer-1", RelativePath: "config/service.json"},
			},
		},
		exactRows: map[string][]FileContent{
			"sample-service-api.qa.example.test": {
				{RepoID: "repo-consumer-1", RelativePath: "deploy/values.yaml"},
			},
		},
	}

	got, err := loadConsumerRepositoryEnrichment(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN"):
					if got, want := params["repo_id"], "repo-sample-service-api"; got != want {
						t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"repo_id":             "repo-consumer-1",
							"repo_name":           "api-node-saved-search",
							"relationship_type":   "PROVISIONS_DEPENDENCY_FOR",
							"relationship_reason": "terraform_provider_reference",
						},
						{
							"repo_id":             "repo-consumer-2",
							"repo_name":           "terraform-stack-prod",
							"relationship_type":   "USES_MODULE",
							"relationship_reason": "terraform_module_source_path",
						},
					}, nil
				case strings.Contains(cypher, "MATCH (r:Repository) WHERE r.id IN $repo_ids"):
					return nil, nil
				default:
					t.Fatalf("cypher = %q, want provisioning or repo lookup query", cypher)
					return nil, nil
				}
			},
		},
		content,
		"repo-sample-service-api",
		"sample-service-api",
		[]string{"sample-service-api.qa.example.test"},
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichment() error = %v, want nil", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(loadConsumerRepositoryEnrichment()) = %d, want 2", len(got))
	}

	first := got[0]
	if got, want := first["repository"], "api-node-saved-search"; got != want {
		t.Fatalf("got[0][repository] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(first, "consumer_kinds"), []string{
		"graph_provisioning_consumer",
		"hostname_reference_consumer",
		"service_reference_consumer",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got[0][consumer_kinds] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(first, "graph_relationship_types"), []string{"PROVISIONS_DEPENDENCY_FOR"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got[0][graph_relationship_types] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(first, "evidence_kinds"), []string{"hostname_reference", "repository_reference"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got[0][evidence_kinds] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(first, "matched_values"), []string{"sample-service-api", "sample-service-api.qa.example.test"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got[0][matched_values] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(first, "sample_paths"), []string{"config/service.json", "deploy/values.yaml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got[0][sample_paths] = %#v, want %#v", got, want)
	}

	second := got[1]
	if got, want := second["repository"], "terraform-stack-prod"; got != want {
		t.Fatalf("got[1][repository] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(second, "consumer_kinds"), []string{"graph_provisioning_consumer"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got[1][consumer_kinds] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(second, "evidence_kinds"), []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("got[1][evidence_kinds] = %#v, want %#v", got, want)
	}
}

func TestLoadConsumerRepositoryEnrichmentFindsCrossRepoConsumersOutsideGraphCandidates(t *testing.T) {
	t.Parallel()

	content := patternConsumerSearchContentStore{
		fileRows: map[string][]FileContent{
			"sample-service-api": {
				{RepoID: "repo-consumer-9", RelativePath: "configs/service.json"},
			},
		},
		exactRows: map[string][]FileContent{
			"sample-service-api.qa.example.test": {
				{RepoID: "repo-consumer-9", RelativePath: "deploy/values.yaml"},
			},
		},
	}

	got, err := loadConsumerRepositoryEnrichment(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN"):
					if got, want := params["repo_id"], "repo-sample-service-api"; got != want {
						t.Fatalf("params[repo_id] = %#v, want %#v", got, want)
					}
					return nil, nil
				case strings.Contains(cypher, "MATCH (r:Repository) WHERE r.id IN $repo_ids"):
					return nil, nil
				default:
					t.Fatalf("cypher = %q, want provisioning or repo lookup query", cypher)
					return nil, nil
				}
			},
		},
		content,
		"repo-sample-service-api",
		"sample-service-api",
		[]string{"sample-service-api.qa.example.test"},
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichment() error = %v, want nil", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(loadConsumerRepositoryEnrichment()) = %d, want 1", len(got))
	}

	consumer := got[0]
	if got, want := consumer["repo_id"], "repo-consumer-9"; got != want {
		t.Fatalf("consumer[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(consumer, "consumer_kinds"), []string{
		"hostname_reference_consumer",
		"service_reference_consumer",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("consumer[consumer_kinds] = %#v, want %#v", got, want)
	}
	if got, want := StringSliceVal(consumer, "matched_values"), []string{
		"sample-service-api",
		"sample-service-api.qa.example.test",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("consumer[matched_values] = %#v, want %#v", got, want)
	}
}

func TestLoadConsumerRepositoryEnrichmentWithLimitCapsMergedConsumersByEvidenceStrength(t *testing.T) {
	t.Parallel()

	content := patternConsumerSearchContentStore{
		fileRows: map[string][]FileContent{
			"sample-service-api": {
				{RepoID: "repo-consumer-1", RelativePath: "config/service.json"},
				{RepoID: "repo-consumer-3", RelativePath: "config/service.json"},
			},
		},
		exactRows: map[string][]FileContent{
			"sample-service-api.qa.example.test": {
				{RepoID: "repo-consumer-1", RelativePath: "deploy/values.yaml"},
				{RepoID: "repo-consumer-4", RelativePath: "deploy/values.yaml"},
			},
		},
	}

	got, err := loadConsumerRepositoryEnrichmentWithLimit(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN"):
					if got, want := params["limit"], 2; got != want {
						t.Fatalf("params[limit] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"repo_id":             "repo-consumer-1",
							"repo_name":           "alpha-consumer",
							"relationship_type":   "PROVISIONS_DEPENDENCY_FOR",
							"relationship_reason": "terraform_provider_reference",
						},
						{
							"repo_id":             "repo-consumer-2",
							"repo_name":           "beta-consumer",
							"relationship_type":   "USES_MODULE",
							"relationship_reason": "terraform_module_source_path",
						},
					}, nil
				case strings.Contains(cypher, "MATCH (r:Repository) WHERE r.id IN $repo_ids"):
					if got, want := params["repo_ids"], []string{"repo-consumer-1", "repo-consumer-2", "repo-consumer-3", "repo-consumer-4"}; !reflect.DeepEqual(got, want) {
						t.Fatalf("params[repo_ids] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{"repo_id": "repo-consumer-1", "repo_name": "alpha-consumer"},
						{"repo_id": "repo-consumer-2", "repo_name": "beta-consumer"},
					}, nil
				default:
					t.Fatalf("unexpected cypher = %q", cypher)
					return nil, nil
				}
			},
		},
		content,
		"repo-sample-service-api",
		"sample-service-api",
		[]string{"sample-service-api.qa.example.test"},
		2,
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichmentWithLimit() error = %v, want nil", err)
	}
	if gotLen, wantLen := len(got), 2; gotLen != wantLen {
		t.Fatalf("len(loadConsumerRepositoryEnrichmentWithLimit()) = %d, want %d", gotLen, wantLen)
	}
	if gotRepo, wantRepo := StringVal(got[0], "repository"), "alpha-consumer"; gotRepo != wantRepo {
		t.Fatalf("got[0][repository] = %q, want %q", gotRepo, wantRepo)
	}
	if gotRepo, wantRepo := StringVal(got[1], "repository"), "beta-consumer"; gotRepo != wantRepo {
		t.Fatalf("got[1][repository] = %q, want %q", gotRepo, wantRepo)
	}
}

func TestLoadConsumerRepositoryEnrichmentBackfillsRepositoryNamesForContentOnlyConsumers(t *testing.T) {
	t.Parallel()

	content := patternConsumerSearchContentStore{
		fileRows: map[string][]FileContent{
			"sample-service-api": {
				{RepoID: "repo-consumer-9", RelativePath: "configs/service.json"},
			},
		},
		exactRows: map[string][]FileContent{
			"sample-service-api.qa.example.test": {
				{RepoID: "repo-consumer-9", RelativePath: "deploy/values.yaml"},
			},
		},
	}

	got, err := loadConsumerRepositoryEnrichment(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN"):
					return nil, nil
				case strings.Contains(cypher, "MATCH (r:Repository) WHERE r.id IN $repo_ids"):
					if got, want := params["repo_ids"], []string{"repo-consumer-9"}; !reflect.DeepEqual(got, want) {
						t.Fatalf("params[repo_ids] = %#v, want %#v", got, want)
					}
					return []map[string]any{
						{
							"repo_id":   "repo-consumer-9",
							"repo_name": "api-node-saved-search",
						},
					}, nil
				default:
					t.Fatalf("unexpected cypher = %q", cypher)
					return nil, nil
				}
			},
		},
		content,
		"repo-sample-service-api",
		"sample-service-api",
		[]string{"sample-service-api.qa.example.test"},
	)
	if err != nil {
		t.Fatalf("loadConsumerRepositoryEnrichment() error = %v, want nil", err)
	}
	if gotLen, wantLen := len(got), 1; gotLen != wantLen {
		t.Fatalf("len(loadConsumerRepositoryEnrichment()) = %d, want %d", gotLen, wantLen)
	}

	consumer := got[0]
	if gotRepo, wantRepo := StringVal(consumer, "repository"), "api-node-saved-search"; gotRepo != wantRepo {
		t.Fatalf("consumer[repository] = %q, want %q", gotRepo, wantRepo)
	}
	if gotRepoName, wantRepoName := StringVal(consumer, "repo_name"), "api-node-saved-search"; gotRepoName != wantRepoName {
		t.Fatalf("consumer[repo_name] = %q, want %q", gotRepoName, wantRepoName)
	}
}

func TestBoundedTraceEnrichmentLimitUsesOperatorSafeDefault(t *testing.T) {
	t.Parallel()

	if got, want := boundedTraceEnrichmentLimit(0), 25; got != want {
		t.Fatalf("boundedTraceEnrichmentLimit(0) = %d, want %d", got, want)
	}
	if got, want := boundedTraceEnrichmentLimit(3), 30; got != want {
		t.Fatalf("boundedTraceEnrichmentLimit(3) = %d, want %d", got, want)
	}
	if got, want := boundedTraceEnrichmentLimit(25), 100; got != want {
		t.Fatalf("boundedTraceEnrichmentLimit(25) = %d, want %d", got, want)
	}
}
