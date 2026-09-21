// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package firehose

import (
	"context"
	"time"
)

// Client lists metadata-only Amazon Data Firehose observations for one claimed
// account and region. Implementations adapt the AWS SDK; the scanner depends on
// this small surface so tests can supply fakes and the SDK adapter owns
// pagination and telemetry.
type Client interface {
	// ListDeliveryStreams returns one DeliveryStream per Firehose delivery
	// stream in the boundary, already described into scanner-owned metadata.
	ListDeliveryStreams(ctx context.Context) ([]DeliveryStream, error)
}

// DeliveryStream is the scanner-owned view of one Amazon Data Firehose delivery
// stream. It carries safe identity, status, source, encryption, and destination
// metadata. Delivery records, destination access keys, Splunk HEC tokens,
// Redshift passwords, and processing-configuration Lambda bodies stay outside
// the contract.
type DeliveryStream struct {
	// Name is the Firehose delivery stream name.
	Name string
	// ARN is the Firehose delivery stream ARN as AWS reports it.
	ARN string
	// Status is the delivery stream status (for example ACTIVE, CREATING).
	Status string
	// StreamType is the delivery stream type (DirectPut or
	// KinesisStreamAsSource).
	StreamType string
	// SourceType classifies the stream source (kinesis_stream, direct_put,
	// msk, database, or "" when AWS reports none).
	SourceType string
	// SourceKinesisStreamARN is the source Kinesis data stream ARN when the
	// stream type is KinesisStreamAsSource. It is empty for DirectPut streams.
	SourceKinesisStreamARN string
	// EncryptionMode is the server-side encryption mode AWS reports
	// (for example AWS_OWNED_CMK, CUSTOMER_MANAGED_CMK, or "" when disabled).
	EncryptionMode string
	// EncryptionStatus is the server-side encryption status AWS reports
	// (for example ENABLED, DISABLED) used to record the SSE posture.
	EncryptionStatus string
	// EncryptionKMSKeyARN is the customer-managed KMS key ARN protecting the
	// stream when AWS reports CUSTOMER_MANAGED_CMK. It is empty for
	// AWS-owned keys.
	EncryptionKMSKeyARN string
	// CreationTimestamp is when the delivery stream was created.
	CreationTimestamp time.Time
	// Destinations holds one entry per reported destination on the stream.
	Destinations []Destination
	// Tags holds AWS resource tags as reported, key to value.
	Tags map[string]string
}

// Destination is the scanner-owned view of one Firehose delivery stream
// destination. Each destination carries only the join-relevant target
// identities (S3 bucket ARN, Redshift cluster identifier, OpenSearch domain
// ARN), the delivery IAM role ARN, the CloudWatch log group name, and the
// transform Lambda ARNs. Endpoint access keys, HEC tokens, and Redshift
// passwords are never mapped.
type Destination struct {
	// Kind classifies the destination (s3, redshift, opensearch, splunk,
	// http_endpoint, or other) so the scanner records the destination class
	// without persisting any payload.
	Kind string
	// RoleARN is the delivery IAM role ARN for this destination when AWS
	// reports one.
	RoleARN string
	// S3BucketARN is the S3 destination (or backup) bucket ARN when AWS reports
	// one. AWS returns a full ARN, so the scanner uses it directly.
	S3BucketARN string
	// RedshiftClusterIdentifier is the Redshift cluster identifier parsed from
	// the destination's JDBC URL host when the destination is Redshift.
	RedshiftClusterIdentifier string
	// OpenSearchDomainARN is the OpenSearch Service domain ARN when AWS reports
	// one for an OpenSearch/Elasticsearch destination.
	OpenSearchDomainARN string
	// LogGroupName is the CloudWatch Logs log group name used for delivery
	// error logging when AWS reports CloudWatch logging enabled.
	LogGroupName string
	// TransformLambdaARNs holds the data-transformation Lambda function ARNs
	// reported in the destination processing configuration. Only ARNs survive;
	// the processing-configuration body is never mapped.
	TransformLambdaARNs []string
}
