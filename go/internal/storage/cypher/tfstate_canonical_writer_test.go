package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterBuildsTerraformStateStatements(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "tf-scope-1",
		GenerationID: "tf-generation-1",
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:                "tf-resource-uid-1",
			Address:            "module.app.aws_instance.web",
			Mode:               "managed",
			ResourceType:       "aws_instance",
			Name:               "web",
			ModuleAddress:      "module.app",
			ProviderAddress:    "provider[\"registry.terraform.io/hashicorp/aws\"]",
			Lineage:            "lineage-123",
			Serial:             17,
			BackendKind:        "s3",
			LocatorHash:        "locator-hash-1",
			StatePath:          "tfstate://s3/locator-hash-1",
			SourceFactID:       "tf-resource-1",
			StableFactKey:      "terraform_state_resource:resource:module.app.aws_instance.web",
			SourceSystem:       "terraform_state",
			SourceRecordID:     "module.app.aws_instance.web",
			SourceConfidence:   facts.SourceConfidenceObserved,
			CollectorKind:      "terraform_state",
			CorrelationAnchors: []string{"arn:anchor-hash-1"},
			TagKeyHashes:       []string{"tag-key-hash-1"},
		}},
		TerraformStateModules: []projector.TerraformStateModuleRow{{
			UID:              "tf-module-uid-1",
			ModuleAddress:    "module.app",
			ResourceCount:    1,
			Lineage:          "lineage-123",
			Serial:           17,
			BackendKind:      "s3",
			LocatorHash:      "locator-hash-1",
			StatePath:        "tfstate://s3/locator-hash-1",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
		}},
		TerraformStateOutputs: []projector.TerraformStateOutputRow{{
			UID:              "tf-output-uid-1",
			Name:             "web_instance_id",
			Sensitive:        true,
			ValueShape:       "redacted_scalar",
			Lineage:          "lineage-123",
			Serial:           17,
			BackendKind:      "s3",
			LocatorHash:      "locator-hash-1",
			StatePath:        "tfstate://s3/locator-hash-1",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
		}},
	}

	statements := writer.buildTerraformStateStatements(mat)
	if got, want := len(statements), 3; got != want {
		t.Fatalf("buildTerraformStateStatements() count = %d, want %d", got, want)
	}

	resource := statements[0]
	if !strings.Contains(resource.Cypher, "MERGE (r:TerraformResource {uid: row.uid})") {
		t.Fatalf("resource Cypher = %q, want TerraformResource uid merge", resource.Cypher)
	}
	rows := resource.Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["provider_address"], "provider[\"registry.terraform.io/hashicorp/aws\"]"; got != want {
		t.Fatalf("provider_address = %q, want %q", got, want)
	}
	if got, want := rows[0]["source_confidence"], facts.SourceConfidenceObserved; got != want {
		t.Fatalf("source_confidence = %q, want %q", got, want)
	}
	if got, want := rows[0]["correlation_anchors"].([]string)[0], "arn:anchor-hash-1"; got != want {
		t.Fatalf("correlation_anchors[0] = %q, want %q", got, want)
	}
	if got, want := rows[0]["tag_key_hashes"].([]string)[0], "tag-key-hash-1"; got != want {
		t.Fatalf("tag_key_hashes[0] = %q, want %q", got, want)
	}

	if !strings.Contains(statements[1].Cypher, "MERGE (m:TerraformModule {uid: row.uid})") {
		t.Fatalf("module Cypher = %q, want TerraformModule uid merge", statements[1].Cypher)
	}
	if !strings.Contains(statements[2].Cypher, "MERGE (o:TerraformOutput {uid: row.uid})") {
		t.Fatalf("output Cypher = %q, want TerraformOutput uid merge", statements[2].Cypher)
	}
}
