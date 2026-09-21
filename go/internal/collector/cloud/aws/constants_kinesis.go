// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceKinesis identifies the regional Amazon Kinesis metadata-only scan
	// slice. The slice covers Kinesis Data Streams, Kinesis Data Firehose
	// delivery streams, and Kinesis Video Streams under one service_kind.
	ServiceKinesis = "kinesis"
)

const (
	// ResourceTypeKinesisDataStream identifies a Kinesis Data Streams stream
	// metadata resource. The scanner records shard count, retention, stream
	// mode, and encryption status; it never reads stream records.
	ResourceTypeKinesisDataStream = "aws_kinesis_data_stream"
	// ResourceTypeKinesisFirehoseDeliveryStream identifies a Kinesis Data
	// Firehose delivery stream metadata resource. The scanner records source,
	// destination type, and encryption status; it never persists processing
	// Lambda bodies or destination secret material.
	ResourceTypeKinesisFirehoseDeliveryStream = "aws_kinesis_firehose_delivery_stream"
	// ResourceTypeKinesisVideoStream identifies a Kinesis Video Streams stream
	// metadata resource. The scanner records status, KMS key, and data
	// retention; it never reads media fragments.
	ResourceTypeKinesisVideoStream = "aws_kinesis_video_stream"
)

const (
	// ResourceTypeFirehoseHTTPEndpoint identifies a generic HTTP endpoint
	// Firehose destination keyed by its reported URL. The scanner never
	// persists the endpoint access key.
	ResourceTypeFirehoseHTTPEndpoint = "aws_firehose_http_endpoint"
	// ResourceTypeSplunkEndpoint identifies a Splunk HEC endpoint Firehose
	// destination keyed by its reported endpoint URL. The scanner never
	// persists the HEC token.
	ResourceTypeSplunkEndpoint = "aws_splunk_endpoint"
)

const (
	// RelationshipKinesisDataStreamUsesKMSKey records a Kinesis data stream's
	// reported server-side encryption KMS key dependency. The relationship
	// emits only when AWS reports the customer-managed KMS key ARN.
	RelationshipKinesisDataStreamUsesKMSKey = "kinesis_data_stream_uses_kms_key"
	// RelationshipKinesisVideoStreamUsesKMSKey records a Kinesis video stream's
	// reported KMS key dependency. The relationship emits only when AWS reports
	// the customer-managed KMS key ARN.
	RelationshipKinesisVideoStreamUsesKMSKey = "kinesis_video_stream_uses_kms_key"
	// RelationshipFirehoseDeliveryStreamUsesIAMRole records a Firehose delivery
	// stream's reported service IAM role dependency for a destination.
	RelationshipFirehoseDeliveryStreamUsesIAMRole = "firehose_delivery_stream_uses_iam_role"
	// RelationshipFirehoseDeliveryStreamUsesLambdaTransform records a Firehose
	// delivery stream's reported data-transformation Lambda function. Only the
	// Lambda ARN is recorded; the processing configuration body is never
	// persisted.
	RelationshipFirehoseDeliveryStreamUsesLambdaTransform = "firehose_delivery_stream_uses_lambda_transform"
	// RelationshipFirehoseDeliveryStreamDeliversToS3 records a Firehose delivery
	// stream's reported S3 bucket destination.
	RelationshipFirehoseDeliveryStreamDeliversToS3 = "firehose_delivery_stream_delivers_to_s3"
	// RelationshipFirehoseDeliveryStreamDeliversToRedshift records a Firehose
	// delivery stream's reported Redshift cluster destination keyed by the
	// cluster identifier parsed from the JDBC URL host.
	RelationshipFirehoseDeliveryStreamDeliversToRedshift = "firehose_delivery_stream_delivers_to_redshift"
	// RelationshipFirehoseDeliveryStreamDeliversToOpenSearch records a Firehose
	// delivery stream's reported OpenSearch domain destination.
	RelationshipFirehoseDeliveryStreamDeliversToOpenSearch = "firehose_delivery_stream_delivers_to_opensearch"
	// RelationshipFirehoseDeliveryStreamDeliversToSplunk records a Firehose
	// delivery stream's reported Splunk HEC endpoint destination keyed by the
	// endpoint URL. The HEC token is never persisted.
	RelationshipFirehoseDeliveryStreamDeliversToSplunk = "firehose_delivery_stream_delivers_to_splunk"
	// RelationshipFirehoseDeliveryStreamDeliversToHTTPEndpoint records a
	// Firehose delivery stream's reported generic HTTP endpoint destination
	// keyed by the endpoint URL. The endpoint access key is never persisted.
	RelationshipFirehoseDeliveryStreamDeliversToHTTPEndpoint = "firehose_delivery_stream_delivers_to_http_endpoint"
)
