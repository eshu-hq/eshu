// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance_test

import (
	"encoding/json"
	"testing"

	collector "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

const enumPayloadSchema = `{
  "type": "object",
  "additionalProperties": true,
  "required": ["account_id"],
  "properties": {
    "account_id": {"type": "string", "enum": ["123456789012", "210987654321"]},
    "state": {"type": ["string", "null"], "enum": ["active", null]}
  }
}`

func TestRunAcceptsAndEnforcesStringEnums(t *testing.T) {
	t.Parallel()

	valid := validAWSResourcePayload()
	valid["state"] = "active"
	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(valid)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(enumPayloadSchema)},
	})
	if !report.OK() {
		t.Fatalf("valid enum findings = %#v, want passed", report.Findings)
	}

	invalid := validAWSResourcePayload()
	invalid["state"] = "inactive"
	report = conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(invalid)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(enumPayloadSchema)},
	})
	if report.OK() {
		t.Fatal("invalid enum report OK = true, want failed")
	}
	if !findingMentions(report, conformance.FindingPayloadSchemaInvalid, "state") {
		t.Fatalf("invalid enum findings = %#v, want field name", report.Findings)
	}
}

func TestRunEnumStillConstrainsNullableProperty(t *testing.T) {
	t.Parallel()

	const schema = `{
      "type": "object",
      "additionalProperties": true,
      "properties": {
        "state": {"type": ["string", "null"], "enum": ["active"]}
      }
    }`
	payload := validAWSResourcePayload()
	payload["state"] = nil
	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(payload)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(schema)},
	})
	if report.OK() {
		t.Fatal("null outside enum report OK = true, want failed")
	}
	if !findingMentions(report, conformance.FindingPayloadSchemaInvalid, "state") {
		t.Fatalf("null outside enum findings = %#v, want field name", report.Findings)
	}
}

func TestCompileSchemaRejectsNonStringEnums(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"boolean":              `{"type":"object","properties":{"a":{"type":"boolean","enum":[true]}}}`,
		"integer":              `{"type":"object","properties":{"a":{"type":"integer","enum":[1]}}}`,
		"number":               `{"type":"object","properties":{"a":{"type":"number","enum":[1.5]}}}`,
		"nullOnly":             `{"type":"object","properties":{"a":{"type":"null","enum":[null]}}}`,
		"mixedStringNumber":    `{"type":"object","properties":{"a":{"type":["string","number"],"enum":["x",1]}}}`,
		"adjacentAbove2To53":   `{"type":"object","properties":{"a":{"type":"integer","enum":[9007199254740992,9007199254740993]}}}`,
		"outsideInt64":         `{"type":"object","properties":{"a":{"type":"integer","enum":[18446744073709551616]}}}`,
		"highPrecisionDecimal": `{"type":"object","properties":{"a":{"type":"number","enum":[1.0000000000000001,1.0000000000000002]}}}`,
		"equalSpellings":       `{"type":"object","properties":{"a":{"type":"number","enum":[1,1.0,1e0]}}}`,
	}
	for name, schema := range cases {
		name, schema := name, schema
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := conformance.CompileSchema(json.RawMessage(schema)); err == nil {
				t.Fatalf("CompileSchema accepted non-string enum %q", name)
			}
		})
	}
}

func TestCompileSchemaRejectsInvalidStringEnums(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":                `{"type":"object","properties":{"a":{"type":"string","enum":[]}}}`,
		"duplicate":            `{"type":"object","properties":{"a":{"type":"string","enum":["x","x"]}}}`,
		"duplicateNull":        `{"type":"object","properties":{"a":{"type":["string","null"],"enum":[null,null]}}}`,
		"nullWithoutNullType":  `{"type":"object","properties":{"a":{"type":"string","enum":["x",null]}}}`,
		"wrongType":            `{"type":"object","properties":{"a":{"type":"string","enum":[1]}}}`,
		"arrayValue":           `{"type":"object","properties":{"a":{"type":"array","items":{"type":"string"},"enum":[["x"]]}}}`,
		"objectValue":          `{"type":"object","properties":{"a":{"type":"object","enum":[{"x":"y"}]}}}`,
		"arrayOnString":        `{"type":"object","properties":{"a":{"type":"string","enum":[["x"]]}}}`,
		"objectOnString":       `{"type":"object","properties":{"a":{"type":"string","enum":[{"x":"y"}]}}}`,
		"additionalProperties": `{"type":"object","additionalProperties":{"type":"string","enum":["x"]}}`,
	}
	for name, schema := range cases {
		name, schema := name, schema
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := conformance.CompileSchema(json.RawMessage(schema)); err == nil {
				t.Fatalf("CompileSchema accepted invalid enum %q", name)
			}
		})
	}
}
