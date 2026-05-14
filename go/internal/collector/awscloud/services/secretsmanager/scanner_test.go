package secretsmanager

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsSecretsManagerMetadataOnlyFactsAndRelationships(t *testing.T) {
	secretARN := "arn:aws:secretsmanager:us-east-1:123456789012:secret:orders-db-a1b2c3"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	rotationARN := "arn:aws:lambda:us-east-1:123456789012:function:rotate-orders-db"
	client := fakeClient{secrets: []Secret{{
		ARN:                secretARN,
		Name:               "orders/db",
		DescriptionPresent: true,
		KMSKeyID:           kmsARN,
		RotationEnabled:    true,
		RotationLambdaARN:  rotationARN,
		CreatedAt:          time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastChangedAt:      time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC),
		LastRotatedAt:      time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC),
		NextRotationAt:     time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC),
		PrimaryRegion:      "us-east-1",
		OwningService:      "rds",
		SecretType:         "aws",
		RotationEveryDays:  30,
		RotationDuration:   "2h",
		RotationSchedule:   "rate(30 days)",
		Tags:               map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeSecretsManagerSecret)
	if got, want := resource.Payload["arn"], secretARN; got != want {
		t.Fatalf("secret arn = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["name"], "orders/db"; got != want {
		t.Fatalf("secret name = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, resource)
	assertAttribute(t, attributes, "description_present", true)
	assertAttribute(t, attributes, "kms_key_id", kmsARN)
	assertAttribute(t, attributes, "rotation_enabled", true)
	assertAttribute(t, attributes, "created_at", time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	assertAttribute(t, attributes, "last_changed_at", time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC))
	assertAttribute(t, attributes, "last_rotated_at", time.Date(2026, 5, 14, 14, 0, 0, 0, time.UTC))
	assertAttribute(t, attributes, "next_rotation_at", time.Date(2026, 6, 14, 14, 0, 0, 0, time.UTC))
	assertAttribute(t, attributes, "primary_region", "us-east-1")
	assertAttribute(t, attributes, "owning_service", "rds")
	assertAttribute(t, attributes, "secret_type", "aws")
	assertAttribute(t, attributes, "rotation_every_days", int64(30))
	assertAttribute(t, attributes, "rotation_duration", "2h")
	assertAttribute(t, attributes, "rotation_schedule", "rate(30 days)")
	for _, forbidden := range []string{
		"description",
		"secret_value",
		"secret_string",
		"secret_binary",
		"resource_policy",
		"secret_versions_to_stages",
		"version_ids_to_stages",
		"external_secret_rotation_metadata",
		"external_secret_rotation_role_arn",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; Secrets Manager scanner must stay metadata-only", forbidden)
		}
	}

	kmsRelationship := relationshipByType(t, envelopes, awscloud.RelationshipSecretsManagerSecretUsesKMSKey)
	if got, want := kmsRelationship.Payload["target_resource_id"], kmsARN; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got, want := kmsRelationship.Payload["target_arn"], kmsARN; got != want {
		t.Fatalf("kms target_arn = %#v, want %q", got, want)
	}

	rotationRelationship := relationshipByType(t, envelopes, awscloud.RelationshipSecretsManagerSecretUsesRotationLambda)
	if got, want := rotationRelationship.Payload["target_resource_id"], rotationARN; got != want {
		t.Fatalf("rotation target_resource_id = %#v, want %q", got, want)
	}
	if got, want := rotationRelationship.Payload["target_arn"], rotationARN; got != want {
		t.Fatalf("rotation target_arn = %#v, want %q", got, want)
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{secrets: []Secret{{
		ARN:      "arn:aws:secretsmanager:us-east-1:123456789012:secret:orders-db-a1b2c3",
		Name:     "orders/db",
		KMSKeyID: "alias/secrets",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipSecretsManagerSecretUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/secrets"; got != want {
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
		ServiceKind:         awscloud.ServiceSecretsManager,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:secretsmanager:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	secrets []Secret
}

func (c fakeClient) ListSecrets(context.Context) ([]Secret, error) {
	return c.secrets, nil
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
