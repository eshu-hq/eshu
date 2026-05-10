package terraformstate_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserEmitsTerraformStateModuleFacts(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"root","instances":[{"attributes":{"id":"i-root"}}]},
		{"module":"module.api","mode":"managed","type":"aws_instance","name":"web","instances":[{"attributes":{"id":"i-1"}}]},
		{"module":"module.api","mode":"managed","type":"aws_security_group","name":"web","instances":[{"attributes":{"id":"sg-1"}}]},
		{"module":"module.worker","mode":"managed","type":"aws_instance","name":"worker","instances":[{"attributes":{"id":"i-2"}}]}
	]}`

	result := parseFixtureFacts(t, state)

	requireFactKinds(t, result, facts.TerraformStateModuleFactKind)
	modules := factsByKind(result, facts.TerraformStateModuleFactKind)
	if got, want := len(modules), 3; got != want {
		t.Fatalf("module fact count = %d, want %d: %#v", got, want, modules)
	}

	api := factByStableKey(t, modules, "terraform_state_module:module:module.api:resource:module.api.aws_instance.web")
	if got, want := api.Payload["module_address"], "module.api"; got != want {
		t.Fatalf("api module_address = %#v, want %q", got, want)
	}
	if got, want := api.Payload["resource_count"], int64(1); got != want {
		t.Fatalf("api resource_count = %#v, want %d", got, want)
	}
	if got, want := api.SchemaVersion, facts.TerraformStateModuleSchemaVersion; got != want {
		t.Fatalf("api SchemaVersion = %q, want %q", got, want)
	}
	if strings.Contains(api.SourceRef.SourceURI, "s3://tfstate-prod/services/api/terraform.tfstate") ||
		strings.Contains(api.SourceRef.SourceRecordID, "s3://tfstate-prod/services/api/terraform.tfstate") {
		t.Fatalf("module source ref leaked raw locator: %#v", api.SourceRef)
	}

	worker := factByStableKey(t, modules, "terraform_state_module:module:module.worker:resource:module.worker.aws_instance.worker")
	if got, want := worker.Payload["resource_count"], int64(1); got != want {
		t.Fatalf("worker resource_count = %#v, want %d", got, want)
	}
}

func TestParserModuleFactKeysAreStableAcrossResourceOrder(t *testing.T) {
	t.Parallel()

	first := `{"serial":17,"lineage":"lineage-123","resources":[
		{"module":"module.api","mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"id":"i-1"}}]},
		{"module":"module.worker","mode":"managed","type":"aws_instance","name":"worker","instances":[{"attributes":{"id":"i-2"}}]}
	]}`
	second := `{"serial":17,"lineage":"lineage-123","resources":[
		{"module":"module.worker","mode":"managed","type":"aws_instance","name":"worker","instances":[{"attributes":{"id":"i-2"}}]},
		{"module":"module.api","mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"id":"i-1"}}]}
	]}`

	firstFacts := parseFixtureFacts(t, first)
	secondFacts := parseFixtureFacts(t, second)

	if got, want := stableKeysByKind(firstFacts, facts.TerraformStateModuleFactKind), stableKeysByKind(secondFacts, facts.TerraformStateModuleFactKind); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("module stable keys changed with order:\ngot  %#v\nwant %#v", got, want)
	}
}

func factsByKind(envelopes []facts.Envelope, kind string) []facts.Envelope {
	matches := []facts.Envelope{}
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			matches = append(matches, envelope)
		}
	}
	return matches
}

func factByStableKey(t *testing.T, envelopes []facts.Envelope, stableKey string) facts.Envelope {
	t.Helper()

	for _, envelope := range envelopes {
		if envelope.StableFactKey == stableKey {
			return envelope
		}
	}
	t.Fatalf("missing stable key %q in %#v", stableKey, envelopes)
	return facts.Envelope{}
}
