// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	collector "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

// awsResourceKind is the namespaced fact kind an out-of-tree collector emits
// when it re-emits the aws_resource payload SHAPE. The wire kind is namespaced
// (collector-extraction-policy.md: external collectors emit namespaced kinds,
// never the bare core kind "aws_resource", which is host-owned via
// ReservedFactKinds) while the payload is validated against the aws_resource
// schema shape. PayloadSchemas is keyed by this fixture kind, and the fixture
// pack ships the aws_resource schema shape as the reusable artifact.
const awsResourceKind = "dev.eshu.examples.aws.resource"

// awsResourceSchema is the checked-in aws_resource.v1 payload schema
// (sdk/go/factschema/schema/aws_resource.v1.schema.json). It is inlined here so
// the conformance package test proves payload validation without importing the
// factschema module (which would pull that module's schema-generator dependency
// into the dependency-free collector module). The fixture pack ships this exact
// artifact for out-of-tree callers.
const awsResourceSchema = `{
  "$id": "https://eshu.dev/schemas/factschema/aws/v1/resource.schema.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "additionalProperties": true,
  "properties": {
    "account_id": {"type": "string"},
    "arn": {"type": ["string", "null"]},
    "correlation_anchors": {"items": {"type": "string"}, "type": ["array", "null"]},
    "name": {"type": ["string", "null"]},
    "region": {"type": "string"},
    "resource_id": {"type": "string"},
    "resource_type": {"type": "string"},
    "service_kind": {"type": ["string", "null"]},
    "state": {"type": ["string", "null"]},
    "tags": {"additionalProperties": {"type": "string"}, "type": ["object", "null"]}
  },
  "required": ["account_id", "resource_id", "region", "resource_type"],
  "title": "Eshu aws_resource Payload (schema version 1)",
  "type": "object"
}`

// awsManifest builds a manifest that declares the aws_resource fact kind so a
// fixture emitting it passes kind/version/confidence checks and the payload
// validation is the only thing that can fail.
func awsManifest() conformance.Manifest {
	manifest := validManifest()
	manifest.Spec.EmittedFacts = []conformance.FactFamily{{
		Kind:             awsResourceKind,
		SchemaVersions:   []string{"1.0.0"},
		SourceConfidence: []string{"observed"},
	}}
	return manifest
}

// awsResourceResult builds a valid collector result emitting one aws_resource
// fact with the supplied payload, wired so envelope/claim/source-ref checks all
// pass and only the payload shape is under test.
func awsResourceResult(payload map[string]any) collector.Result {
	observedAt := time.Date(2026, time.June, 9, 15, 0, 0, 0, time.UTC)
	claim := collector.Claim{
		ComponentID:   "dev.example.collector.scorecard",
		InstanceID:    "aws-primary",
		CollectorKind: "scorecard",
		SourceSystem:  "dev.example.collector.scorecard",
		Scope:         collector.Scope{ID: "component:aws-primary", Kind: "component"},
		SourceRunID:   "run-1",
		GenerationID:  "generation-1",
		WorkItemID:    "work-1",
		FencingToken:  "fence-1",
		Attempt:       1,
		Deadline:      observedAt.Add(time.Hour),
		ConfigHandle:  "component-config:scorecard",
	}
	return collector.Result{
		ProtocolVersion: collector.ProtocolVersionV1Alpha1,
		State:           collector.ResultComplete,
		Claim:           claim,
		Generation:      collector.Generation{ID: "generation-1", ObservedAt: observedAt},
		Facts: []collector.Fact{{
			Kind:             awsResourceKind,
			SchemaVersion:    "1.0.0",
			StableKey:        "aws:resource:1",
			SourceConfidence: collector.SourceConfidenceObserved,
			ObservedAt:       observedAt,
			SourceRef: collector.SourceRef{
				SourceSystem: "dev.example.collector.scorecard",
				ScopeID:      "component:aws-primary",
				GenerationID: "generation-1",
				FactKey:      "aws:resource:1",
				URI:          "component://aws/resource/1",
				RecordID:     "resource-1",
			},
			Payload: payload,
		}},
	}
}

func validAWSResourcePayload() map[string]any {
	return map[string]any{
		"account_id":    "123456789012",
		"resource_id":   "vpc-abc123",
		"region":        "us-east-1",
		"resource_type": "aws_vpc",
	}
}

// TestRunValidatesPayloadAgainstSchema is the accuracy gate this issue adds: a
// fixture whose aws_resource payload omits the required "region" field must fail
// closed with a finding that names the offending field. Before payload schema
// validation existed, this fixture passed conformance (kind, version, and
// confidence were all valid) — the silent-wrong-truth hole the contract system
// exists to close.
func TestRunValidatesPayloadAgainstSchema(t *testing.T) {
	t.Parallel()

	payload := validAWSResourcePayload()
	delete(payload, "region")

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(payload)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(awsResourceSchema)},
	})

	if report.OK() {
		t.Fatal("report OK = true, want failed for aws_resource payload missing required field")
	}
	assertFinding(t, report, conformance.FindingPayloadSchemaInvalid)

	if !findingMentions(report, conformance.FindingPayloadSchemaInvalid, "region") {
		t.Fatalf("findings = %#v, want a payload-schema finding naming %q", report.Findings, "region")
	}
}

// TestRunAcceptsSchemaValidPayload proves a complete, schema-valid aws_resource
// payload passes once payload validation is wired.
func TestRunAcceptsSchemaValidPayload(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(validAWSResourcePayload())},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(awsResourceSchema)},
	})

	if !report.OK() {
		t.Fatalf("findings = %#v, want passed for schema-valid aws_resource payload", report.Findings)
	}
}

// TestRunRejectsWrongTypedPayloadField proves a required field present with the
// wrong JSON type (a number where the schema requires a string) fails closed and
// names the field, not only the missing-field case.
func TestRunRejectsWrongTypedPayloadField(t *testing.T) {
	t.Parallel()

	payload := validAWSResourcePayload()
	payload["region"] = float64(42)

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(payload)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(awsResourceSchema)},
	})

	if report.OK() {
		t.Fatal("report OK = true, want failed for aws_resource payload with wrong-typed field")
	}
	if !findingMentions(report, conformance.FindingPayloadSchemaInvalid, "region") {
		t.Fatalf("findings = %#v, want a payload-schema finding naming %q", report.Findings, "region")
	}
}

// TestRunPassesThroughKindsWithoutSchema proves that a fact kind with no
// registered schema (a provenance-only kind, like the scorecard example's own
// dev.eshu.examples.* kinds) is not payload-validated and still passes, matching
// the change-matrix "provenance-only fact kind" row: stored, unconsumed, no
// schema required.
func TestRunPassesThroughKindsWithoutSchema(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest: validManifest(),
		Fixtures: []collector.Result{validResult()},
		Mode:     conformance.ModeFixture,
		// A schema is supplied for aws_resource, but the fixture emits
		// dev.example.scorecard.snapshot, which has no schema — it must pass.
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(awsResourceSchema)},
	})

	if !report.OK() {
		t.Fatalf("findings = %#v, want passed for a kind with no registered schema", report.Findings)
	}
}

// TestRunRejectsUnsupportedSchemaConstruct is the fail-closed guardrail: the
// stdlib schema subset validator must reject, not silently skip, any schema
// construct it does not implement. A schema using "enum" (unsupported) must
// produce a blocking finding rather than a false pass — silent under-validation
// is the exact failure the contract system exists to kill.
func TestRunRejectsUnsupportedSchemaConstruct(t *testing.T) {
	t.Parallel()

	const enumSchema = `{
      "type": "object",
      "additionalProperties": true,
      "required": ["account_id"],
      "properties": {"account_id": {"type": "string", "enum": ["a", "b"]}}
    }`

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(validAWSResourcePayload())},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(enumSchema)},
	})

	if report.OK() {
		t.Fatal("report OK = true, want failed for an unsupported schema construct")
	}
	assertFinding(t, report, conformance.FindingPayloadSchemaInvalid)
}

// TestRunRejectsMalformedSchema proves a schema that is not valid JSON fails
// closed rather than being ignored.
func TestRunRejectsMalformedSchema(t *testing.T) {
	t.Parallel()

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(validAWSResourcePayload())},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage("{not json")},
	})

	if report.OK() {
		t.Fatal("report OK = true, want failed for a malformed schema")
	}
	assertFinding(t, report, conformance.FindingPayloadSchemaInvalid)
}

// TestCompileSchemaRejectsUnsupportedCompositions locks the fail-closed
// contract at the compile boundary directly: every composition keyword outside
// the supported subset must be rejected, not silently ignored. This is the unit
// counterpart to the in-tree construct-coverage test — it proves the validator
// says no, where that test proves the real schemas stay inside the yes set.
func TestCompileSchemaRejectsUnsupportedCompositions(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"ref":                `{"type":"object","properties":{"a":{"$ref":"#/x"}}}`,
		"oneOf":              `{"type":"object","oneOf":[{"type":"object"}]}`,
		"anyOf":              `{"type":"object","properties":{"a":{"anyOf":[{"type":"string"}]}}}`,
		"allOf":              `{"type":"object","allOf":[{"type":"object"}]}`,
		"enum":               `{"type":"object","properties":{"a":{"type":"string","enum":["x"]}}}`,
		"pattern":            `{"type":"object","properties":{"a":{"type":"string","pattern":"^x$"}}}`,
		"numericBounds":      `{"type":"object","properties":{"a":{"type":"integer","minimum":0}}}`,
		"nonObjectRoot":      `{"type":"array","items":{"type":"string"}}`,
		"unknownType":        `{"type":"object","properties":{"a":{"type":"date"}}}`,
		"closedObjectRoot":   `{"type":"object","additionalProperties":false,"properties":{"a":{"type":"string"}}}`,
		"closedNestedObject": `{"type":"object","additionalProperties":true,"properties":{"o":{"type":"object","additionalProperties":false,"properties":{"x":{"type":"string"}}}}}`,
	}
	for name, schema := range cases {
		name, schema := name, schema
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := conformance.CompileSchema(json.RawMessage(schema)); err == nil {
				t.Fatalf("CompileSchema accepted an unsupported construct %q, want fail closed", name)
			}
		})
	}
}

// TestCompileSchemaAcceptsOpenObjects proves the reject of
// additionalProperties:false does not over-reject the open shapes every
// checked-in schema uses: additionalProperties:true and an omitted
// additionalProperties both compile, and the string-valued map form compiles.
func TestCompileSchemaAcceptsOpenObjects(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"additionalTrue":  `{"type":"object","additionalProperties":true,"properties":{"a":{"type":"string"}}}`,
		"additionalOmit":  `{"type":"object","properties":{"a":{"type":"string"}}}`,
		"stringValuedMap": `{"type":"object","additionalProperties":true,"properties":{"tags":{"type":["object","null"],"additionalProperties":{"type":"string"}}}}`,
	}
	for name, schema := range cases {
		name, schema := name, schema
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := conformance.CompileSchema(json.RawMessage(schema)); err != nil {
				t.Fatalf("CompileSchema rejected an open object %q: %v", name, err)
			}
		})
	}
}

// TestRunValidatesNestedObjectArrayItems proves the validator descends into a
// nested-object array element (the ec2_instance_posture block_devices shape): a
// wrong-typed field inside an array-of-objects element fails closed and names
// the nested path.
func TestRunValidatesNestedObjectArrayItems(t *testing.T) {
	t.Parallel()

	const nestedSchema = `{
      "type": "object",
      "additionalProperties": true,
      "required": ["account_id"],
      "properties": {
        "account_id": {"type": "string"},
        "block_devices": {
          "type": ["array", "null"],
          "items": {
            "type": "object",
            "additionalProperties": true,
            "properties": {"encrypted": {"type": ["boolean", "null"]}}
          }
        }
      }
    }`

	payload := map[string]any{
		"account_id":    "123456789012",
		"block_devices": []any{map[string]any{"encrypted": "yes"}}, // string, want boolean|null
	}

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(payload)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(nestedSchema)},
	})

	if report.OK() {
		t.Fatal("report OK = true, want failed for wrong-typed nested-object array element")
	}
	if !findingMentions(report, conformance.FindingPayloadSchemaInvalid, "encrypted") {
		t.Fatalf("findings = %#v, want a finding naming the nested field %q", report.Findings, "encrypted")
	}
}

// TestRunAcceptsNativeGoPayloadValues proves conformance accepts a payload built
// with native JSON-serializable Go types — the exact shape an out-of-tree
// collector's Collect() returns before any json round trip. int, []string, and
// map[string]string must validate against integer, array, and string-map schema
// fields without the caller having to marshal/unmarshal first. Before the fix
// these decoded to Go's native kinds that the type switch reported as "unknown",
// failing an otherwise-valid payload.
func TestRunAcceptsNativeGoPayloadValues(t *testing.T) {
	t.Parallel()

	const nativeSchema = `{
      "type": "object",
      "additionalProperties": true,
      "required": ["account_id"],
      "properties": {
        "account_id": {"type": "string"},
        "port": {"type": ["integer", "null"]},
        "anchors": {"type": ["array", "null"], "items": {"type": "string"}},
        "tags": {"type": ["object", "null"], "additionalProperties": {"type": "string"}}
      }
    }`

	payload := map[string]any{
		"account_id": "123456789012",
		"port":       int(443),                         // native int, not float64
		"anchors":    []string{"a", "b"},               // native []string, not []any
		"tags":       map[string]string{"env": "prod"}, // native map, not map[string]any
	}

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(payload)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(nativeSchema)},
	})

	if !report.OK() {
		t.Fatalf("findings = %#v, want passed for native Go payload values", report.Findings)
	}
}

// TestRunRejectsNonSerializablePayload proves a payload that cannot be marshaled
// to JSON (a channel value) fails closed rather than panicking or passing.
func TestRunRejectsNonSerializablePayload(t *testing.T) {
	t.Parallel()

	payload := validAWSResourcePayload()
	payload["bad"] = make(chan int)

	report := conformance.Run(conformance.Request{
		Manifest:       awsManifest(),
		Fixtures:       []collector.Result{awsResourceResult(payload)},
		Mode:           conformance.ModeFixture,
		PayloadSchemas: map[string]json.RawMessage{awsResourceKind: json.RawMessage(awsResourceSchema)},
	})

	if report.OK() {
		t.Fatal("report OK = true, want failed for a non-JSON-serializable payload")
	}
}

// findingMentions reports whether the report carries a finding of the given code
// whose message contains substr.
func findingMentions(report conformance.Report, code conformance.FindingCode, substr string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code && strings.Contains(finding.Message, substr) {
			return true
		}
	}
	return false
}
