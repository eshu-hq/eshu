package ssm

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsSSMParameterMetadataOnlyFactsAndKMSRelationship(t *testing.T) {
	parameterARN := "arn:aws:ssm:us-east-1:123456789012:parameter/orders/db/password"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	client := fakeClient{parameters: []Parameter{{
		ARN:                   parameterARN,
		Name:                  "/orders/db/password",
		Type:                  "SecureString",
		Tier:                  "Advanced",
		DataType:              "text",
		KeyID:                 kmsARN,
		LastModifiedAt:        time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		DescriptionPresent:    true,
		AllowedPatternPresent: true,
		Policies: []PolicyMetadata{{
			Type:   "Expiration",
			Status: "Pending",
		}},
		Tags: map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeSSMParameter)
	if got, want := resource.Payload["arn"], parameterARN; got != want {
		t.Fatalf("parameter arn = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["name"], "/orders/db/password"; got != want {
		t.Fatalf("parameter name = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, resource)
	assertAttribute(t, attributes, "type", "SecureString")
	assertAttribute(t, attributes, "tier", "Advanced")
	assertAttribute(t, attributes, "data_type", "text")
	assertAttribute(t, attributes, "key_id", kmsARN)
	assertAttribute(t, attributes, "last_modified_at", time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	assertAttribute(t, attributes, "description_present", true)
	assertAttribute(t, attributes, "allowed_pattern_present", true)
	assertPolicyMetadata(t, attributes, []map[string]string{{"type": "Expiration", "status": "Pending"}})
	for _, forbidden := range []string{
		"value",
		"parameter_value",
		"history_values",
		"description",
		"allowed_pattern",
		"policy_text",
		"policy_json",
		"policies_json",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; SSM scanner must stay metadata-only", forbidden)
		}
	}

	relationship := relationshipByType(t, envelopes, awscloud.RelationshipSSMParameterUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], kmsARN; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got, want := relationship.Payload["target_arn"], kmsARN; got != want {
		t.Fatalf("kms target_arn = %#v, want %q", got, want)
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{parameters: []Parameter{{
		ARN:   "arn:aws:ssm:us-east-1:123456789012:parameter/orders/db/password",
		Name:  "/orders/db/password",
		Type:  "SecureString",
		KeyID: "alias/ssm",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipSSMParameterUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/ssm"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSSM,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:ssm:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	parameters []Parameter
}

func (c fakeClient) ListParameters(context.Context) ([]Parameter, error) {
	return c.parameters, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func assertPolicyMetadata(t *testing.T, attributes map[string]any, want []map[string]string) {
	t.Helper()
	got, ok := attributes["policies"].([]map[string]string)
	if !ok {
		t.Fatalf("policies = %#v, want []map[string]string", attributes["policies"])
	}
	if len(got) != len(want) {
		t.Fatalf("len(policies) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i]["type"] != want[i]["type"] || got[i]["status"] != want[i]["status"] {
			t.Fatalf("policies[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
