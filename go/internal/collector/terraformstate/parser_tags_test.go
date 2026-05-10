package terraformstate_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserEmitsTerraformStateTagObservationFacts(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[{
		"mode":"managed",
		"type":"aws_instance",
		"name":"web",
		"instances":[{
			"attributes":{
				"id":"i-1",
				"tags":{
					"Name":"web",
					"password":"super-secret",
					"Nested":{"unsafe":"value"}
				},
				"tags_all":{
					"Environment":"prod"
				}
			}
		}]
	}]}`

	result := parseFixtureFacts(t, state)

	requireFactKinds(t, result, facts.TerraformStateTagObservationFactKind, facts.TerraformStateWarningFactKind)
	tags := factsByKind(result, facts.TerraformStateTagObservationFactKind)
	if got, want := len(tags), 3; got != want {
		t.Fatalf("tag observation fact count = %d, want %d: %#v", got, want, tags)
	}

	name := factByPayloadValue(t, tags, "tag_key", "Name")
	if got, want := name.Payload["resource_address"], "aws_instance.web"; got != want {
		t.Fatalf("resource_address = %#v, want %q", got, want)
	}
	if got, want := name.Payload["tag_source"], "tags"; got != want {
		t.Fatalf("tag_source = %#v, want %q", got, want)
	}
	if got, want := name.Payload["tag_value"], "web"; got != want {
		t.Fatalf("tag_value = %#v, want %q", got, want)
	}

	environment := factByPayloadValue(t, tags, "tag_key", "Environment")
	if got, want := environment.Payload["tag_source"], "tags_all"; got != want {
		t.Fatalf("tags_all tag_source = %#v, want %q", got, want)
	}

	assertNoRawSecret(t, result, "super-secret")
	assertNoRawSecret(t, result, "password")
}

func TestParserTagObservationFactKeysAreStableAcrossResourceOrder(t *testing.T) {
	t.Parallel()

	first := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"tags":{"Name":"api"}}}]},
		{"mode":"managed","type":"aws_instance","name":"worker","instances":[{"attributes":{"tags":{"Name":"worker"}}}]}
	]}`
	second := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_instance","name":"worker","instances":[{"attributes":{"tags":{"Name":"worker"}}}]},
		{"mode":"managed","type":"aws_instance","name":"api","instances":[{"attributes":{"tags":{"Name":"api"}}}]}
	]}`

	firstFacts := parseFixtureFacts(t, first)
	secondFacts := parseFixtureFacts(t, second)

	if got, want := stableKeysByKind(firstFacts, facts.TerraformStateTagObservationFactKind), stableKeysByKind(secondFacts, facts.TerraformStateTagObservationFactKind); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("tag observation stable keys changed with order:\ngot  %#v\nwant %#v", got, want)
	}
}
