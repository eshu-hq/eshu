// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kinesis

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsDataStreamFactsAndKMSRelationship(t *testing.T) {
	streamARN := "arn:aws:kinesis:us-east-1:123456789012:stream/orders"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	client := fakeClient{dataStreams: []DataStream{{
		ARN:               streamARN,
		Name:              "orders",
		Status:            "ACTIVE",
		StreamMode:        "ON_DEMAND",
		OpenShardCount:    4,
		RetentionHours:    48,
		EncryptionType:    "KMS",
		KMSKeyID:          kmsARN,
		CreationTimestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Tags:              map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	stream := resourceByType(t, envelopes, awscloud.ResourceTypeKinesisDataStream)
	attributes := attributesOf(t, stream)
	if got, want := attributes["open_shard_count"], int32(4); got != want {
		t.Fatalf("open_shard_count = %#v, want %v", got, want)
	}
	if got, want := attributes["retention_period_hours"], int32(48); got != want {
		t.Fatalf("retention_period_hours = %#v, want %v", got, want)
	}
	if got, want := attributes["encryption_type"], "KMS"; got != want {
		t.Fatalf("encryption_type = %#v, want %q", got, want)
	}
	if got, want := attributes["stream_mode"], "ON_DEMAND"; got != want {
		t.Fatalf("stream_mode = %#v, want %q", got, want)
	}
	assertNoRecordPlaneAttributes(t, attributes)
	if got, want := stream.Payload["arn"], streamARN; got != want {
		t.Fatalf("resource ARN = %#v, want %q", got, want)
	}
	assertRelationship(t, envelopes, awscloud.RelationshipKinesisDataStreamUsesKMSKey)
}

func TestScannerDataStreamOmitsKMSRelationshipForNonARNKey(t *testing.T) {
	client := fakeClient{dataStreams: []DataStream{{
		ARN:            "arn:aws:kinesis:us-east-1:123456789012:stream/orders",
		Name:           "orders",
		Status:         "ACTIVE",
		EncryptionType: "KMS",
		KMSKeyID:       "alias/aws/kinesis",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	assertNoRelationship(t, envelopes, awscloud.RelationshipKinesisDataStreamUsesKMSKey)
}

func TestScannerEmitsFirehoseFactsWithAllDestinationRelationships(t *testing.T) {
	streamARN := "arn:aws:firehose:us-east-1:123456789012:deliverystream/ingest"
	roleARN := "arn:aws:iam::123456789012:role/firehose-delivery"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:transform"
	bucketARN := "arn:aws:s3:::ingest-bucket"
	domainARN := "arn:aws:es:us-east-1:123456789012:domain/search"
	client := fakeClient{deliveryStreams: []FirehoseDeliveryStream{{
		ARN:                 streamARN,
		Name:                "ingest",
		Status:              "ACTIVE",
		StreamType:          "KinesisStreamAsSource",
		SourceKinesisStream: "arn:aws:kinesis:us-east-1:123456789012:stream/orders",
		EncryptionStatus:    "ENABLED",
		EncryptionKeyType:   "CUSTOMER_MANAGED_CMK",
		EncryptionKMSKeyARN: "arn:aws:kms:us-east-1:123456789012:key/abc",
		Destinations: []FirehoseDestination{
			{
				DestinationID:       "destinationId-1",
				Kind:                FirehoseDestinationKindS3,
				RoleARN:             roleARN,
				TransformLambdaARNs: []string{lambdaARN},
				S3BucketARN:         bucketARN,
			},
			{
				DestinationID:       "destinationId-2",
				Kind:                FirehoseDestinationKindOpenSearch,
				RoleARN:             roleARN,
				OpenSearchDomainARN: domainARN,
			},
			{
				DestinationID:  "destinationId-3",
				Kind:           FirehoseDestinationKindSplunk,
				SplunkEndpoint: "https://splunk.example.com:8088",
			},
			{
				DestinationID:    "destinationId-4",
				Kind:             FirehoseDestinationKindHTTPEndpoint,
				HTTPEndpointURL:  "https://collector.example.com/ingest",
				HTTPEndpointName: "partner-collector",
			},
			{
				DestinationID:     "destinationId-5",
				Kind:              FirehoseDestinationKindRedshift,
				RoleARN:           roleARN,
				RedshiftClusterID: "analytics-cluster",
			},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	delivery := resourceByType(t, envelopes, awscloud.ResourceTypeKinesisFirehoseDeliveryStream)
	attributes := attributesOf(t, delivery)
	if got, want := attributes["encryption_status"], "ENABLED"; got != want {
		t.Fatalf("encryption_status = %#v, want %q", got, want)
	}
	if got, want := attributes["delivery_stream_type"], "KinesisStreamAsSource"; got != want {
		t.Fatalf("delivery_stream_type = %#v, want %q", got, want)
	}
	if _, exists := attributes["http_endpoint_access_key"]; exists {
		t.Fatalf("http_endpoint_access_key persisted; Firehose scanner must not store HTTP endpoint access keys")
	}
	if _, exists := attributes["hec_token"]; exists {
		t.Fatalf("hec_token persisted; Firehose scanner must not store Splunk HEC tokens")
	}
	if _, exists := attributes["redshift_password"]; exists {
		t.Fatalf("redshift_password persisted; Firehose scanner must not store Redshift passwords")
	}
	if _, exists := attributes["processing_configuration"]; exists {
		t.Fatalf("processing_configuration persisted; Firehose scanner must not store the Lambda processing body")
	}

	assertRelationship(t, envelopes, awscloud.RelationshipFirehoseDeliveryStreamUsesIAMRole)
	assertRelationship(t, envelopes, awscloud.RelationshipFirehoseDeliveryStreamUsesLambdaTransform)
	assertRelationship(t, envelopes, awscloud.RelationshipFirehoseDeliveryStreamDeliversToS3)
	assertRelationship(t, envelopes, awscloud.RelationshipFirehoseDeliveryStreamDeliversToOpenSearch)
	assertRelationship(t, envelopes, awscloud.RelationshipFirehoseDeliveryStreamDeliversToSplunk)
	assertRelationship(t, envelopes, awscloud.RelationshipFirehoseDeliveryStreamDeliversToHTTPEndpoint)
	assertRelationship(t, envelopes, awscloud.RelationshipFirehoseDeliveryStreamDeliversToRedshift)

	// The shared role ARN appears on three destinations but must dedupe to one
	// IAM-role relationship to avoid inflating edge counts.
	if got := countRelationships(envelopes, awscloud.RelationshipFirehoseDeliveryStreamUsesIAMRole); got != 1 {
		t.Fatalf("IAM role relationships = %d, want 1 after dedupe", got)
	}
}

func TestScannerEmitsVideoStreamFactsAndKMSRelationship(t *testing.T) {
	streamARN := "arn:aws:kinesisvideo:us-east-1:123456789012:stream/door-cam/123"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/video-key"
	client := fakeClient{videoStreams: []VideoStream{{
		ARN:            streamARN,
		Name:           "door-cam",
		Status:         "ACTIVE",
		KMSKeyID:       kmsARN,
		MediaType:      "video/h264",
		RetentionHours: 24,
		Tags:           map[string]string{"Site": "hq"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	stream := resourceByType(t, envelopes, awscloud.ResourceTypeKinesisVideoStream)
	attributes := attributesOf(t, stream)
	if got, want := attributes["data_retention_hours"], int32(24); got != want {
		t.Fatalf("data_retention_hours = %#v, want %v", got, want)
	}
	if got, want := attributes["kms_key_id"], kmsARN; got != want {
		t.Fatalf("kms_key_id = %#v, want %q", got, want)
	}
	if got, want := attributes["media_type"], "video/h264"; got != want {
		t.Fatalf("media_type = %#v, want %q", got, want)
	}
	if _, exists := attributes["fragment"]; exists {
		t.Fatalf("fragment attribute persisted; video scanner must not read media fragments")
	}
	assertRelationship(t, envelopes, awscloud.RelationshipKinesisVideoStreamUsesKMSKey)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceMSK

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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceKinesis,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:kinesis:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	dataStreams     []DataStream
	deliveryStreams []FirehoseDeliveryStream
	videoStreams    []VideoStream
}

func (c fakeClient) ListDataStreams(context.Context) ([]DataStream, error) {
	return c.dataStreams, nil
}

func (c fakeClient) ListFirehoseDeliveryStreams(context.Context) ([]FirehoseDeliveryStream, error) {
	return c.deliveryStreams, nil
}

func (c fakeClient) ListVideoStreams(context.Context) ([]VideoStream, error) {
	return c.videoStreams, nil
}

func assertNoRecordPlaneAttributes(t *testing.T, attributes map[string]any) {
	t.Helper()
	for _, forbidden := range []string{"records", "data", "shard_iterator", "record"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("attribute %q persisted; data stream scanner must not read records", forbidden)
		}
	}
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	if countRelationships(envelopes, relationshipType) == 0 {
		t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	}
}

func assertNoRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	if countRelationships(envelopes, relationshipType) != 0 {
		t.Fatalf("unexpected relationship_type %q in %#v", relationshipType, envelopes)
	}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
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
