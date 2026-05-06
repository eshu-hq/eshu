package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverStructuredTerraformAndTerragruntEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		envelope       facts.Envelope
		catalog        []CatalogEntry
		wantKind       EvidenceKind
		wantCount      int
		wantNoEvidence bool
	}{
		{
			name: "terraform module source from remote repo reference",
			envelope: facts.Envelope{
				ScopeID: "repo-infra",
				Payload: map[string]any{
					"relative_path": "main.tf",
					"parsed_file_data": map[string]any{
						"terraform_modules": []any{
							map[string]any{
								"name":   "service",
								"source": "git::https://github.com/myorg/payments-service.git//modules/service?ref=v1.2.3",
							},
						},
					},
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-payments", Aliases: []string{"myorg/payments-service", "payments-service"}},
			},
			wantKind:  EvidenceKindTerraformModuleSource,
			wantCount: 1,
		},
		{
			name: "terragrunt terraform source from local relative reference",
			envelope: facts.Envelope{
				ScopeID: "repo-gitops",
				Payload: map[string]any{
					"relative_path": "terragrunt.hcl",
					"parsed_file_data": map[string]any{
						"terraform_modules": []any{
							map[string]any{
								"name":   "app",
								"source": "../payments-service/modules/app",
							},
						},
					},
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
			},
			wantKind:  EvidenceKindTerraformModuleSource,
			wantCount: 1,
		},
		{
			name: "terragrunt dependency config path from local relative reference",
			envelope: facts.Envelope{
				ScopeID: "repo-gitops",
				Payload: map[string]any{
					"relative_path": "terragrunt.hcl",
					"parsed_file_data": map[string]any{
						"terragrunt_dependencies": []any{
							map[string]any{
								"name":        "payments",
								"config_path": "../payments-service",
							},
						},
					},
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
			},
			wantKind:  EvidenceKindTerragruntDependencyConfigPath,
			wantCount: 1,
		},
		{
			name: "ignores ambiguous registry-style module source",
			envelope: facts.Envelope{
				ScopeID: "repo-infra",
				Payload: map[string]any{
					"relative_path": "main.tf",
					"parsed_file_data": map[string]any{
						"terraform_modules": []any{
							map[string]any{
								"name":   "eks",
								"source": "terraform-aws-modules/eks/aws",
							},
						},
					},
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-cluster", Aliases: []string{"eks"}},
			},
			wantNoEvidence: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evidence := DiscoverEvidence([]facts.Envelope{tt.envelope}, tt.catalog)
			if tt.wantNoEvidence {
				if len(evidence) != 0 {
					t.Fatalf("len(evidence) = %d, want 0", len(evidence))
				}
				return
			}

			if len(evidence) != tt.wantCount {
				t.Fatalf("len(evidence) = %d, want %d", len(evidence), tt.wantCount)
			}
			if evidence[0].EvidenceKind != tt.wantKind {
				t.Fatalf("EvidenceKind = %q, want %q", evidence[0].EvidenceKind, tt.wantKind)
			}
			if tt.wantKind == EvidenceKindTerragruntDependencyConfigPath {
				if evidence[0].RelationshipType != RelDiscoversConfigIn {
					t.Fatalf("RelationshipType = %q, want %q", evidence[0].RelationshipType, RelDiscoversConfigIn)
				}
			} else if evidence[0].RelationshipType != RelUsesModule {
				t.Fatalf("RelationshipType = %q, want %q", evidence[0].RelationshipType, RelUsesModule)
			}
			if evidence[0].TargetRepoID != "repo-payments" {
				t.Fatalf("TargetRepoID = %q, want repo-payments", evidence[0].TargetRepoID)
			}
		})
	}
}

func TestDiscoverStructuredTerragruntHelperConfigEvidence(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{
		ScopeID: "repo-live",
		Payload: map[string]any{
			"relative_path": "env/prod/terragrunt.hcl",
			"parsed_file_data": map[string]any{
				"terragrunt_configs": []any{
					map[string]any{
						"name":                         "terragrunt",
						"include_paths":                "../iac-eks-terragrunt-core/root.hcl",
						"read_config_paths":            "../iac-eks-terragrunt-core/env.hcl",
						"find_in_parent_folders_paths": "../iac-eks-terragrunt-core/global.yaml",
						"local_config_asset_paths":     "../terraform-modules-aws/config/runtime.yaml",
					},
				},
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-terragrunt-core", Aliases: []string{"iac-eks-terragrunt-core"}},
		{RepoID: "repo-terraform-modules-aws", Aliases: []string{"terraform-modules-aws"}},
	}

	evidence := DiscoverEvidence([]facts.Envelope{envelope}, catalog)
	if len(evidence) != 4 {
		t.Fatalf("len(evidence) = %d, want 4", len(evidence))
	}

	want := map[string]struct {
		targetRepoID string
	}{
		"include_path": {
			targetRepoID: "repo-terragrunt-core",
		},
		"read_config_path": {
			targetRepoID: "repo-terragrunt-core",
		},
		"find_in_parent_folders_path": {
			targetRepoID: "repo-terragrunt-core",
		},
		"local_config_asset_path": {
			targetRepoID: "repo-terraform-modules-aws",
		},
	}
	for _, item := range evidence {
		if item.RelationshipType != RelDiscoversConfigIn {
			t.Fatalf("RelationshipType = %q, want %q", item.RelationshipType, RelDiscoversConfigIn)
		}
		if item.EvidenceKind != EvidenceKindTerragruntConfigAssetPath {
			t.Fatalf("EvidenceKind = %q, want %q", item.EvidenceKind, EvidenceKindTerragruntConfigAssetPath)
		}
		helperKind, _ := item.Details["helper_kind"].(string)
		expected, ok := want[helperKind]
		if !ok {
			t.Fatalf("unexpected helper_kind %q in details %#v", helperKind, item.Details)
		}
		if item.TargetRepoID != expected.targetRepoID {
			t.Fatalf("TargetRepoID for %q = %q, want %q", helperKind, item.TargetRepoID, expected.targetRepoID)
		}
		if got, wantPath := item.Details["config_path"], item.Details["matched_value"]; got != wantPath {
			t.Fatalf("config_path %#v does not match matched_value %#v", got, wantPath)
		}
	}
}
