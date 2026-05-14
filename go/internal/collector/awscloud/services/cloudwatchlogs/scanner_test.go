package cloudwatchlogs

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsCloudWatchLogsMetadataOnlyFactsAndKMSRelationship(t *testing.T) {
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/orders"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/logs"
	client := fakeClient{logGroups: []LogGroup{{
		ARN:                  logGroupARN,
		Name:                 "/aws/lambda/orders",
		CreationTime:         time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		RetentionInDays:      30,
		StoredBytes:          2048,
		MetricFilterCount:    2,
		LogGroupClass:        "STANDARD",
		DataProtectionStatus: "ACTIVATED",
		InheritedProperties:  []string{"ACCOUNT_DATA_PROTECTION"},
		KMSKeyID:             kmsARN,
		DeletionProtected:    true,
		BearerTokenAuth:      true,
		Tags:                 map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeCloudWatchLogsLogGroup)
	if got, want := resource.Payload["arn"], logGroupARN; got != want {
		t.Fatalf("log group arn = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["name"], "/aws/lambda/orders"; got != want {
		t.Fatalf("log group name = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, resource)
	assertAttribute(t, attributes, "creation_time", time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC))
	assertAttribute(t, attributes, "retention_in_days", int32(30))
	assertAttribute(t, attributes, "stored_bytes", int64(2048))
	assertAttribute(t, attributes, "metric_filter_count", int32(2))
	assertAttribute(t, attributes, "log_group_class", "STANDARD")
	assertAttribute(t, attributes, "data_protection_status", "ACTIVATED")
	assertAttribute(t, attributes, "inherited_properties", []string{"ACCOUNT_DATA_PROTECTION"})
	assertAttribute(t, attributes, "kms_key_id", kmsARN)
	assertAttribute(t, attributes, "deletion_protected", true)
	assertAttribute(t, attributes, "bearer_token_authentication_enabled", true)
	for _, forbidden := range []string{
		"log_events",
		"log_streams",
		"log_stream_payloads",
		"insights_query_results",
		"resource_policy",
		"export_task",
		"subscription_filter_payloads",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; CloudWatch Logs scanner must stay metadata-only", forbidden)
		}
	}

	relationship := relationshipByType(t, envelopes, awscloud.RelationshipCloudWatchLogsLogGroupUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], kmsARN; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got, want := relationship.Payload["target_arn"], kmsARN; got != want {
		t.Fatalf("kms target_arn = %#v, want %q", got, want)
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{logGroups: []LogGroup{{
		ARN:      "arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/orders",
		Name:     "/aws/lambda/orders",
		KMSKeyID: "alias/logs",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipCloudWatchLogsLogGroupUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/logs"; got != want {
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
		ServiceKind:         awscloud.ServiceCloudWatchLogs,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:cloudwatchlogs:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	logGroups []LogGroup
}

func (c fakeClient) ListLogGroups(context.Context) ([]LogGroup, error) {
	return c.logGroups, nil
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
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotStrings, ok := got.([]string)
		if !ok || len(gotStrings) != len(want) {
			return false
		}
		for i := range want {
			if gotStrings[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
