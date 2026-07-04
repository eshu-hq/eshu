// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// TestDecodeInt32Field_FailsClosedOnMalformedNumber proves the marshal-free
// decoder rejects a JSONB number that does not fit an int32 field
// (aws_security_group_rule.from_port) as a classified input_invalid dead-letter,
// rather than silently truncating a non-integral value or wrapping an
// out-of-range one. A silent int32(8080.5)=8080 or an int32(overflow) wrap would
// project a wrong port into the reachability graph — the exact silent-corruption
// class this PR exists to close. The valid integral float64 case (the shape a
// Postgres JSONB roundtrip actually delivers) still decodes, so the guard cannot
// pass by rejecting every number.
func TestDecodeInt32Field_FailsClosedOnMalformedNumber(t *testing.T) {
	t.Parallel()

	baseValid := func() map[string]any {
		return map[string]any{
			"account_id":   "123456789012",
			"region":       "us-east-1",
			"group_id":     "sg-0abc",
			"direction":    "ingress",
			"ip_protocol":  "tcp",
			"source_kind":  "cidr",
			"source_value": "0.0.0.0/0",
		}
	}

	t.Run("valid_integral_float64_decodes", func(t *testing.T) {
		t.Parallel()
		payload := baseValid()
		payload["from_port"] = float64(8080) // JSONB delivers numbers as float64.
		env := Envelope{FactKind: FactKindAWSSecurityGroupRule, SchemaVersion: "1.0.0", Payload: payload}
		rule, err := DecodeAWSSecurityGroupRule(env)
		if err != nil {
			t.Fatalf("decode valid from_port 8080: error = %v, want nil", err)
		}
		if rule.FromPort == nil || *rule.FromPort != 8080 {
			t.Fatalf("decode valid from_port 8080: FromPort = %v, want 8080", rule.FromPort)
		}
	})

	malformed := map[string]any{
		"non_integral":  float64(8080.5),
		"overflow_high": float64(3_000_000_000),
		"overflow_low":  float64(-3_000_000_000),
	}
	for name, badValue := range malformed {
		name, badValue := name, badValue
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload := baseValid()
			payload["from_port"] = badValue
			env := Envelope{FactKind: FactKindAWSSecurityGroupRule, SchemaVersion: "1.0.0", Payload: payload}
			_, err := DecodeAWSSecurityGroupRule(env)
			if err == nil {
				t.Fatalf("decode malformed from_port %v: error = nil, want *DecodeError", badValue)
			}
			var decodeErr *DecodeError
			if !errors.As(err, &decodeErr) {
				t.Fatalf("decode malformed from_port %v: error = %T, want *DecodeError", badValue, err)
			}
			if decodeErr.Classification != ClassificationInputInvalid {
				t.Fatalf("decode malformed from_port %v: classification = %q, want %q", badValue, decodeErr.Classification, ClassificationInputInvalid)
			}
		})
	}
}

// TestDecodeAWSRelationship_AttributesPassThroughPreservesJSONTypes proves the
// aws_relationship pass-through captures verb-specific keys (the nested
// "attributes" object a cloudwatch_alarm_observes_metric fact carries) with JSON
// type fidelity, and that named identity fields never leak into Attributes. This
// mirrors the aws_resource pass-through so both polymorphic AWS envelopes behave
// identically.
func TestDecodeAWSRelationship_AttributesPassThroughPreservesJSONTypes(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"account_id":         "111111111111",
		"region":             "us-east-1",
		"relationship_type":  "cloudwatch_alarm_observes_metric",
		"source_resource_id": "arn:aws:cloudwatch:us-east-1:111111111111:alarm:cpu",
		"target_resource_id": "i-0123456789abcdef0",
		// Verb-specific nested attributes bag the reducer reads.
		"attributes": map[string]any{
			"dimensions": []any{
				map[string]any{"name": "InstanceId", "value": "i-0123456789abcdef0"},
			},
		},
	}

	decoded, err := DecodeAWSRelationship(Envelope{FactKind: FactKindAWSRelationship, SchemaVersion: "1.0.0", Payload: payload})
	if err != nil {
		t.Fatalf("DecodeAWSRelationship() error = %v, want nil", err)
	}
	if decoded.Attributes == nil {
		t.Fatal("Attributes = nil, want the verb-specific attributes captured")
	}
	for _, named := range []string{"account_id", "region", "relationship_type", "source_resource_id", "target_resource_id"} {
		if _, leaked := decoded.Attributes[named]; leaked {
			t.Fatalf("named field %q leaked into Attributes", named)
		}
	}
	nested, ok := decoded.Attributes["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("Attributes[attributes] = %#v, want map[string]any", decoded.Attributes["attributes"])
	}
	dims, ok := nested["dimensions"].([]any)
	if !ok || len(dims) != 1 {
		t.Fatalf("nested dimensions = %#v, want []any of length 1", nested["dimensions"])
	}
}

// TestDecodeAWSRelationship_RoundTrip_Attributes proves an encoded Relationship
// carrying Attributes flattens the verb-specific keys back to the top-level
// payload (not nested under a stray key) and decodes back deep-equal.
func TestDecodeAWSRelationship_RoundTrip_Attributes(t *testing.T) {
	t.Parallel()

	original := awsv1.Relationship{
		AccountID:        "111111111111",
		Region:           "us-east-1",
		RelationshipType: "xray_matches_service",
		SourceResourceID: "svc-a",
		TargetResourceID: "svc-b",
		Attributes: map[string]any{
			"attributes": map[string]any{"service_name": "checkout"},
		},
	}

	payload, err := EncodeAWSRelationship(original)
	if err != nil {
		t.Fatalf("EncodeAWSRelationship() error = %v, want nil", err)
	}
	if _, ok := payload["attributes"]; !ok {
		t.Fatalf("EncodeAWSRelationship dropped the verb-specific attributes; payload = %v", payload)
	}

	decoded, err := DecodeAWSRelationship(Envelope{FactKind: FactKindAWSRelationship, SchemaVersion: "1.0.0", Payload: payload})
	if err != nil {
		t.Fatalf("DecodeAWSRelationship() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSRelationship() = %+v, want %+v", decoded, original)
	}
}
