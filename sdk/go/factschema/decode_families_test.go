// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// fullPayloadForKind returns a minimal valid payload map (every required key
// present, non-empty) for one fact kind, so a per-kind test can delete a single
// required key and prove decode dead-letters on exactly that field.
func fullPayloadForKind(factKind string) map[string]any {
	base := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for _, key := range requiredFields[factKind] {
			out[key] = "x"
		}
		for key, value := range extra {
			out[key] = value
		}
		return out
	}
	return base(nil)
}

// decodeByKind dispatches to the kind's public Decode function so the test
// exercises the real production seam, not a re-implementation. It returns the
// error only, which is all the required-field tests assert on.
func decodeByKind(t *testing.T, factKind string, payload map[string]any) error {
	t.Helper()
	env := Envelope{FactKind: factKind, SchemaVersion: "1.0.0", Payload: payload}
	switch factKind {
	case FactKindAWSResource:
		_, err := DecodeAWSResource(env)
		return err
	case FactKindAWSRelationship:
		_, err := DecodeAWSRelationship(env)
		return err
	case FactKindAWSSecurityGroupRule:
		_, err := DecodeAWSSecurityGroupRule(env)
		return err
	case FactKindEC2InstancePosture:
		_, err := DecodeEC2InstancePosture(env)
		return err
	case FactKindS3BucketPosture:
		_, err := DecodeS3BucketPosture(env)
		return err
	case FactKindAWSIAMPermission:
		_, err := DecodeAWSIAMPermission(env)
		return err
	case FactKindAWSResourcePolicyPermission:
		_, err := DecodeAWSResourcePolicyPermission(env)
		return err
	case FactKindAWSIAMPrincipal:
		_, err := DecodeAWSIAMPrincipal(env)
		return err
	default:
		t.Fatalf("decodeByKind: unhandled fact kind %q — add it to the switch", factKind)
		return nil
	}
}

// allDecodedKinds is every fact kind this module decodes, so the per-kind tests
// below fail if a new kind is added to requiredFields without wiring its Decode
// dispatch and coverage here.
var allDecodedKinds = []string{
	FactKindAWSResource,
	FactKindAWSRelationship,
	FactKindAWSSecurityGroupRule,
	FactKindEC2InstancePosture,
	FactKindS3BucketPosture,
	FactKindAWSIAMPermission,
	FactKindAWSResourcePolicyPermission,
	FactKindAWSIAMPrincipal,
}

// TestDecodeEachKind_MissingEachRequiredFieldDeadLetters proves, for every
// decoded fact kind and every one of its required fields, that removing that one
// field from an otherwise-valid payload yields a classified *DecodeError naming
// exactly that field with ClassificationInputInvalid. This is the accuracy
// backstop generalized across the whole migrated domain: no required field can
// go silently unvalidated.
func TestDecodeEachKind_MissingEachRequiredFieldDeadLetters(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		factKind := factKind
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			for _, field := range requiredFields[factKind] {
				field := field
				t.Run(field, func(t *testing.T) {
					t.Parallel()

					payload := fullPayloadForKind(factKind)
					delete(payload, field)

					err := decodeByKind(t, factKind, payload)
					if err == nil {
						t.Fatalf("decode %s missing %q: error = nil, want *DecodeError", factKind, field)
					}
					var decodeErr *DecodeError
					if !errors.As(err, &decodeErr) {
						t.Fatalf("decode %s missing %q: error = %T, want *DecodeError", factKind, field, err)
					}
					if decodeErr.Classification != ClassificationInputInvalid {
						t.Fatalf("decode %s missing %q: classification = %q, want %q", factKind, field, decodeErr.Classification, ClassificationInputInvalid)
					}
					if decodeErr.Field != field {
						t.Fatalf("decode %s missing %q: field = %q, want %q", factKind, field, decodeErr.Field, field)
					}
				})
			}
		})
	}
}

// TestDecodeEachKind_FullRequiredPayloadDecodes proves that an envelope carrying
// every required key (each present and non-empty) decodes without error for
// every kind — the positive counterpart to the missing-field test, so the
// dead-letter assertion cannot pass merely because decode always errors.
func TestDecodeEachKind_FullRequiredPayloadDecodes(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		factKind := factKind
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			if err := decodeByKind(t, factKind, fullPayloadForKind(factKind)); err != nil {
				t.Fatalf("decode %s full required payload: error = %v, want nil", factKind, err)
			}
		})
	}
}

// TestDecodeEachKind_PresentButEmptyRequiredFieldDecodes proves the
// absent-vs-empty distinction holds for every kind: a required key present with
// an empty string is a valid observed value and decodes, while only an absent or
// null key dead-letters (covered above). This guards the byte-identical contract
// — an incomplete-but-present fact must decode exactly as it did before typing.
func TestDecodeEachKind_PresentButEmptyRequiredFieldDecodes(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		factKind := factKind
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			payload := fullPayloadForKind(factKind)
			for _, field := range requiredFields[factKind] {
				payload[field] = ""
			}
			if err := decodeByKind(t, factKind, payload); err != nil {
				t.Fatalf("decode %s all-empty required payload: error = %v, want nil (present-but-empty is valid)", factKind, err)
			}
		})
	}
}

// TestDecodeEachKind_UnsupportedMajorDeadLetters proves every kind's Decode
// function rejects an unsupported schema-version major as a classified error
// wrapping ErrUnsupportedSchemaMajor, not a best-effort decode.
func TestDecodeEachKind_UnsupportedMajorDeadLetters(t *testing.T) {
	t.Parallel()

	for _, factKind := range allDecodedKinds {
		factKind := factKind
		t.Run(factKind, func(t *testing.T) {
			t.Parallel()

			env := Envelope{FactKind: factKind, SchemaVersion: "2.0.0", Payload: fullPayloadForKind(factKind)}
			var err error
			switch factKind {
			case FactKindAWSResource:
				_, err = DecodeAWSResource(env)
			case FactKindAWSRelationship:
				_, err = DecodeAWSRelationship(env)
			case FactKindAWSSecurityGroupRule:
				_, err = DecodeAWSSecurityGroupRule(env)
			case FactKindEC2InstancePosture:
				_, err = DecodeEC2InstancePosture(env)
			case FactKindS3BucketPosture:
				_, err = DecodeS3BucketPosture(env)
			case FactKindAWSIAMPermission:
				_, err = DecodeAWSIAMPermission(env)
			case FactKindAWSResourcePolicyPermission:
				_, err = DecodeAWSResourcePolicyPermission(env)
			case FactKindAWSIAMPrincipal:
				_, err = DecodeAWSIAMPrincipal(env)
			}
			if !errors.Is(err, ErrUnsupportedSchemaMajor) {
				t.Fatalf("decode %s unsupported major: error = %v, want errors.Is ErrUnsupportedSchemaMajor", factKind, err)
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
