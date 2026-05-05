package query

import (
	"context"
	"testing"
)

func TestBuildContentRelationshipSetTerragruntConfigPromotesHelperPaths(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:   "tg-config-helpers-1",
		RepoID:     "repo-1",
		EntityType: "TerragruntConfig",
		EntityName: "terragrunt",
		Metadata: map[string]any{
			"include_paths":                "root.hcl",
			"read_config_paths":            "env.hcl",
			"find_in_parent_folders_paths": "env.hcl,root.hcl",
			"local_config_asset_paths":     "config/runtime.yaml,templates/runtime.json",
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	want := []map[string]string{
		{
			"type":        "DISCOVERS_CONFIG_IN",
			"target_name": "root.hcl",
			"reason":      "terragrunt_include_path",
		},
		{
			"type":        "DISCOVERS_CONFIG_IN",
			"target_name": "env.hcl",
			"reason":      "terragrunt_read_config",
		},
		{
			"type":        "DISCOVERS_CONFIG_IN",
			"target_name": "env.hcl",
			"reason":      "terragrunt_find_in_parent_folders",
		},
		{
			"type":        "DISCOVERS_CONFIG_IN",
			"target_name": "root.hcl",
			"reason":      "terragrunt_find_in_parent_folders",
		},
		{
			"type":        "DISCOVERS_CONFIG_IN",
			"target_name": "config/runtime.yaml",
			"reason":      "local_config_asset",
		},
		{
			"type":        "DISCOVERS_CONFIG_IN",
			"target_name": "templates/runtime.json",
			"reason":      "local_config_asset",
		},
	}

	if len(relationships.outgoing) != len(want) {
		t.Fatalf("len(relationships.outgoing) = %d, want %d", len(relationships.outgoing), len(want))
	}

	gotByKey := make(map[string]map[string]any, len(relationships.outgoing))
	for _, relationship := range relationships.outgoing {
		key := relationship["type"].(string) + "|" + relationship["target_name"].(string) + "|" + relationship["reason"].(string)
		gotByKey[key] = relationship
	}

	for _, expected := range want {
		key := expected["type"] + "|" + expected["target_name"] + "|" + expected["reason"]
		if _, ok := gotByKey[key]; !ok {
			t.Fatalf("missing relationship %q in %#v", key, relationships.outgoing)
		}
	}
}

func TestBuildContentRelationshipSetTerragruntDependencyPromotesConfigDiscovery(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:   "tg-dependency-1",
		RepoID:     "repo-1",
		EntityType: "TerragruntDependency",
		EntityName: "payments",
		Metadata: map[string]any{
			"config_path": "../payments-service",
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	got := relationships.outgoing[0]
	if got["type"] != "DISCOVERS_CONFIG_IN" {
		t.Fatalf("relationship type = %v, want DISCOVERS_CONFIG_IN", got["type"])
	}
	if got["target_name"] != "../payments-service" {
		t.Fatalf("target_name = %v, want ../payments-service", got["target_name"])
	}
	if got["reason"] != "terragrunt_dependency_config_path" {
		t.Fatalf("reason = %v, want terragrunt_dependency_config_path", got["reason"])
	}
}

func TestBuildContentRelationshipSetNormalizesHelperBuiltTerraformSource(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:   "tg-config-helpers-2",
		RepoID:     "repo-1",
		EntityType: "TerragruntConfig",
		EntityName: "terragrunt",
		Metadata: map[string]any{
			"terraform_source": `join("/", [get_repo_root(), "modules/app"])`,
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	got := relationships.outgoing[0]
	if got["type"] != "USES_MODULE" {
		t.Fatalf("relationship type = %v, want USES_MODULE", got["type"])
	}
	if got["target_name"] != "modules/app" {
		t.Fatalf("target_name = %v, want modules/app", got["target_name"])
	}
	if got["reason"] != "terragrunt_terraform_source" {
		t.Fatalf("reason = %v, want terragrunt_terraform_source", got["reason"])
	}
}

func TestBuildContentRelationshipSetNormalizesHelperBuiltDependencyConfigPath(t *testing.T) {
	t.Parallel()

	relationships, err := buildContentRelationshipSet(context.Background(), nil, EntityContent{
		EntityID:   "tg-dependency-2",
		RepoID:     "repo-1",
		EntityType: "TerragruntDependency",
		EntityName: "network",
		Metadata: map[string]any{
			"config_path": `join("/", [get_repo_root(), "network/root.hcl"])`,
		},
	})
	if err != nil {
		t.Fatalf("buildContentRelationshipSet() error = %v, want nil", err)
	}

	if len(relationships.outgoing) != 1 {
		t.Fatalf("len(relationships.outgoing) = %d, want 1", len(relationships.outgoing))
	}

	got := relationships.outgoing[0]
	if got["type"] != "DISCOVERS_CONFIG_IN" {
		t.Fatalf("relationship type = %v, want DISCOVERS_CONFIG_IN", got["type"])
	}
	if got["target_name"] != "network/root.hcl" {
		t.Fatalf("target_name = %v, want network/root.hcl", got["target_name"])
	}
	if got["reason"] != "terragrunt_dependency_config_path" {
		t.Fatalf("reason = %v, want terragrunt_dependency_config_path", got["reason"])
	}
}
