// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package timestream

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testDatabaseARN = "arn:aws:timestream:us-east-1:123456789012:database/metrics"
	testTableARN    = "arn:aws:timestream:us-east-1:123456789012:database/metrics/table/cpu"
	testKMSARN      = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
)

func TestScannerEmitsTimestreamMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Databases: []Database{{
		ARN:             testDatabaseARN,
		Name:            "metrics",
		KMSKeyID:        testKMSARN,
		TableCount:      1,
		CreationTime:    time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		LastUpdatedTime: time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
		Tags:            map[string]string{"Environment": "prod"},
		Tables: []Table{{
			ARN:                                testTableARN,
			Name:                               "cpu",
			DatabaseName:                       "metrics",
			State:                              "ACTIVE",
			MemoryStoreRetentionPeriodInHours:  24,
			MagneticStoreRetentionPeriodInDays: 365,
			MagneticStoreWritesEnabled:         true,
			RejectedDataS3Bucket:               "rejected-data-bucket",
			RejectedDataS3Prefix:               "errors/",
			RejectedDataS3EncryptionOption:     "SSE_KMS",
			PartitionKeyNames:                  []string{"host"},
			CreationTime:                       time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
			Tags:                               map[string]string{"Team": "observability"},
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Database resource node.
	database := resourceByType(t, envelopes, awscloud.ResourceTypeTimestreamDatabase)
	if got, want := database.Payload["resource_id"], testDatabaseARN; got != want {
		t.Fatalf("database resource_id = %#v, want %q", got, want)
	}
	if got, want := database.Payload["arn"], testDatabaseARN; got != want {
		t.Fatalf("database arn = %#v, want %q", got, want)
	}
	dbAttrs := attributesOf(t, database)
	assertAttribute(t, dbAttrs, "kms_key_id", testKMSARN)
	assertAttribute(t, dbAttrs, "table_count", int64(1))
	assertAttribute(t, dbAttrs, "database_name", "metrics")

	// Table resource node.
	table := resourceByType(t, envelopes, awscloud.ResourceTypeTimestreamTable)
	if got, want := table.Payload["resource_id"], testTableARN; got != want {
		t.Fatalf("table resource_id = %#v, want %q", got, want)
	}
	if got, want := table.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("table state = %#v, want %q", got, want)
	}
	tableAttrs := attributesOf(t, table)
	assertAttribute(t, tableAttrs, "memory_store_retention_period_in_hours", int64(24))
	assertAttribute(t, tableAttrs, "magnetic_store_retention_period_in_days", int64(365))
	assertAttribute(t, tableAttrs, "magnetic_store_writes_enabled", true)
	assertAttribute(t, tableAttrs, "rejected_data_s3_bucket", "rejected-data-bucket")
	assertAttribute(t, tableAttrs, "partition_key_names", []string{"host"})

	// table -> database edge, keyed by the database ARN the database node publishes.
	tableInDB := relationshipByType(t, envelopes, awscloud.RelationshipTimestreamTableInDatabase)
	assertEdgeTarget(t, tableInDB, awscloud.ResourceTypeTimestreamDatabase, testDatabaseARN)
	if got, want := tableInDB.Payload["source_resource_id"], testTableARN; got != want {
		t.Fatalf("table->database source_resource_id = %#v, want %q", got, want)
	}
	if got, want := tableInDB.Payload["target_arn"], testDatabaseARN; got != want {
		t.Fatalf("table->database target_arn = %#v, want %q", got, want)
	}

	// database -> KMS key edge.
	dbKMS := relationshipByType(t, envelopes, awscloud.RelationshipTimestreamDatabaseUsesKMSKey)
	assertEdgeTarget(t, dbKMS, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := dbKMS.Payload["source_resource_id"], testDatabaseARN; got != want {
		t.Fatalf("database->kms source_resource_id = %#v, want %q", got, want)
	}
	if got, want := dbKMS.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("database->kms target_arn = %#v, want %q", got, want)
	}

	// table -> S3 bucket edge, keyed by the synthesized partition-aware ARN the
	// S3 scanner publishes for a bucket node.
	tableS3 := relationshipByType(t, envelopes, awscloud.RelationshipTimestreamTableRejectsToS3)
	wantBucketARN := "arn:aws:s3:::rejected-data-bucket"
	assertEdgeTarget(t, tableS3, awscloud.ResourceTypeS3Bucket, wantBucketARN)
	if got, want := tableS3.Payload["source_resource_id"], testTableARN; got != want {
		t.Fatalf("table->s3 source_resource_id = %#v, want %q", got, want)
	}
	if got, want := tableS3.Payload["target_arn"], wantBucketARN; got != want {
		t.Fatalf("table->s3 target_arn = %#v, want %q", got, want)
	}

	// No records / measures / query leakage anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"records", "record_values", "measures", "measure_values",
			"query_results", "rows", "data_points", "time_series",
			"rejected_records", "magnetic_store_records",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Timestream scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{Databases: []Database{{
		ARN:  "arn:aws-us-gov:timestream:us-gov-west-1:123456789012:database/metrics",
		Name: "metrics",
		Tables: []Table{{
			ARN:                  "arn:aws-us-gov:timestream:us-gov-west-1:123456789012:database/metrics/table/cpu",
			Name:                 "cpu",
			DatabaseName:         "metrics",
			RejectedDataS3Bucket: "gov-rejected-bucket",
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	tableS3 := relationshipByType(t, envelopes, awscloud.RelationshipTimestreamTableRejectsToS3)
	wantARN := "arn:aws-us-gov:s3:::gov-rejected-bucket"
	if got := tableS3.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud table->s3 target_resource_id = %#v, want %q", got, wantARN)
	}
	if got := tableS3.Payload["target_arn"]; got != wantARN {
		t.Fatalf("GovCloud table->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	client := fakeClient{snapshot: Snapshot{Databases: []Database{{
		ARN:  "arn:aws-cn:timestream:cn-north-1:123456789012:database/metrics",
		Name: "metrics",
		Tables: []Table{{
			ARN:                  "arn:aws-cn:timestream:cn-north-1:123456789012:database/metrics/table/cpu",
			Name:                 "cpu",
			DatabaseName:         "metrics",
			RejectedDataS3Bucket: "cn-rejected-bucket",
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	tableS3 := relationshipByType(t, envelopes, awscloud.RelationshipTimestreamTableRejectsToS3)
	wantARN := "arn:aws-cn:s3:::cn-rejected-bucket"
	if got := tableS3.Payload["target_arn"]; got != wantARN {
		t.Fatalf("China table->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Databases: []Database{{
		ARN:  testDatabaseARN,
		Name: "metrics",
		// No KMS key, no tables: no KMS edge, no table edges.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsKMSEdgeForNonARNKeyButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Databases: []Database{{
		ARN:      testDatabaseARN,
		Name:     "metrics",
		KMSKeyID: "alias/timestream-metrics",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	dbKMS := relationshipByType(t, envelopes, awscloud.RelationshipTimestreamDatabaseUsesKMSKey)
	if got, want := dbKMS.Payload["target_resource_id"], "alias/timestream-metrics"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := dbKMS.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for non-ARN key identifier", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	database := Database{ARN: testDatabaseARN, Name: "metrics", KMSKeyID: testKMSARN}
	databaseID := databaseResourceID(database)
	table := Table{
		ARN:                  testTableARN,
		Name:                 "cpu",
		DatabaseName:         "metrics",
		RejectedDataS3Bucket: "rejected-data-bucket",
	}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		databaseKMSRelationship(boundary, database),
		tableInDatabaseRelationship(boundary, databaseID, table),
		tableRejectedDataS3Relationship(boundary, table),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Databases: []Database{{ARN: testDatabaseARN, Name: "metrics"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Timestream ListTables throttled after SDK retries; table metadata omitted for this scan",
			SourceRecordID: "timestream_tables_throttled",
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
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceTimestream,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:timestream:1",
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

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
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
