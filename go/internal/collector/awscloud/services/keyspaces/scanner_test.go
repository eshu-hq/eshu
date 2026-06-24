package keyspaces

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testKeyspaceARN = "arn:aws:cassandra:us-east-1:123456789012:/keyspace/orders/"
	testTableARN    = "arn:aws:cassandra:us-east-1:123456789012:/keyspace/orders/table/events"
	testKMSARN      = "arn:aws:kms:us-east-1:123456789012:key/abcd-1234"
)

func TestScannerEmitsKeyspaceAndTableMetadataWithEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Keyspaces: []Keyspace{{
			Name:                "orders",
			ARN:                 testKeyspaceARN,
			ReplicationStrategy: "SINGLE_REGION",
			ReplicationRegions:  []string{"us-east-1"},
		}},
		Tables: []Table{{
			ARN:                  testTableARN,
			Name:                 "events",
			KeyspaceName:         "orders",
			KeyspaceARN:          testKeyspaceARN,
			Status:               "ACTIVE",
			CreationTime:         time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			DefaultTimeToLive:    3600,
			CapacityMode:         "PROVISIONED",
			ReadCapacityUnits:    5,
			WriteCapacityUnits:   10,
			TimeToLiveStatus:     "ENABLED",
			ClientSideTimestamps: "ENABLED",
			CDCStatus:            "ENABLED",
			Comment:              "order events",
			Encryption: Encryption{
				Type:             "CUSTOMER_MANAGED_KMS_KEY",
				KMSKeyIdentifier: testKMSARN,
			},
			PointInTimeRecovery: PointInTimeRecovery{Status: "ENABLED"},
			Schema: Schema{
				Columns: []Column{
					{Name: "tenant_id", Type: "uuid"},
					{Name: "event_id", Type: "timeuuid"},
					{Name: "payload", Type: "text"},
				},
				PartitionKeys:  []string{"tenant_id"},
				ClusteringKeys: []ClusteringKey{{Name: "event_id", OrderBy: "ASC"}},
				StaticColumns:  []string{"tenant_name"},
			},
			Tags: map[string]string{"Environment": "prod"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	keyspace := resourceByType(t, envelopes, awscloud.ResourceTypeKeyspacesKeyspace)
	if got, want := keyspace.Payload["resource_id"], testKeyspaceARN; got != want {
		t.Fatalf("keyspace resource_id = %#v, want %q", got, want)
	}
	if got, want := keyspace.Payload["arn"], testKeyspaceARN; got != want {
		t.Fatalf("keyspace arn = %#v, want %q", got, want)
	}
	ksAttrs := attributesOf(t, keyspace)
	assertAttribute(t, ksAttrs, "replication_strategy", "SINGLE_REGION")

	table := resourceByType(t, envelopes, awscloud.ResourceTypeKeyspacesTable)
	if got, want := table.Payload["resource_id"], testTableARN; got != want {
		t.Fatalf("table resource_id = %#v, want %q", got, want)
	}
	if got, want := table.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("table state = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, table)
	assertAttribute(t, attrs, "keyspace_name", "orders")
	assertAttribute(t, attrs, "keyspace_arn", testKeyspaceARN)
	assertAttribute(t, attrs, "capacity_mode", "PROVISIONED")
	assertAttribute(t, attrs, "read_capacity_units", int64(5))
	assertAttribute(t, attrs, "encryption_type", "CUSTOMER_MANAGED_KMS_KEY")
	assertAttribute(t, attrs, "point_in_time_recovery", "ENABLED")
	assertAttribute(t, attrs, "ttl_status", "ENABLED")
	// Schema column NAMES and types are structural metadata, which is allowed.
	assertAttribute(t, attrs, "schema_columns", []map[string]string{
		{"name": "tenant_id", "type": "uuid"},
		{"name": "event_id", "type": "timeuuid"},
		{"name": "payload", "type": "text"},
	})
	assertAttribute(t, attrs, "schema_partition_keys", []string{"tenant_id"})
	assertAttribute(t, attrs, "schema_clustering_keys", []map[string]string{
		{"name": "event_id", "order_by": "ASC"},
	})
	assertAttribute(t, attrs, "schema_static_columns", []string{"tenant_name"})

	// Row data and any data-plane payloads must never be persisted.
	for _, forbidden := range []string{
		"rows",
		"row_data",
		"cells",
		"cell_values",
		"items",
		"select_results",
		"query_results",
		"execute_statement",
		"statement_results",
		"cql_results",
	} {
		if _, exists := attrs[forbidden]; exists {
			t.Fatalf("%s attribute persisted; Keyspaces scanner must stay metadata-only", forbidden)
		}
	}

	inKeyspace := relationshipByType(t, envelopes, awscloud.RelationshipKeyspacesTableInKeyspace)
	if got, want := inKeyspace.Payload["target_resource_id"], testKeyspaceARN; got != want {
		t.Fatalf("table-in-keyspace target_resource_id = %#v, want %q", got, want)
	}
	if got, want := inKeyspace.Payload["target_type"], awscloud.ResourceTypeKeyspacesKeyspace; got != want {
		t.Fatalf("table-in-keyspace target_type = %#v, want %q", got, want)
	}
	if got, want := inKeyspace.Payload["target_arn"], testKeyspaceARN; got != want {
		t.Fatalf("table-in-keyspace target_arn = %#v, want %q", got, want)
	}

	usesKMS := relationshipByType(t, envelopes, awscloud.RelationshipKeyspacesTableUsesKMSKey)
	if got, want := usesKMS.Payload["target_resource_id"], testKMSARN; got != want {
		t.Fatalf("table-uses-kms target_resource_id = %#v, want %q", got, want)
	}
	if got, want := usesKMS.Payload["target_type"], awscloud.ResourceTypeKMSKey; got != want {
		t.Fatalf("table-uses-kms target_type = %#v, want %q", got, want)
	}
	if got, want := usesKMS.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("table-uses-kms target_arn = %#v, want %q", got, want)
	}

	relguard.AssertObservations(
		t,
		*tableKeyspaceRelationship(testBoundary(), client.snapshot.Tables[0]),
		*tableKMSRelationship(testBoundary(), client.snapshot.Tables[0]),
	)
}

func TestScannerOmitsKMSEdgeForAWSOwnedKey(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Tables: []Table{{
		ARN:          testTableARN,
		Name:         "events",
		KeyspaceName: "orders",
		KeyspaceARN:  testKeyspaceARN,
		Encryption:   Encryption{Type: "AWS_OWNED_KMS_KEY"},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipKeyspacesTableUsesKMSKey {
			t.Fatalf("KMS edge emitted for AWS-owned key; want none")
		}
	}
	// The table-in-keyspace edge must still be present.
	relationshipByType(t, envelopes, awscloud.RelationshipKeyspacesTableInKeyspace)
}

func TestTableKeyspaceRelationshipDerivesPartitionFromTableARN(t *testing.T) {
	cases := []struct {
		name      string
		tableARN  string
		wantKSARN string
	}{
		{
			name:      "commercial",
			tableARN:  "arn:aws:cassandra:us-east-1:123456789012:/keyspace/orders/table/events",
			wantKSARN: "arn:aws:cassandra:us-east-1:123456789012:/keyspace/orders/",
		},
		{
			name:      "govcloud",
			tableARN:  "arn:aws-us-gov:cassandra:us-gov-west-1:123456789012:/keyspace/orders/table/events",
			wantKSARN: "arn:aws-us-gov:cassandra:us-gov-west-1:123456789012:/keyspace/orders/",
		},
		{
			name:      "china",
			tableARN:  "arn:aws-cn:cassandra:cn-north-1:123456789012:/keyspace/orders/table/events",
			wantKSARN: "arn:aws-cn:cassandra:cn-north-1:123456789012:/keyspace/orders/",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// KeyspaceARN intentionally left empty so the edge derives it from the
			// table ARN, proving the partition is inherited, never hardcoded.
			table := Table{ARN: tc.tableARN, Name: "events", KeyspaceName: "orders"}
			obs := tableKeyspaceRelationship(testBoundary(), table)
			if obs == nil {
				t.Fatalf("tableKeyspaceRelationship returned nil for a valid table ARN")
			}
			if obs.TargetResourceID != tc.wantKSARN {
				t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, tc.wantKSARN)
			}
			if obs.TargetARN != tc.wantKSARN {
				t.Fatalf("target_arn = %q, want %q", obs.TargetARN, tc.wantKSARN)
			}
			if obs.TargetType != awscloud.ResourceTypeKeyspacesKeyspace {
				t.Fatalf("target_type = %q, want %q", obs.TargetType, awscloud.ResourceTypeKeyspacesKeyspace)
			}
		})
	}
}

func TestTableKMSRelationshipDoesNotTreatNonARNIdentifierAsARN(t *testing.T) {
	table := Table{
		ARN:        testTableARN,
		Name:       "events",
		Encryption: Encryption{Type: "CUSTOMER_MANAGED_KMS_KEY", KMSKeyIdentifier: "abcd-1234"},
	}
	obs := tableKMSRelationship(testBoundary(), table)
	if obs == nil {
		t.Fatalf("tableKMSRelationship returned nil for a customer-managed key")
	}
	if obs.TargetResourceID != "abcd-1234" {
		t.Fatalf("target_resource_id = %q, want %q", obs.TargetResourceID, "abcd-1234")
	}
	if obs.TargetARN != "" {
		t.Fatalf("target_arn = %q, want empty for non-ARN KMS identifier", obs.TargetARN)
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

func TestScannerEmitsWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Keyspaces: []Keyspace{{Name: "orders", ARN: testKeyspaceARN, ReplicationStrategy: "SINGLE_REGION"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Keyspaces GetTable throttled after SDK retries; table metadata omitted for this scan",
			SourceRecordID: "keyspaces_get_table_throttled",
			Attributes:     map[string]any{"operation": "GetTable"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
	resourceByType(t, envelopes, awscloud.ResourceTypeKeyspacesKeyspace)
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceKeyspaces,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:keyspaces:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
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

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
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
	case []string:
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}

func stringMapValuesEqual(got map[string]string, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	for key, wantValue := range want {
		gotValue, exists := got[key]
		if !exists || gotValue != wantValue {
			return false
		}
	}
	return true
}
