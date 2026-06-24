// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func TestParserWarnsAndContinuesForMalformedTagMaps(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[{
		"mode":"managed",
		"type":"aws_instance",
		"name":"web",
		"instances":[{
			"attributes":{
				"id":"i-1",
				"tags":null,
				"tags_all":["unexpected"]
			}
		}]
	},{
		"mode":"managed",
		"type":"aws_instance",
		"name":"api",
		"instances":[{
			"attributes":{
				"id":"i-2",
				"tags":"unexpected"
			}
		}]
	}]}`

	result := parseFixtureFacts(t, state)

	requireFactKinds(t, result, facts.TerraformStateResourceFactKind, facts.TerraformStateWarningFactKind)
	if tags := factsByKind(result, facts.TerraformStateTagObservationFactKind); len(tags) != 0 {
		t.Fatalf("tag observation facts = %#v, want none for malformed tag maps", tags)
	}
	warnings := factsByKind(result, facts.TerraformStateWarningFactKind)
	if got, want := len(warnings), 3; got != want {
		t.Fatalf("warning count = %d, want %d: %#v", got, want, warnings)
	}
	expectTagMapWarning := func(source string, reason string, sourceShape string) {
		t.Helper()
		warning := factByPayloadValue(t, warnings, "source", source)
		if got, want := warning.Payload["warning_kind"], "tag_map_dropped"; got != want {
			t.Fatalf("warning_kind = %#v, want %q", got, want)
		}
		if got, want := warning.Payload["reason"], reason; got != want {
			t.Fatalf("reason = %#v, want %q", got, want)
		}
		if got, want := warning.Payload["source_shape"], sourceShape; got != want {
			t.Fatalf("source_shape = %#v, want %q", got, want)
		}
		switch reason {
		case "null_tag_map":
			assertWarningClassification(t, warning, "info", "accepted_normalization")
		case "malformed_tag_map":
			assertWarningClassification(t, warning, "warning", "source_normalization_review")
		default:
			assertWarningClassification(t, warning, "info", "accepted_guardrail")
		}
	}
	expectTagMapWarning("resources.aws_instance.web.attributes.tags", "null_tag_map", "null")
	expectTagMapWarning("resources.aws_instance.web.attributes.tags_all", "unsupported_tag_map_shape", "array")
	expectTagMapWarning("resources.aws_instance.api.attributes.tags", "malformed_tag_map", "scalar")
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
