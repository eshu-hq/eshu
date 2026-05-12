package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
)

func TestConfigRowFromParserEntryPopulatesAttributes(t *testing.T) {
	t.Parallel()

	entry := map[string]any{
		"resource_type": "aws_s3_bucket",
		"resource_name": "logs",
		"attributes": map[string]any{
			"acl":                "private",
			"versioning.enabled": "true",
		},
		"unknown_attributes": []any{"tags", "logging.target_bucket"},
	}
	row, ok := configRowFromParserEntry(entry, "")
	if !ok {
		t.Fatal("ok = false")
	}
	if got, want := row.Address, "aws_s3_bucket.logs"; got != want {
		t.Fatalf("Address = %q, want %q", got, want)
	}
	if got, want := row.Attributes["versioning.enabled"], "true"; got != want {
		t.Fatalf("Attributes[versioning.enabled] = %q, want %q", got, want)
	}
	if got, want := row.Attributes["acl"], "private"; got != want {
		t.Fatalf("Attributes[acl] = %q, want %q", got, want)
	}
	if !row.UnknownAttributes["tags"] || !row.UnknownAttributes["logging.target_bucket"] {
		t.Fatalf("UnknownAttributes = %v, want both 'tags' and 'logging.target_bucket'", row.UnknownAttributes)
	}
}

func TestConfigRowFromParserEntryRejectsBlankTypeOrName(t *testing.T) {
	t.Parallel()
	for _, entry := range []map[string]any{
		{"resource_type": "", "resource_name": "web"},
		{"resource_type": "aws_instance", "resource_name": ""},
		{},
	} {
		if _, ok := configRowFromParserEntry(entry, ""); ok {
			t.Fatalf("configRowFromParserEntry(%v) ok = true, want false", entry)
		}
	}
}

func TestConfigRowFromParserEntryAppliesModulePrefix(t *testing.T) {
	t.Parallel()

	entry := map[string]any{"resource_type": "aws_instance", "resource_name": "web"}
	row, ok := configRowFromParserEntry(entry, "module.vpc")
	if !ok {
		t.Fatal("ok = false")
	}
	if got, want := row.Address, "module.vpc.aws_instance.web"; got != want {
		t.Fatalf("Address = %q, want %q", got, want)
	}
}

func TestConfigRowFromParserEntryEmptyPrefixKeepsRootAddress(t *testing.T) {
	t.Parallel()

	entry := map[string]any{"resource_type": "aws_instance", "resource_name": "web"}
	row, ok := configRowFromParserEntry(entry, "")
	if !ok {
		t.Fatal("ok = false")
	}
	if got, want := row.Address, "aws_instance.web"; got != want {
		t.Fatalf("Address = %q, want %q (root-module byte-identical regression)", got, want)
	}
}

func TestConfigRowFromParserEntryHandlesMissingAttributeFields(t *testing.T) {
	t.Parallel()
	entry := map[string]any{"resource_type": "aws_s3_bucket", "resource_name": "empty"}
	row, ok := configRowFromParserEntry(entry, "")
	if !ok {
		t.Fatal("ok = false")
	}
	if row.Attributes != nil {
		t.Fatalf("Attributes = %v, want nil", row.Attributes)
	}
	if row.UnknownAttributes != nil {
		t.Fatalf("UnknownAttributes = %v, want nil", row.UnknownAttributes)
	}
}

func TestParserToClassifierEndToEndNestedAttributeDrift(t *testing.T) {
	t.Parallel()

	cfg, ok := configRowFromParserEntry(map[string]any{
		"resource_type": "aws_s3_bucket",
		"resource_name": "logs",
		"attributes": map[string]any{
			"server_side_encryption_configuration.rule.apply_server_side_encryption_by_default.sse_algorithm": "AES256",
		},
	}, "")
	if !ok {
		t.Fatal("cfg ok = false")
	}

	stateBytes := []byte(`{
		"address": "aws_s3_bucket.logs",
		"type": "aws_s3_bucket",
		"attributes": {
			"server_side_encryption_configuration": [
				{"rule": [{"apply_server_side_encryption_by_default": [{"sse_algorithm": "aws:kms"}]}]}
			]
		}
	}`)
	state, ok := stateRowFromCollectorPayload(context.Background(), nil, "aws_s3_bucket.logs", stateBytes, false)
	if !ok {
		t.Fatal("state ok = false")
	}

	got := tfconfigstate.Classify(cfg, state, nil)
	if got != tfconfigstate.DriftKindAttributeDrift {
		t.Fatalf("Classify(...) = %q, want %q", got, tfconfigstate.DriftKindAttributeDrift)
	}
}

func TestParserToClassifierEndToEndUnknownAttributeSuppressesDrift(t *testing.T) {
	t.Parallel()

	cfg, ok := configRowFromParserEntry(map[string]any{
		"resource_type":      "aws_s3_bucket",
		"resource_name":      "logs",
		"unknown_attributes": []any{"versioning.enabled"},
	}, "")
	if !ok {
		t.Fatal("cfg ok = false")
	}
	state, ok := stateRowFromCollectorPayload(context.Background(), nil, "aws_s3_bucket.logs",
		[]byte(`{"address":"aws_s3_bucket.logs","type":"aws_s3_bucket","attributes":{"versioning":[{"enabled":false}]}}`),
		false)
	if !ok {
		t.Fatal("state ok = false")
	}
	if got := tfconfigstate.Classify(cfg, state, nil); got != "" {
		t.Fatalf("Classify(...) = %q, want empty (unknown must suppress)", got)
	}
}
