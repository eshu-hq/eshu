package relationships

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func fixtureSchemaDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "tests", "fixtures", "schemas")
}

func TestRegisterSchemaDrivenTerraformExtractorsRegistersFixtureTypes(t *testing.T) {
	resetTerraformSchemaRegistryForTest()

	summary := RegisterSchemaDrivenTerraformExtractors(fixtureSchemaDir(t))
	if got := summary["aws"]; got == 0 {
		t.Fatalf("summary[aws] = %d, want > 0", got)
	}

	extractors := getTerraformResourceExtractors("aws_wafv2_web_acl")
	if len(extractors) == 0 {
		t.Fatal("expected aws_wafv2_web_acl extractor to be registered")
	}
}

func TestRegisterSchemaDrivenTerraformExtractorsRegistersMultipleProviders(t *testing.T) {
	resetTerraformSchemaRegistryForTest()

	dir := t.TempDir()
	path := filepath.Join(dir, "multi-provider-schema.json")
	content := []byte(`{
  "format_version": "1.0",
  "provider_schemas": {
    "registry.terraform.io/hashicorp/google": {
      "resource_schemas": {
        "google_storage_bucket": {
          "block": {
            "attributes": {
              "name": {"type": "string"}
            }
          }
        }
      }
    },
    "registry.terraform.io/hashicorp/aws": {
      "resource_schemas": {
        "aws_s3_bucket": {
          "block": {
            "attributes": {
              "bucket": {"type": "string"}
            }
          }
        }
      }
    }
  }
}`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	summary := RegisterSchemaDrivenTerraformExtractors(dir)
	if got := summary["aws"]; got == 0 {
		t.Fatalf("summary[aws] = %d, want > 0", got)
	}
	if got := summary["google"]; got == 0 {
		t.Fatalf("summary[google] = %d, want > 0", got)
	}

	if extractors := getTerraformResourceExtractors("aws_s3_bucket"); len(extractors) == 0 {
		t.Fatal("expected aws_s3_bucket extractor to be registered")
	}
	if extractors := getTerraformResourceExtractors("google_storage_bucket"); len(extractors) == 0 {
		t.Fatal("expected google_storage_bucket extractor to be registered")
	}
}

func TestRegisterSchemaDrivenTerraformExtractorsIsIdempotent(t *testing.T) {
	resetTerraformSchemaRegistryForTest()

	first := RegisterSchemaDrivenTerraformExtractors(fixtureSchemaDir(t))
	second := RegisterSchemaDrivenTerraformExtractors(fixtureSchemaDir(t))

	if got := first["aws"]; got == 0 {
		t.Fatalf("first[aws] = %d, want > 0", got)
	}
	if got := second["aws"]; got != 0 {
		t.Fatalf("second[aws] = %d, want 0 after first registration", got)
	}
}

func TestDiscoverEvidenceIncludesSchemaDrivenTerraformEvidence(t *testing.T) {
	resetTerraformSchemaRegistryForTest()
	RegisterSchemaDrivenTerraformExtractors(fixtureSchemaDir(t))

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "main.tf",
				"content": `resource "aws_wafv2_web_acl" "edge" {
  name  = "prod-waf-acl"
  scope = "REGIONAL"
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-edge", Aliases: []string{"prod-waf-acl"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1", len(evidence))
	}
	if got, want := evidence[0].EvidenceKind, EvidenceKind("TERRAFORM_WAFV2_WEB_ACL"); got != want {
		t.Fatalf("EvidenceKind = %q, want %q", got, want)
	}
	if got, want := evidence[0].TargetRepoID, "repo-edge"; got != want {
		t.Fatalf("TargetRepoID = %q, want %q", got, want)
	}
	if got, want := evidence[0].Details["schema_driven"], true; got != want {
		t.Fatalf("schema_driven detail = %#v, want %#v", got, want)
	}
}

func TestDiscoverEvidenceSchemaDrivenFallbackUsesResourceName(t *testing.T) {
	resetTerraformSchemaRegistryForTest()
	RegisterSchemaDrivenTerraformExtractors(fixtureSchemaDir(t))

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "service.tf",
				"content": `resource "aws_apprunner_service" "my-api-service" {
  auto_deployments_enabled = true
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-api", Aliases: []string{"my-api-service"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 1 {
		t.Fatalf("len(evidence) = %d, want 1", len(evidence))
	}
	if got, want := evidence[0].Details["identity_key"], "resource_name"; got != want {
		t.Fatalf("identity_key = %#v, want %#v", got, want)
	}
}

func TestDiscoverEvidenceSchemaDrivenSkipsGenericResourceName(t *testing.T) {
	resetTerraformSchemaRegistryForTest()
	RegisterSchemaDrivenTerraformExtractors(fixtureSchemaDir(t))

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "network.tf",
				"content": `resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-network", Aliases: []string{"main"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len(evidence) = %d, want 0", len(evidence))
	}
}
