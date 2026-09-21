// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firehose

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsFirehoseMetadataResourceAndRelationships(t *testing.T) {
	streamName := "orders-firehose"
	streamARN := "arn:aws:firehose:us-east-1:123456789012:deliverystream/orders-firehose"
	sourceStreamARN := "arn:aws:kinesis:us-east-1:123456789012:stream/orders-raw"
	roleARN := "arn:aws:iam::123456789012:role/firehose-orders-delivery"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	bucketARN := "arn:aws:s3:::orders-firehose-dest"
	domainARN := "arn:aws:es:us-east-1:123456789012:domain/orders-search"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:orders-transform"
	logGroupName := "/aws/kinesisfirehose/orders-firehose"

	client := fakeClient{streams: []DeliveryStream{{
		Name:                   streamName,
		ARN:                    streamARN,
		Status:                 "ACTIVE",
		StreamType:             "KinesisStreamAsSource",
		SourceType:             "kinesis_stream",
		SourceKinesisStreamARN: sourceStreamARN,
		EncryptionMode:         "CUSTOMER_MANAGED_CMK",
		EncryptionStatus:       "ENABLED",
		EncryptionKMSKeyARN:    kmsKeyARN,
		CreationTimestamp:      time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
		Tags:                   map[string]string{"team": "data"},
		Destinations: []Destination{
			{
				Kind:                destinationKindS3,
				RoleARN:             roleARN,
				S3BucketARN:         bucketARN,
				LogGroupName:        logGroupName,
				TransformLambdaARNs: []string{lambdaARN},
			},
			{
				Kind:                destinationKindOpenSearch,
				RoleARN:             roleARN,
				OpenSearchDomainARN: domainARN,
				LogGroupName:        logGroupName,
			},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	stream := resourceByType(t, envelopes, awscloud.ResourceTypeFirehoseDeliveryStream)
	if got, want := stream.Payload["name"], streamName; got != want {
		t.Fatalf("stream name = %#v, want %q", got, want)
	}
	if got, want := stream.Payload["resource_id"], streamARN; got != want {
		t.Fatalf("stream resource_id = %#v, want %q", got, want)
	}
	if got, want := stream.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("stream state = %#v, want %q", got, want)
	}
	streamAttributes := attributesOf(t, stream)
	if got, want := streamAttributes["delivery_stream_type"], "KinesisStreamAsSource"; got != want {
		t.Fatalf("stream delivery_stream_type = %#v, want %q", got, want)
	}
	if got, want := streamAttributes["encryption_mode"], "CUSTOMER_MANAGED_CMK"; got != want {
		t.Fatalf("stream encryption_mode = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{
		"records", "splunk_hec_token", "http_endpoint_access_key", "redshift_password",
		"processing_configuration", "lambda_body", "endpoint_url",
	} {
		if _, exists := streamAttributes[forbidden]; exists {
			t.Fatalf("stream %s attribute persisted; metadata-only contract forbids record/secret payloads", forbidden)
		}
	}

	source := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamSourcedFromKinesisStream)
	assertEdge(t, source, streamARN, sourceStreamARN, sourceStreamARN, awscloud.ResourceTypeKinesisDataStream)

	kms := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamUsesKMSKey)
	assertEdge(t, kms, streamARN, kmsKeyARN, kmsKeyARN, awscloud.ResourceTypeKMSKey)

	role := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamUsesIAMRole)
	assertEdge(t, role, streamARN, roleARN, roleARN, awscloud.ResourceTypeIAMRole)
	if got := countRelationships(envelopes, awscloud.RelationshipFirehoseStreamUsesIAMRole); got != 1 {
		t.Fatalf("role edge count = %d, want 1 (duplicate role across destinations must collapse)", got)
	}

	logGroup := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamLogsToCloudWatchLogGroup)
	assertEdge(t, logGroup, streamARN, logGroupName, "", awscloud.ResourceTypeCloudWatchLogsLogGroup)
	if got := countRelationships(envelopes, awscloud.RelationshipFirehoseStreamLogsToCloudWatchLogGroup); got != 1 {
		t.Fatalf("log-group edge count = %d, want 1 (duplicate log group must collapse)", got)
	}

	lambda := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamUsesLambdaTransform)
	assertEdge(t, lambda, streamARN, lambdaARN, lambdaARN, awscloud.ResourceTypeLambdaFunction)

	s3 := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamDeliversToS3Bucket)
	assertEdge(t, s3, streamARN, bucketARN, bucketARN, awscloud.ResourceTypeS3Bucket)

	opensearch := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamDeliversToOpenSearchDomain)
	assertEdge(t, opensearch, streamARN, domainARN, domainARN, awscloud.ResourceTypeOpenSearchDomain)
}

func TestScannerEmitsRedshiftDestinationEdgeKeyedByClusterIdentifier(t *testing.T) {
	streamARN := "arn:aws:firehose:us-east-1:123456789012:deliverystream/analytics"
	client := fakeClient{streams: []DeliveryStream{{
		Name: "analytics",
		ARN:  streamARN,
		Destinations: []Destination{{
			Kind:                      destinationKindRedshift,
			RoleARN:                   "arn:aws:iam::123456789012:role/firehose-redshift",
			RedshiftClusterIdentifier: "warehouse-prod",
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	redshift := relationshipByType(t, envelopes, awscloud.RelationshipFirehoseStreamDeliversToRedshiftCluster)
	if got, want := redshift.Payload["target_resource_id"], "warehouse-prod"; got != want {
		t.Fatalf("redshift target_resource_id = %#v, want %q (bare cluster id, the Redshift scanner Name)", got, want)
	}
	if got, want := redshift.Payload["target_type"], awscloud.ResourceTypeRedshiftCluster; got != want {
		t.Fatalf("redshift target_type = %#v, want %q", got, want)
	}
	if _, exists := redshift.Payload["target_arn"]; exists {
		if arn, _ := redshift.Payload["target_arn"].(string); arn != "" {
			t.Fatalf("redshift edge set a fabricated target_arn %q; AWS reports a JDBC host, not an ARN", arn)
		}
	}
}

func TestScannerOmitsDestinationEdgesWhenTargetIdentityMissing(t *testing.T) {
	client := fakeClient{streams: []DeliveryStream{{
		Name: "incomplete",
		ARN:  "arn:aws:firehose:us-east-1:123456789012:deliverystream/incomplete",
		Destinations: []Destination{
			{Kind: destinationKindS3, S3BucketARN: "not-an-arn"},
			{Kind: destinationKindOpenSearch, OpenSearchDomainARN: ""},
			{Kind: destinationKindRedshift, RedshiftClusterIdentifier: ""},
			{Kind: destinationKindSplunk},
			{Kind: destinationKindHTTPEndpoint},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipFirehoseStreamDeliversToS3Bucket,
		awscloud.RelationshipFirehoseStreamDeliversToOpenSearchDomain,
		awscloud.RelationshipFirehoseStreamDeliversToRedshiftCluster,
	} {
		if got := countRelationships(envelopes, relationshipType); got != 0 {
			t.Fatalf("relationship %q count = %d, want 0 when target identity is missing", relationshipType, got)
		}
	}
}

func TestScannerOmitsKMSEdgeForAWSOwnedKey(t *testing.T) {
	client := fakeClient{streams: []DeliveryStream{{
		Name:                "aws-owned",
		ARN:                 "arn:aws:firehose:us-east-1:123456789012:deliverystream/aws-owned",
		EncryptionMode:      "AWS_OWNED_CMK",
		EncryptionStatus:    "ENABLED",
		EncryptionKMSKeyARN: "",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipFirehoseStreamUsesKMSKey); got != 0 {
		t.Fatalf("kms edge count = %d, want 0 for an AWS-owned key (no customer key ARN)", got)
	}
}

func TestScannerOmitsSourceEdgeForDirectPutStream(t *testing.T) {
	client := fakeClient{streams: []DeliveryStream{{
		Name:       "direct",
		ARN:        "arn:aws:firehose:us-east-1:123456789012:deliverystream/direct",
		StreamType: "DirectPut",
		SourceType: "direct_put",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipFirehoseStreamSourcedFromKinesisStream); got != 0 {
		t.Fatalf("source edge count = %d, want 0 for a DirectPut stream", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	client := fakeClient{streams: []DeliveryStream{{
		Name: "full",
		ARN:  "arn:aws:firehose:us-east-1:123456789012:deliverystream/full",
		Destinations: []Destination{
			{
				Kind:                      destinationKindRedshift,
				RoleARN:                   "arn:aws:iam::123456789012:role/r",
				RedshiftClusterIdentifier: "warehouse",
				LogGroupName:              "/aws/kinesisfirehose/full",
				TransformLambdaARNs:       []string{"arn:aws:lambda:us-east-1:123456789012:function:t"},
			},
		},
		SourceKinesisStreamARN: "arn:aws:kinesis:us-east-1:123456789012:stream/raw",
		EncryptionKMSKeyARN:    "arn:aws:kms:us-east-1:123456789012:key/abc",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relguard.AssertObservations(t, relationshipObservations(t, envelopes)...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceKinesis

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

// TestStagingS3RelationshipForNonS3Destination guards the staging-bucket join: a
// non-S3 destination (Redshift/Splunk/HTTP) that reports a backup/staging S3
// bucket must still emit a stream->S3 edge, while an S3-kind destination's bucket
// is handled by the primary edge and must not double-emit here.
func TestStagingS3RelationshipForNonS3Destination(t *testing.T) {
	const streamARN = "arn:aws:firehose:us-east-1:123456789012:deliverystream/stream-1"
	edge, ok := stagingS3Relationship(testBoundary(), "stream-1", streamARN,
		Destination{Kind: destinationKindRedshift, S3BucketARN: "arn:aws:s3:::staging-bucket", RedshiftClusterIdentifier: "warehouse"},
		map[string]struct{}{})
	if !ok {
		t.Fatal("expected a staging S3 edge for a Redshift destination with a backup bucket")
	}
	if edge.TargetType != awscloud.ResourceTypeS3Bucket || edge.TargetResourceID != "arn:aws:s3:::staging-bucket" {
		t.Fatalf("staging edge = %+v, want S3 bucket arn:aws:s3:::staging-bucket", edge)
	}
	if _, ok := stagingS3Relationship(testBoundary(), "stream-1", streamARN,
		Destination{Kind: destinationKindS3, S3BucketARN: "arn:aws:s3:::primary"}, map[string]struct{}{}); ok {
		t.Fatal("S3-kind destination must not emit a staging edge (its primary edge covers it)")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceFirehose,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:firehose:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	streams []DeliveryStream
}

func (c fakeClient) ListDeliveryStreams(context.Context) ([]DeliveryStream, error) {
	return c.streams, nil
}

func assertEdge(t *testing.T, envelope facts.Envelope, sourceID, targetID, targetARN, targetType string) {
	t.Helper()
	if got, want := envelope.Payload["source_resource_id"], sourceID; got != want {
		t.Fatalf("edge source_resource_id = %#v, want %q", got, want)
	}
	if got, want := envelope.Payload["target_resource_id"], targetID; got != want {
		t.Fatalf("edge target_resource_id = %#v, want %q", got, want)
	}
	if got, want := envelope.Payload["target_type"], targetType; got != want {
		t.Fatalf("edge target_type = %#v, want %q", got, want)
	}
	if targetARN != "" {
		if got, want := envelope.Payload["target_arn"], targetARN; got != want {
			t.Fatalf("edge target_arn = %#v, want %q", got, want)
		}
	}
}

func relationshipObservations(t *testing.T, envelopes []facts.Envelope) []awscloud.RelationshipObservation {
	t.Helper()
	var observations []awscloud.RelationshipObservation
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		relationshipType, _ := envelope.Payload["relationship_type"].(string)
		targetType, _ := envelope.Payload["target_type"].(string)
		sourceID, _ := envelope.Payload["source_resource_id"].(string)
		targetID, _ := envelope.Payload["target_resource_id"].(string)
		targetARN, _ := envelope.Payload["target_arn"].(string)
		observations = append(observations, awscloud.RelationshipObservation{
			RelationshipType: relationshipType,
			TargetType:       targetType,
			SourceResourceID: sourceID,
			TargetResourceID: targetID,
			TargetARN:        targetARN,
		})
	}
	return observations
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
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
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
	t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
