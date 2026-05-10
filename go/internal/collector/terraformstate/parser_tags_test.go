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

	for _, tag := range tags {
		if got, want := tag.Payload["resource_address"], "aws_instance.web"; got != want {
			t.Fatalf("resource_address = %#v, want %q", got, want)
		}
		assertTagFieldRedacted(t, tag, "tag_key")
		assertTagFieldRedacted(t, tag, "tag_value")
	}

	assertNoRawSecret(t, result, "Name")
	assertNoRawSecret(t, result, "Environment")
	assertNoRawSecret(t, result, "prod")
	assertNoRawSecret(t, result, "super-secret")
	assertNoRawSecret(t, result, "password")
}

func assertTagFieldRedacted(t *testing.T, tag facts.Envelope, field string) {
	t.Helper()

	value, ok := tag.Payload[field].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want redaction marker map", field, tag.Payload[field])
	}
	marker, ok := value["marker"].(string)
	if !ok || !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("%s marker = %#v, want redacted marker", field, value["marker"])
	}
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
