// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceFirehose identifies the regional Amazon Data Firehose
	// metadata-only scan slice. The slice covers Kinesis Data Firehose delivery
	// streams and the destination, source, encryption, and transform
	// relationships those streams report. The scanner never reads delivery
	// records or persists destination secret material.
	ServiceFirehose = "firehose"
)

const (
	// ResourceTypeFirehoseDeliveryStream identifies an Amazon Data Firehose
	// delivery stream metadata resource. The scanner records the stream name,
	// ARN, status, stream type, source type, encryption mode, and creation
	// time; it never reads delivery records and never persists destination
	// access keys, HEC tokens, Redshift passwords, or processing-configuration
	// Lambda bodies.
	ResourceTypeFirehoseDeliveryStream = "aws_firehose_delivery_stream"
)

const (
	// RelationshipFirehoseStreamDeliversToS3Bucket records a Firehose delivery
	// stream's reported S3 bucket destination (primary or backup). AWS reports
	// the bucket ARN directly, so the edge keys the S3 bucket node by its ARN.
	RelationshipFirehoseStreamDeliversToS3Bucket = "firehose_stream_delivers_to_s3_bucket"
	// RelationshipFirehoseStreamDeliversToRedshiftCluster records a Firehose
	// delivery stream's reported Amazon Redshift cluster destination, keyed by
	// the cluster identifier parsed from the JDBC URL host so it joins the
	// cluster node the Redshift scanner publishes.
	RelationshipFirehoseStreamDeliversToRedshiftCluster = "firehose_stream_delivers_to_redshift_cluster"
	// RelationshipFirehoseStreamDeliversToOpenSearchDomain records a Firehose
	// delivery stream's reported Amazon OpenSearch Service domain destination.
	// AWS reports the domain ARN directly, so the edge keys the domain node by
	// its ARN.
	RelationshipFirehoseStreamDeliversToOpenSearchDomain = "firehose_stream_delivers_to_opensearch_domain"
	// RelationshipFirehoseStreamSourcedFromKinesisStream records a Firehose
	// delivery stream's reported Kinesis Data Streams source. AWS reports the
	// source stream ARN directly, so the edge keys the data-stream node by its
	// ARN.
	RelationshipFirehoseStreamSourcedFromKinesisStream = "firehose_stream_sourced_from_kinesis_stream"
	// RelationshipFirehoseStreamUsesIAMRole records a Firehose delivery
	// stream's reported delivery IAM role dependency. AWS reports the role ARN
	// directly, so the edge keys the role node by its ARN.
	RelationshipFirehoseStreamUsesIAMRole = "firehose_stream_uses_iam_role"
	// RelationshipFirehoseStreamUsesKMSKey records a Firehose delivery stream's
	// reported server-side encryption (SSE) customer-managed KMS key. The
	// relationship emits only when AWS reports a customer-managed key ARN.
	RelationshipFirehoseStreamUsesKMSKey = "firehose_stream_uses_kms_key"
	// RelationshipFirehoseStreamLogsToCloudWatchLogGroup records a Firehose
	// delivery stream's reported CloudWatch Logs log group used for delivery
	// error logging, keyed by the log group name AWS reports.
	RelationshipFirehoseStreamLogsToCloudWatchLogGroup = "firehose_stream_logs_to_cloudwatch_log_group"
	// RelationshipFirehoseStreamUsesLambdaTransform records a Firehose delivery
	// stream's reported data-transformation Lambda function. Only the Lambda
	// ARN is recorded; the processing-configuration body is never persisted.
	RelationshipFirehoseStreamUsesLambdaTransform = "firehose_stream_uses_lambda_transform"
)
