package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverStructuredTerragruntParentDirHelperConfigEvidence(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		ScopeID: "repo-live",
		Payload: map[string]any{
			"relative_path": "env/prod/terragrunt.hcl",
			"parsed_file_data": map[string]any{
				"terragrunt_configs": []any{
					map[string]any{
						"name":                     "terragrunt",
						"local_config_asset_paths": "../terraform-modules-aws/global.yaml,../terraform-modules-aws/templates/runtime.json",
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-terraform-modules-aws", Aliases: []string{"terraform-modules-aws"}},
	}

	evidence := DiscoverEvidence([]facts.Envelope{envelope}, catalog)
	if len(evidence) != 2 {
		t.Fatalf("len(evidence) = %d, want 2", len(evidence))
	}

	wantPaths := map[string]struct{}{
		"../terraform-modules-aws/global.yaml":            {},
		"../terraform-modules-aws/templates/runtime.json": {},
	}
	for _, item := range evidence {
		if item.RelationshipType != RelDiscoversConfigIn {
			t.Fatalf("RelationshipType = %q, want %q", item.RelationshipType, RelDiscoversConfigIn)
		}
		if item.EvidenceKind != EvidenceKindTerragruntConfigAssetPath {
			t.Fatalf("EvidenceKind = %q, want %q", item.EvidenceKind, EvidenceKindTerragruntConfigAssetPath)
		}
		if item.TargetRepoID != "repo-terraform-modules-aws" {
			t.Fatalf("TargetRepoID = %q, want repo-terraform-modules-aws", item.TargetRepoID)
		}
		configPath, _ := item.Details["config_path"].(string)
		if _, ok := wantPaths[configPath]; !ok {
			t.Fatalf("unexpected config_path %#v in details %#v", configPath, item.Details)
		}
		if helperKind, _ := item.Details["helper_kind"].(string); helperKind != "local_config_asset_path" {
			t.Fatalf("helper_kind = %#v, want %#v", helperKind, "local_config_asset_path")
		}
		delete(wantPaths, configPath)
	}
	if len(wantPaths) != 0 {
		t.Fatalf("missing evidence for %#v", wantPaths)
	}
}
