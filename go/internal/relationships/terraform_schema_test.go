// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestDiscoverEvidenceIncludesTypedPagerDutyTerraformEvidence(t *testing.T) {
	resetTerraformSchemaRegistryForTest()
	RegisterSchemaDrivenTerraformExtractors(defaultTerraformSchemaDir())

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "pagerduty.tf",
				"content": `resource "pagerduty_service" "checkout" {
  name = "checkout-service"
}

resource "pagerduty_escalation_policy" "checkout" {
  name = "checkout-escalation"
}

resource "pagerduty_team" "checkout" {
  name = "checkout-team"
}

resource "pagerduty_service_integration" "checkout" {
  name    = "checkout-events"
  service = pagerduty_service.checkout.id
}

resource "pagerduty_event_orchestration" "checkout" {
  name = "checkout-orchestration"
}

resource "pagerduty_webhook_subscription" "checkout-webhook" {
  type   = "webhook_subscription"
  active = true
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-service", Aliases: []string{"checkout-service"}},
		{RepoID: "repo-escalation", Aliases: []string{"checkout-escalation"}},
		{RepoID: "repo-team", Aliases: []string{"checkout-team"}},
		{RepoID: "repo-integration", Aliases: []string{"checkout-events"}},
		{RepoID: "repo-orchestration", Aliases: []string{"checkout-orchestration"}},
		{RepoID: "repo-webhook", Aliases: []string{"checkout-webhook"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	expected := map[EvidenceKind]struct {
		targetRepoID    string
		resourceType    string
		identityKey     string
		resourceService string
		category        string
	}{
		EvidenceKind("TERRAFORM_PAGERDUTY_SERVICE"): {
			targetRepoID: "repo-service", resourceType: "pagerduty_service", identityKey: "name",
			resourceService: "pagerduty_service", category: "monitoring",
		},
		EvidenceKind("TERRAFORM_PAGERDUTY_ESCALATION_POLICY"): {
			targetRepoID: "repo-escalation", resourceType: "pagerduty_escalation_policy", identityKey: "name",
			resourceService: "pagerduty_escalation_policy", category: "monitoring",
		},
		EvidenceKind("TERRAFORM_PAGERDUTY_TEAM"): {
			targetRepoID: "repo-team", resourceType: "pagerduty_team", identityKey: "name",
			resourceService: "pagerduty_team", category: "monitoring",
		},
		EvidenceKind("TERRAFORM_PAGERDUTY_SERVICE_INTEGRATION"): {
			targetRepoID: "repo-integration", resourceType: "pagerduty_service_integration", identityKey: "name",
			resourceService: "pagerduty_service_integration", category: "monitoring",
		},
		EvidenceKind("TERRAFORM_PAGERDUTY_EVENT_ORCHESTRATION"): {
			targetRepoID: "repo-orchestration", resourceType: "pagerduty_event_orchestration", identityKey: "name",
			resourceService: "pagerduty_event_orchestration", category: "monitoring",
		},
		EvidenceKind("TERRAFORM_PAGERDUTY_WEBHOOK_SUBSCRIPTION"): {
			targetRepoID: "repo-webhook", resourceType: "pagerduty_webhook_subscription", identityKey: "resource_name",
			resourceService: "pagerduty_webhook_subscription", category: "monitoring",
		},
	}
	if len(evidence) != len(expected) {
		t.Fatalf("len(evidence) = %d, want %d: %#v", len(evidence), len(expected), evidence)
	}

	for kind, want := range expected {
		got := evidenceFactByKind(t, evidence, kind)
		if got.TargetRepoID != want.targetRepoID {
			t.Fatalf("%s TargetRepoID = %q, want %q", kind, got.TargetRepoID, want.targetRepoID)
		}
		if got.RelationshipType != RelProvisionsDependencyFor {
			t.Fatalf("%s RelationshipType = %q, want %q", kind, got.RelationshipType, RelProvisionsDependencyFor)
		}
		if got.Details["resource_type"] != want.resourceType {
			t.Fatalf("%s resource_type = %#v, want %q", kind, got.Details["resource_type"], want.resourceType)
		}
		if got.Details["identity_key"] != want.identityKey {
			t.Fatalf("%s identity_key = %#v, want %q", kind, got.Details["identity_key"], want.identityKey)
		}
		if got.Details["resource_service"] != want.resourceService {
			t.Fatalf("%s resource_service = %#v, want %q", kind, got.Details["resource_service"], want.resourceService)
		}
		if got.Details["category"] != want.category {
			t.Fatalf("%s category = %#v, want %q", kind, got.Details["category"], want.category)
		}
	}
}

func TestDiscoverEvidencePagerDutyDoesNotUseSensitiveIdentityAttributes(t *testing.T) {
	resetTerraformSchemaRegistryForTest()
	RegisterSchemaDrivenTerraformExtractors(defaultTerraformSchemaDir())

	envelopes := []facts.Envelope{
		{
			ScopeID: "repo-infra",
			Payload: map[string]any{
				"artifact_type": "terraform",
				"relative_path": "pagerduty.tf",
				"content": `resource "pagerduty_service_integration" "main" {
  integration_key = "pd-secret-routing-key"
  service         = pagerduty_service.checkout.id
}

resource "pagerduty_team_membership" "main" {
  team_id = "PTEAM123"
  user_id = "PUSER456"
}`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-secret", Aliases: []string{"pd-secret-routing-key"}},
		{RepoID: "repo-user", Aliases: []string{"PUSER456"}},
		{RepoID: "repo-team", Aliases: []string{"PTEAM123"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len(evidence) = %d, want 0 so sensitive PagerDuty attributes stay out of candidates: %#v", len(evidence), evidence)
	}
}

func evidenceFactByKind(t *testing.T, evidence []EvidenceFact, kind EvidenceKind) EvidenceFact {
	t.Helper()
	for _, fact := range evidence {
		if fact.EvidenceKind == kind {
			return fact
		}
	}
	t.Fatalf("missing evidence kind %q in %#v", kind, evidence)
	return EvidenceFact{}
}
