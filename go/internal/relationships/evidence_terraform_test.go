package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDiscoverTerraformLocalModuleSourceEvidence(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-fsbo",
			Payload: map[string]any{
				"artifact_type": "terraform_hcl",
				"relative_path": "environments/qa/prod_payment_keys.tf",
				"content": `module "api_key_default" {
  source = "../../local-modules/payment-api"
}
`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payment-api", Aliases: []string{"payment-api"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1", len(evidence))
	}
	if got, want := evidence[0].EvidenceKind, EvidenceKindTerraformModuleSource; got != want {
		t.Fatalf("EvidenceKind = %q, want %q", got, want)
	}
	if got, want := evidence[0].RelationshipType, RelUsesModule; got != want {
		t.Fatalf("RelationshipType = %q, want %q", got, want)
	}
	if got, want := evidence[0].TargetRepoID, "repo-payment-api"; got != want {
		t.Fatalf("TargetRepoID = %q, want %q", got, want)
	}
	if got, want := evidence[0].Details["source_ref"], "../../local-modules/payment-api"; got != want {
		t.Fatalf("source_ref = %#v, want %#v", got, want)
	}
}

func TestDiscoverTerraformRegistryMonorepoModuleSourceEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		artifactType string
		relativePath string
		content      string
	}{
		{
			name:         "terraform module source maps to aws monorepo",
			artifactType: "terraform_hcl",
			relativePath: "shared/pending-provisioning.tf",
			content: `module "queue" {
  source = "packages.example.test/terraform/lambda-function/aws"
}
`,
		},
		{
			name:         "terragrunt terraform source maps to aws monorepo",
			artifactType: "terragrunt",
			relativePath: "env/dev/terragrunt.hcl",
			content: `terraform {
  source = "packages.example.test/terraform/ecs-application/aws"
}
`,
		},
	}

	catalog := []CatalogEntry{
		{RepoID: "repo-terraform-modules-aws", Aliases: []string{"terraform-modules-aws"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				{
					ScopeID: "repo-stack",
					Payload: map[string]any{
						"artifact_type": tt.artifactType,
						"relative_path": tt.relativePath,
						"content":       tt.content,
					},
				},
			}

			evidence := DiscoverEvidence(envelopes, catalog)
			if len(evidence) != 1 {
				t.Fatalf("len(evidence) = %d, want 1", len(evidence))
			}
			if got, want := evidence[0].EvidenceKind, EvidenceKindTerraformModuleSource; got != want {
				t.Fatalf("EvidenceKind = %q, want %q", got, want)
			}
			if got, want := evidence[0].RelationshipType, RelUsesModule; got != want {
				t.Fatalf("RelationshipType = %q, want %q", got, want)
			}
			if got, want := evidence[0].TargetRepoID, "repo-terraform-modules-aws"; got != want {
				t.Fatalf("TargetRepoID = %q, want %q", got, want)
			}
		})
	}
}

func TestDiscoverTerraformAndTerragruntHelperBuiltPathEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		artifactType string
		relativePath string
		parsedData   map[string]any
		catalog      []CatalogEntry
		wantKind     EvidenceKind
		wantTarget   string
	}{
		{
			name:         "helper-built terraform source maps to module repository",
			artifactType: "terraform_hcl",
			relativePath: "shared/pending-provisioning.tf",
			parsedData: map[string]any{
				"terraform_modules": []any{
					map[string]any{
						"name":   "queue",
						"source": `join("/", [get_repo_root(), "local-modules/payments-api"])`,
					},
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-payments-api", Aliases: []string{"payments-api"}},
			},
			wantKind:   EvidenceKindTerraformModuleSource,
			wantTarget: "repo-payments-api",
		},
		{
			name:         "helper-built terragrunt dependency config path maps to target repository",
			artifactType: "terragrunt",
			relativePath: "env/dev/terragrunt.hcl",
			parsedData: map[string]any{
				"terragrunt_dependencies": []any{
					map[string]any{
						"name":        "payments",
						"config_path": `join("/", [get_repo_root(), "payments-service"])`,
					},
				},
			},
			catalog: []CatalogEntry{
				{RepoID: "repo-payments", Aliases: []string{"payments-service"}},
			},
			wantKind:   EvidenceKindTerragruntDependencyConfigPath,
			wantTarget: "repo-payments",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			evidence := DiscoverEvidence([]facts.Envelope{
				{
					ScopeID: "repo-stack",
					Payload: map[string]any{
						"artifact_type":    tt.artifactType,
						"relative_path":    tt.relativePath,
						"parsed_file_data": tt.parsedData,
					},
				},
			}, tt.catalog)
			if len(evidence) != 1 {
				t.Fatalf("len(evidence) = %d, want 1", len(evidence))
			}
			if got, want := evidence[0].EvidenceKind, tt.wantKind; got != want {
				t.Fatalf("EvidenceKind = %q, want %q", got, want)
			}
			if got, want := evidence[0].TargetRepoID, tt.wantTarget; got != want {
				t.Fatalf("TargetRepoID = %q, want %q", got, want)
			}
			if got, ok := evidence[0].Details["first_party_ref_kind"].(string); !ok || got == "" {
				t.Fatalf("first_party_ref_kind = %#v, want non-empty string", evidence[0].Details["first_party_ref_kind"])
			}
			if got, ok := evidence[0].Details["first_party_ref_normalized"].(string); !ok || got == "" {
				t.Fatalf("first_party_ref_normalized = %#v, want non-empty string", evidence[0].Details["first_party_ref_normalized"])
			}
		})
	}
}
