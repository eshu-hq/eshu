package terraformstate_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserEmitsTerraformStateProviderBindingFacts(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[
		{
			"module":"module.api",
			"mode":"managed",
			"type":"aws_instance",
			"name":"web",
			"provider":"provider[\"registry.terraform.io/hashicorp/aws\"].west",
			"instances":[{"attributes":{"id":"i-1"}}]
		},
		{
			"mode":"data",
			"type":"aws_ami",
			"name":"ubuntu",
			"provider":"provider[\"registry.terraform.io/hashicorp/aws\"]",
			"instances":[{"attributes":{"id":"ami-1"}}]
		}
	]}`

	result := parseFixtureFacts(t, state)

	requireFactKinds(t, result, facts.TerraformStateProviderBindingFactKind)
	bindings := factsByKind(result, facts.TerraformStateProviderBindingFactKind)
	if got, want := len(bindings), 2; got != want {
		t.Fatalf("provider binding fact count = %d, want %d: %#v", got, want, bindings)
	}

	binding := factByPayloadValue(t, bindings, "resource_address", "module.api.aws_instance.web")
	if got, want := binding.Payload["provider_address"], "provider[\"registry.terraform.io/hashicorp/aws\"].west"; got != want {
		t.Fatalf("provider_address = %#v, want %q", got, want)
	}
	if got, want := binding.Payload["provider_source_address"], "registry.terraform.io/hashicorp/aws"; got != want {
		t.Fatalf("provider_source_address = %#v, want %q", got, want)
	}
	if got, want := binding.Payload["provider_namespace"], "hashicorp"; got != want {
		t.Fatalf("provider_namespace = %#v, want %q", got, want)
	}
	if got, want := binding.Payload["provider_type"], "aws"; got != want {
		t.Fatalf("provider_type = %#v, want %q", got, want)
	}
	if got, want := binding.Payload["provider_alias"], "west"; got != want {
		t.Fatalf("provider_alias = %#v, want %q", got, want)
	}
	if strings.Contains(binding.StableFactKey, "registry.terraform.io/hashicorp/aws") ||
		strings.Contains(binding.SourceRef.SourceRecordID, "registry.terraform.io/hashicorp/aws") {
		t.Fatalf("provider identity leaked provider address: stable_key=%q source_ref=%#v", binding.StableFactKey, binding.SourceRef)
	}
}

func TestParserProviderBindingFactKeysAreStableAcrossResourceOrder(t *testing.T) {
	t.Parallel()

	first := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"api","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"id":"i-1"}}]},
		{"mode":"managed","type":"google_project","name":"main","provider":"provider[\"registry.terraform.io/hashicorp/google\"]","instances":[{"attributes":{"id":"p-1"}}]}
	]}`
	second := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"google_project","name":"main","provider":"provider[\"registry.terraform.io/hashicorp/google\"]","instances":[{"attributes":{"id":"p-1"}}]},
		{"mode":"managed","type":"aws_instance","name":"api","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"id":"i-1"}}]}
	]}`

	firstFacts := parseFixtureFacts(t, first)
	secondFacts := parseFixtureFacts(t, second)

	if got, want := stableKeysByKind(firstFacts, facts.TerraformStateProviderBindingFactKind), stableKeysByKind(secondFacts, facts.TerraformStateProviderBindingFactKind); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("provider binding stable keys changed with order:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestParserDeduplicatesTerraformStateProviderBindingFacts(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[
		{
			"mode":"managed",
			"type":"aws_instance",
			"name":"web",
			"provider":"provider[\"registry.terraform.io/hashicorp/aws\"]",
			"instances":[{"attributes":{"id":"i-1"}}]
		},
		{
			"mode":"managed",
			"type":"aws_instance",
			"name":"web",
			"provider":"provider[\"registry.terraform.io/hashicorp/aws\"]",
			"instances":[{"attributes":{"id":"i-1"}}]
		}
	]}`

	result := parseFixtureFacts(t, state)
	bindings := factsByKind(result, facts.TerraformStateProviderBindingFactKind)
	if got, want := len(bindings), 1; got != want {
		t.Fatalf("provider binding fact count = %d, want %d: %#v", got, want, bindings)
	}
}

func factByPayloadValue(t *testing.T, envelopes []facts.Envelope, key string, value any) facts.Envelope {
	t.Helper()

	for _, envelope := range envelopes {
		if envelope.Payload[key] == value {
			return envelope
		}
	}
	t.Fatalf("missing payload %s=%#v in %#v", key, value, envelopes)
	return facts.Envelope{}
}
