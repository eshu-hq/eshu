// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"errors"
	"reflect"
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// requiredFieldsForKind returns the reflectively derived required-field set
// for one fact kind, looked up via the payloadContracts registry
// (decode_test.go) so this file has no key list of its own to drift out of
// sync with the structs — it always asks the same single source of truth
// decodeAndValidate itself reads.
func requiredFieldsForKind(t *testing.T, factKind string) []string {
	t.Helper()
	for _, contract := range payloadContracts {
		if contract.factKind == factKind {
			return payloadKeySetOf(contract.typ).Required
		}
	}
	t.Fatalf("requiredFieldsForKind: no payloadContracts row for fact kind %q", factKind)
	return nil
}

// fullPayloadForKind returns a minimal valid payload map (every required key
// present, non-empty) for one fact kind, so a per-kind test can delete a single
// required key and prove decode dead-letters on exactly that field.
func fullPayloadForKind(t *testing.T, factKind string) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, key := range requiredFieldsForKind(t, factKind) {
		out[key] = "x"
	}
	return out
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
	case FactKindIncidentRecord:
		_, err := DecodeIncidentRecord(env)
		return err
	case FactKindIncidentLifecycleEvent:
		_, err := DecodeIncidentLifecycleEvent(env)
		return err
	case FactKindChangeRecord:
		_, err := DecodeChangeRecord(env)
		return err
	case FactKindIncidentRoutingAppliedPagerDutyResource:
		_, err := DecodeIncidentRoutingAppliedPagerDutyResource(env)
		return err
	case FactKindIncidentRoutingAppliedAlertRoute:
		_, err := DecodeIncidentRoutingAppliedAlertRoute(env)
		return err
	case FactKindIncidentRoutingObservedPagerDutyService:
		_, err := DecodeIncidentRoutingObservedPagerDutyService(env)
		return err
	case FactKindIncidentRoutingObservedPagerDutyIntegration:
		_, err := DecodeIncidentRoutingObservedPagerDutyIntegration(env)
		return err
	case FactKindIncidentRoutingCoverageWarning:
		_, err := DecodeIncidentRoutingCoverageWarning(env)
		return err
	default:
		t.Fatalf("decodeByKind: unhandled fact kind %q — add it to the switch", factKind)
		return nil
	}
}

// allDecodedKinds is every fact kind this module decodes, so the per-kind tests
// below fail if a new kind is added to payloadContracts without wiring its
// Decode dispatch and coverage here.
var allDecodedKinds = []string{
	FactKindAWSResource,
	FactKindAWSRelationship,
	FactKindAWSSecurityGroupRule,
	FactKindEC2InstancePosture,
	FactKindS3BucketPosture,
	FactKindAWSIAMPermission,
	FactKindAWSResourcePolicyPermission,
	FactKindAWSIAMPrincipal,
	FactKindIncidentRecord,
	FactKindIncidentLifecycleEvent,
	FactKindChangeRecord,
	FactKindIncidentRoutingAppliedPagerDutyResource,
	FactKindIncidentRoutingAppliedAlertRoute,
	FactKindIncidentRoutingObservedPagerDutyService,
	FactKindIncidentRoutingObservedPagerDutyIntegration,
	FactKindIncidentRoutingCoverageWarning,
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

			for _, field := range requiredFieldsForKind(t, factKind) {
				field := field
				t.Run(field, func(t *testing.T) {
					t.Parallel()

					payload := fullPayloadForKind(t, factKind)
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

			if err := decodeByKind(t, factKind, fullPayloadForKind(t, factKind)); err != nil {
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

			payload := fullPayloadForKind(t, factKind)
			for _, field := range requiredFieldsForKind(t, factKind) {
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

			env := Envelope{FactKind: factKind, SchemaVersion: "2.0.0", Payload: fullPayloadForKind(t, factKind)}
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
			case FactKindIncidentRecord:
				_, err = DecodeIncidentRecord(env)
			case FactKindIncidentLifecycleEvent:
				_, err = DecodeIncidentLifecycleEvent(env)
			case FactKindChangeRecord:
				_, err = DecodeChangeRecord(env)
			case FactKindIncidentRoutingAppliedPagerDutyResource:
				_, err = DecodeIncidentRoutingAppliedPagerDutyResource(env)
			case FactKindIncidentRoutingAppliedAlertRoute:
				_, err = DecodeIncidentRoutingAppliedAlertRoute(env)
			case FactKindIncidentRoutingObservedPagerDutyService:
				_, err = DecodeIncidentRoutingObservedPagerDutyService(env)
			case FactKindIncidentRoutingObservedPagerDutyIntegration:
				_, err = DecodeIncidentRoutingObservedPagerDutyIntegration(env)
			case FactKindIncidentRoutingCoverageWarning:
				_, err = DecodeIncidentRoutingCoverageWarning(env)
			}
			if !errors.Is(err, ErrUnsupportedSchemaMajor) {
				t.Fatalf("decode %s unsupported major: error = %v, want errors.Is ErrUnsupportedSchemaMajor", factKind, err)
			}
		})
	}
}

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
