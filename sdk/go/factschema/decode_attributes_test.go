// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"reflect"
	"testing"

	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
)

// TestDecodeAWSResource_AttributesPassThroughPreservesJSONTypes proves the
// untyped service-specific pass-through captures every payload key that has no
// named struct field, and — critically — preserves each value's JSON-native Go
// type. A service consumer that reads resource.Attributes["engine"] (a string),
// ["backup_retention_period"] (a float64 from a JSON number), or
// ["multi_az"] (a bool) gets exactly the type the raw env.Payload lookup
// produced today, so migrating a consumer off the raw payload is byte-identical.
// Named identity/common fields must NOT leak into Attributes.
func TestDecodeAWSResource_AttributesPassThroughPreservesJSONTypes(t *testing.T) {
	t.Parallel()

	payload := fullAWSResourcePayload()
	// Service-specific keys an RDS-posture-style consumer reads, in their
	// JSON-native shapes as they arrive from a Postgres JSONB round trip.
	payload["engine"] = "postgres"
	payload["backup_retention_period"] = float64(7)
	payload["multi_az"] = true
	payload["parameter_groups"] = []any{"pg-a", "pg-b"}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}

	if decoded.Attributes == nil {
		t.Fatal("Attributes = nil, want the service-specific keys captured")
	}
	// Named fields must not appear in the pass-through.
	for _, named := range []string{"account_id", "resource_id", "region", "resource_type", "arn", "name", "tags"} {
		if _, leaked := decoded.Attributes[named]; leaked {
			t.Fatalf("named field %q leaked into Attributes; it must be a typed field, not a pass-through key", named)
		}
	}
	if got, ok := decoded.Attributes["engine"].(string); !ok || got != "postgres" {
		t.Fatalf("Attributes[engine] = %#v, want string \"postgres\"", decoded.Attributes["engine"])
	}
	if got, ok := decoded.Attributes["backup_retention_period"].(float64); !ok || got != 7 {
		t.Fatalf("Attributes[backup_retention_period] = %#v, want float64(7)", decoded.Attributes["backup_retention_period"])
	}
	if got, ok := decoded.Attributes["multi_az"].(bool); !ok || !got {
		t.Fatalf("Attributes[multi_az] = %#v, want bool true", decoded.Attributes["multi_az"])
	}
	groups, ok := decoded.Attributes["parameter_groups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("Attributes[parameter_groups] = %#v, want []any of length 2", decoded.Attributes["parameter_groups"])
	}
}

// TestDecodeAWSResource_RoundTrip_Attributes proves an encoded Resource carrying
// Attributes flattens those keys back to the top-level payload shape (not nested
// under an "attributes" object) and decodes back deep-equal, so the pass-through
// is a faithful inverse the emit side can rely on.
func TestDecodeAWSResource_RoundTrip_Attributes(t *testing.T) {
	t.Parallel()

	original := awsv1.Resource{
		AccountID:    "111111111111",
		ResourceID:   "db-1",
		Region:       "us-east-1",
		ResourceType: "aws_rds_db_instance",
		Attributes: map[string]any{
			"engine":   "postgres",
			"multi_az": true,
		},
	}

	payload, err := EncodeAWSResource(original)
	if err != nil {
		t.Fatalf("EncodeAWSResource() error = %v, want nil", err)
	}
	if _, nested := payload["attributes"]; nested {
		t.Fatalf("EncodeAWSResource nested the pass-through under \"attributes\"; it must flatten to top level; payload = %v", payload)
	}
	if payload["engine"] != "postgres" {
		t.Fatalf("payload[engine] = %v, want flattened \"postgres\"", payload["engine"])
	}

	decoded, err := DecodeAWSResource(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeAWSResource() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeAWSResource() = %+v, want %+v", decoded, original)
	}
}
