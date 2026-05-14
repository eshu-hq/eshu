package dynamodb

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsDynamoDBMetadataOnlyFactsAndKMSRelationship(t *testing.T) {
	tableARN := "arn:aws:dynamodb:us-east-1:123456789012:table/orders"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	client := fakeClient{tables: []Table{{
		ARN:                       tableARN,
		Name:                      "orders",
		ID:                        "table-123",
		Status:                    "ACTIVE",
		CreationTime:              time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		BillingMode:               "PAY_PER_REQUEST",
		TableClass:                "STANDARD",
		ItemCount:                 42,
		TableSizeBytes:            1024,
		DeletionProtectionEnabled: true,
		KeySchema: []KeySchemaElement{{
			AttributeName: "tenant_id",
			KeyType:       "HASH",
		}, {
			AttributeName: "order_id",
			KeyType:       "RANGE",
		}},
		AttributeDefinitions: []AttributeDefinition{{
			AttributeName: "tenant_id",
			AttributeType: "S",
		}},
		ProvisionedThroughput: Throughput{
			ReadCapacityUnits:  5,
			WriteCapacityUnits: 10,
		},
		SSE: SSE{
			Status:          "ENABLED",
			Type:            "KMS",
			KMSMasterKeyARN: kmsARN,
		},
		TTL: TTL{
			Status:        "ENABLED",
			AttributeName: "expires_at",
		},
		ContinuousBackups: ContinuousBackups{
			Status:                    "ENABLED",
			PointInTimeRecoveryStatus: "ENABLED",
			RecoveryPeriodInDays:      35,
		},
		Stream: Stream{
			Enabled:         true,
			ViewType:        "NEW_AND_OLD_IMAGES",
			LatestStreamARN: "arn:aws:dynamodb:us-east-1:123456789012:table/orders/stream/2026-05-14T12:00:00.000",
			LatestLabel:     "2026-05-14T12:00:00.000",
		},
		GlobalSecondaryIndexes: []SecondaryIndex{{
			Name:           "by_status",
			ARN:            "arn:aws:dynamodb:us-east-1:123456789012:table/orders/index/by_status",
			Status:         "ACTIVE",
			ItemCount:      10,
			SizeBytes:      256,
			KeySchema:      []KeySchemaElement{{AttributeName: "status", KeyType: "HASH"}},
			ProjectionType: "KEYS_ONLY",
		}},
		LocalSecondaryIndexes: []SecondaryIndex{{
			Name:           "by_created_at",
			Status:         "ACTIVE",
			ItemCount:      4,
			SizeBytes:      128,
			KeySchema:      []KeySchemaElement{{AttributeName: "tenant_id", KeyType: "HASH"}},
			ProjectionType: "ALL",
		}},
		Replicas: []Replica{{
			RegionName:     "us-west-2",
			Status:         "ACTIVE",
			KMSMasterKeyID: "alias/orders-replica",
			TableClass:     "STANDARD",
		}},
		Tags: map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeDynamoDBTable)
	if got, want := resource.Payload["arn"], tableARN; got != want {
		t.Fatalf("table arn = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("table state = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, resource)
	assertAttribute(t, attributes, "table_id", "table-123")
	assertAttribute(t, attributes, "billing_mode", "PAY_PER_REQUEST")
	assertAttribute(t, attributes, "table_class", "STANDARD")
	assertAttribute(t, attributes, "item_count", int64(42))
	assertAttribute(t, attributes, "table_size_bytes", int64(1024))
	assertAttribute(t, attributes, "deletion_protection_enabled", true)
	assertAttribute(t, attributes, "sse_status", "ENABLED")
	assertAttribute(t, attributes, "sse_type", "KMS")
	assertAttribute(t, attributes, "ttl_status", "ENABLED")
	assertAttribute(t, attributes, "ttl_attribute_name", "expires_at")
	assertAttribute(t, attributes, "continuous_backups_status", "ENABLED")
	assertAttribute(t, attributes, "point_in_time_recovery_status", "ENABLED")
	assertAttribute(t, attributes, "recovery_period_in_days", int32(35))
	assertAttribute(t, attributes, "stream_enabled", true)
	assertAttribute(t, attributes, "stream_view_type", "NEW_AND_OLD_IMAGES")
	assertAttribute(t, attributes, "global_secondary_indexes", []map[string]any{{
		"name":            "by_status",
		"arn":             "arn:aws:dynamodb:us-east-1:123456789012:table/orders/index/by_status",
		"status":          "ACTIVE",
		"item_count":      int64(10),
		"size_bytes":      int64(256),
		"key_schema":      []map[string]string{{"attribute_name": "status", "key_type": "HASH"}},
		"projection_type": "KEYS_ONLY",
	}})
	assertAttribute(t, attributes, "local_secondary_indexes", []map[string]any{{
		"name":            "by_created_at",
		"status":          "ACTIVE",
		"item_count":      int64(4),
		"size_bytes":      int64(128),
		"key_schema":      []map[string]string{{"attribute_name": "tenant_id", "key_type": "HASH"}},
		"projection_type": "ALL",
	}})
	for _, forbidden := range []string{
		"items",
		"item_values",
		"scan_results",
		"query_results",
		"stream_records",
		"backup_payload",
		"export_payload",
		"resource_policy",
		"partiql",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; DynamoDB scanner must stay metadata-only", forbidden)
		}
	}

	relationship := relationshipByType(t, envelopes, awscloud.RelationshipDynamoDBTableUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], kmsARN; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got, want := relationship.Payload["target_arn"], kmsARN; got != want {
		t.Fatalf("kms target_arn = %#v, want %q", got, want)
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{tables: []Table{{
		ARN:  "arn:aws:dynamodb:us-east-1:123456789012:table/orders",
		Name: "orders",
		SSE: SSE{
			Status:          "ENABLED",
			Type:            "KMS",
			KMSMasterKeyARN: "alias/orders",
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipDynamoDBTableUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/orders"; got != want {
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
		ServiceKind:         awscloud.ServiceDynamoDB,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:dynamodb:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	tables []Table
}

func (c fakeClient) ListTables(context.Context) ([]Table, error) {
	return c.tables, nil
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
	case []map[string]any:
		gotMaps, ok := got.([]map[string]any)
		if !ok || len(gotMaps) != len(want) {
			return false
		}
		for i := range want {
			if !mapValuesEqual(gotMaps[i], want[i]) {
				return false
			}
		}
		return true
	case []map[string]string:
		gotMaps, ok := got.([]map[string]string)
		if !ok || len(gotMaps) != len(want) {
			return false
		}
		for i := range want {
			if !stringMapValuesEqual(gotMaps[i], want[i]) {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}

func mapValuesEqual(got map[string]any, want map[string]any) bool {
	for key, wantValue := range want {
		gotValue, exists := got[key]
		if !exists || !valuesEqual(gotValue, wantValue) {
			return false
		}
	}
	return true
}

func stringMapValuesEqual(got map[string]string, want map[string]string) bool {
	for key, wantValue := range want {
		gotValue, exists := got[key]
		if !exists || gotValue != wantValue {
			return false
		}
	}
	return true
}
