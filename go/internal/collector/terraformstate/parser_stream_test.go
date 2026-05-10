package terraformstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParseStreamPreservesParseFactOrder(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","outputs":{"plain":{"value":"ok"}},"resources":[{
		"module":"module.api",
		"mode":"managed",
		"type":"aws_instance",
		"name":"api",
		"provider":"provider[\"registry.terraform.io/hashicorp/aws\"]",
		"instances":[{"attributes":{"id":"i-1","name":"api"}}]
	}]}`
	options := parseFixtureOptions(t)
	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	streamed := []facts.Envelope{}
	streamResult, err := terraformstate.ParseStream(
		context.Background(),
		strings.NewReader(state),
		options,
		terraformstate.FactSinkFunc(func(_ context.Context, envelope facts.Envelope) error {
			streamed = append(streamed, envelope)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("ParseStream() error = %v, want nil", err)
	}
	if got, want := streamResult.ResourceFacts, result.ResourceFacts; got != want {
		t.Fatalf("ParseStream() ResourceFacts = %d, want %d", got, want)
	}
	if got, want := len(streamed), len(result.Facts); got != want {
		t.Fatalf("ParseStream() emitted %d facts, want %d", got, want)
	}
	for i := range result.Facts {
		if got, want := streamed[i].StableFactKey, result.Facts[i].StableFactKey; got != want {
			t.Fatalf("streamed fact %d key = %q, want %q", i, got, want)
		}
	}
}

func TestParseStreamFactOrderContract(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","outputs":{"plain":{"value":"ok"}},"resources":[{
		"module":"module.api",
		"mode":"managed",
		"type":"aws_instance",
		"name":"api",
		"provider":"provider[\"registry.terraform.io/hashicorp/aws\"]",
		"instances":[{"attributes":{"id":"i-1","name":"api"}}]
	}]}`
	options := parseFixtureOptions(t)
	options.SourceWarnings = []terraformstate.SourceWarning{{
		WarningKind: "state_in_vcs",
		Reason:      "terraform state file was discovered in git and explicitly approved for ingestion",
		Source:      "git_local_file",
	}}

	var streamed []facts.Envelope
	_, err := terraformstate.ParseStream(
		context.Background(),
		strings.NewReader(state),
		options,
		terraformstate.FactSinkFunc(func(_ context.Context, envelope facts.Envelope) error {
			streamed = append(streamed, envelope)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("ParseStream() error = %v, want nil", err)
	}

	got := make([]string, 0, len(streamed))
	for _, envelope := range streamed {
		got = append(got, envelope.FactKind+":"+envelope.StableFactKey)
	}
	want := []string{
		facts.TerraformStateSnapshotFactKind + ":terraform_state_snapshot:snapshot",
		facts.TerraformStateWarningFactKind + ":terraform_state_warning:warning:state_in_vcs:git_local_file:terraform state file was discovered in git and explicitly approved for ingestion",
		facts.TerraformStateOutputFactKind + ":terraform_state_output:output:plain",
		facts.TerraformStateModuleFactKind + ":terraform_state_module:module:module.api:resource:module.api.aws_instance.api",
		facts.TerraformStateProviderBindingFactKind + ":terraform_state_provider_binding:provider_binding:module.api.aws_instance.api:" + providerAddressStableID(t, `provider["registry.terraform.io/hashicorp/aws"]`),
		facts.TerraformStateResourceFactKind + ":terraform_state_resource:resource:module.api.aws_instance.api",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ParseStream fact order:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func providerAddressStableID(t *testing.T, providerAddress string) string {
	t.Helper()
	return facts.StableID("TerraformStateProviderAddress", map[string]any{
		"provider_address": providerAddress,
	})
}
