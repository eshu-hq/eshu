// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfirehosetypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"

	firehoseservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/firehose"
)

// destinationKind constants mirror the scanner-owned destination kinds. They
// stay local to the adapter so the scanner package does not export them.
const (
	destinationKindS3           = "s3"
	destinationKindRedshift     = "redshift"
	destinationKindOpenSearch   = "opensearch"
	destinationKindSplunk       = "splunk"
	destinationKindHTTPEndpoint = "http_endpoint"
)

// mapDeliveryStream maps an AWS SDK DeliveryStreamDescription into the
// scanner-owned DeliveryStream view, keeping only safe identity, status,
// source, encryption, and destination metadata.
func mapDeliveryStream(description awsfirehosetypes.DeliveryStreamDescription) firehoseservice.DeliveryStream {
	stream := firehoseservice.DeliveryStream{
		Name:              strings.TrimSpace(aws.ToString(description.DeliveryStreamName)),
		ARN:               strings.TrimSpace(aws.ToString(description.DeliveryStreamARN)),
		Status:            strings.TrimSpace(string(description.DeliveryStreamStatus)),
		StreamType:        strings.TrimSpace(string(description.DeliveryStreamType)),
		CreationTimestamp: aws.ToTime(description.CreateTimestamp),
		Destinations:      mapDestinations(description.Destinations),
	}
	stream.SourceType, stream.SourceKinesisStreamARN = mapSource(description.Source)
	stream.EncryptionMode, stream.EncryptionStatus, stream.EncryptionKMSKeyARN = mapEncryption(
		description.DeliveryStreamEncryptionConfiguration,
	)
	return stream
}

// mapSource classifies the delivery stream source and extracts the source
// Kinesis data stream ARN when the stream reads from a Kinesis data stream.
// Source role ARNs and MSK/database connection details are not mapped.
func mapSource(source *awsfirehosetypes.SourceDescription) (sourceType string, kinesisStreamARN string) {
	if source == nil {
		return "direct_put", ""
	}
	switch {
	case source.KinesisStreamSourceDescription != nil:
		arn := strings.TrimSpace(aws.ToString(source.KinesisStreamSourceDescription.KinesisStreamARN))
		return "kinesis_stream", arn
	case source.MSKSourceDescription != nil:
		return "msk", ""
	case source.DatabaseSourceDescription != nil:
		return "database", ""
	case source.DirectPutSourceDescription != nil:
		return "direct_put", ""
	default:
		return "", ""
	}
}

// mapEncryption extracts the server-side encryption mode, status, and
// customer-managed KMS key ARN. The KMS key ARN is recorded only for
// CUSTOMER_MANAGED_CMK; AWS-owned keys report no ARN.
func mapEncryption(
	config *awsfirehosetypes.DeliveryStreamEncryptionConfiguration,
) (mode string, status string, kmsKeyARN string) {
	if config == nil {
		return "", "", ""
	}
	mode = strings.TrimSpace(string(config.KeyType))
	status = strings.TrimSpace(string(config.Status))
	if config.KeyType == awsfirehosetypes.KeyTypeCustomerManagedCmk {
		kmsKeyARN = strings.TrimSpace(aws.ToString(config.KeyARN))
	}
	return mode, status, kmsKeyARN
}

// mapDestinations maps each reported destination into the scanner-owned
// Destination view, recording only the join-relevant target identities, the
// delivery role ARN, the CloudWatch log group name, and the transform Lambda
// ARNs.
func mapDestinations(descriptions []awsfirehosetypes.DestinationDescription) []firehoseservice.Destination {
	if len(descriptions) == 0 {
		return nil
	}
	destinations := make([]firehoseservice.Destination, 0, len(descriptions))
	for _, description := range descriptions {
		if destination, ok := mapDestination(description); ok {
			destinations = append(destinations, destination)
		}
	}
	if len(destinations) == 0 {
		return nil
	}
	return destinations
}

// mapDestination maps one DestinationDescription into a scanner-owned
// Destination, choosing the most specific reported destination kind. It returns
// ok=false when AWS reports no recognizable destination so the caller skips it.
func mapDestination(description awsfirehosetypes.DestinationDescription) (firehoseservice.Destination, bool) {
	switch {
	case description.ExtendedS3DestinationDescription != nil:
		return mapExtendedS3Destination(description.ExtendedS3DestinationDescription), true
	case description.S3DestinationDescription != nil:
		return mapS3Destination(description.S3DestinationDescription), true
	case description.RedshiftDestinationDescription != nil:
		return mapRedshiftDestination(description.RedshiftDestinationDescription), true
	case description.AmazonopensearchserviceDestinationDescription != nil:
		return mapOpenSearchServiceDestination(description.AmazonopensearchserviceDestinationDescription), true
	case description.ElasticsearchDestinationDescription != nil:
		return mapElasticsearchDestination(description.ElasticsearchDestinationDescription), true
	case description.SplunkDestinationDescription != nil:
		return mapSplunkDestination(description.SplunkDestinationDescription), true
	case description.HttpEndpointDestinationDescription != nil:
		return mapHTTPEndpointDestination(description.HttpEndpointDestinationDescription), true
	default:
		return firehoseservice.Destination{}, false
	}
}

func mapExtendedS3Destination(d *awsfirehosetypes.ExtendedS3DestinationDescription) firehoseservice.Destination {
	return firehoseservice.Destination{
		Kind:                destinationKindS3,
		RoleARN:             strings.TrimSpace(aws.ToString(d.RoleARN)),
		S3BucketARN:         strings.TrimSpace(aws.ToString(d.BucketARN)),
		LogGroupName:        logGroupName(d.CloudWatchLoggingOptions),
		TransformLambdaARNs: transformLambdaARNs(d.ProcessingConfiguration),
	}
}

func mapS3Destination(d *awsfirehosetypes.S3DestinationDescription) firehoseservice.Destination {
	return firehoseservice.Destination{
		Kind:         destinationKindS3,
		RoleARN:      strings.TrimSpace(aws.ToString(d.RoleARN)),
		S3BucketARN:  strings.TrimSpace(aws.ToString(d.BucketARN)),
		LogGroupName: logGroupName(d.CloudWatchLoggingOptions),
	}
}

func mapRedshiftDestination(d *awsfirehosetypes.RedshiftDestinationDescription) firehoseservice.Destination {
	destination := firehoseservice.Destination{
		Kind:                destinationKindRedshift,
		RoleARN:             strings.TrimSpace(aws.ToString(d.RoleARN)),
		LogGroupName:        logGroupName(d.CloudWatchLoggingOptions),
		TransformLambdaARNs: transformLambdaARNs(d.ProcessingConfiguration),
	}
	destination.RedshiftClusterIdentifier = redshiftClusterIdentifier(aws.ToString(d.ClusterJDBCURL))
	// Redshift streams stage to S3 before COPY; record the staging bucket ARN
	// so the stream-to-S3 edge resolves for the backup/staging bucket too.
	if d.S3DestinationDescription != nil {
		destination.S3BucketARN = strings.TrimSpace(aws.ToString(d.S3DestinationDescription.BucketARN))
	}
	return destination
}

func mapOpenSearchServiceDestination(
	d *awsfirehosetypes.AmazonopensearchserviceDestinationDescription,
) firehoseservice.Destination {
	return firehoseservice.Destination{
		Kind:                destinationKindOpenSearch,
		RoleARN:             strings.TrimSpace(aws.ToString(d.RoleARN)),
		OpenSearchDomainARN: strings.TrimSpace(aws.ToString(d.DomainARN)),
		LogGroupName:        logGroupName(d.CloudWatchLoggingOptions),
		TransformLambdaARNs: transformLambdaARNs(d.ProcessingConfiguration),
	}
}

func mapElasticsearchDestination(
	d *awsfirehosetypes.ElasticsearchDestinationDescription,
) firehoseservice.Destination {
	return firehoseservice.Destination{
		Kind:                destinationKindOpenSearch,
		RoleARN:             strings.TrimSpace(aws.ToString(d.RoleARN)),
		OpenSearchDomainARN: strings.TrimSpace(aws.ToString(d.DomainARN)),
		LogGroupName:        logGroupName(d.CloudWatchLoggingOptions),
		TransformLambdaARNs: transformLambdaARNs(d.ProcessingConfiguration),
	}
}

func mapSplunkDestination(d *awsfirehosetypes.SplunkDestinationDescription) firehoseservice.Destination {
	// The Splunk HEC token is never mapped; only the destination class, the
	// staging bucket, the log group, and the transform Lambdas survive.
	destination := firehoseservice.Destination{
		Kind:                destinationKindSplunk,
		LogGroupName:        logGroupName(d.CloudWatchLoggingOptions),
		TransformLambdaARNs: transformLambdaARNs(d.ProcessingConfiguration),
	}
	if d.S3DestinationDescription != nil {
		destination.RoleARN = strings.TrimSpace(aws.ToString(d.S3DestinationDescription.RoleARN))
		destination.S3BucketARN = strings.TrimSpace(aws.ToString(d.S3DestinationDescription.BucketARN))
	}
	return destination
}

func mapHTTPEndpointDestination(
	d *awsfirehosetypes.HttpEndpointDestinationDescription,
) firehoseservice.Destination {
	// The endpoint URL, name, and access key are never mapped; only the
	// destination class, the delivery role, the staging bucket, the log group,
	// and the transform Lambdas survive.
	destination := firehoseservice.Destination{
		Kind:                destinationKindHTTPEndpoint,
		RoleARN:             strings.TrimSpace(aws.ToString(d.RoleARN)),
		LogGroupName:        logGroupName(d.CloudWatchLoggingOptions),
		TransformLambdaARNs: transformLambdaARNs(d.ProcessingConfiguration),
	}
	if d.S3DestinationDescription != nil {
		destination.S3BucketARN = strings.TrimSpace(aws.ToString(d.S3DestinationDescription.BucketARN))
	}
	return destination
}

// logGroupName returns the CloudWatch log group name when AWS reports
// CloudWatch logging enabled for a destination, or "" otherwise.
func logGroupName(options *awsfirehosetypes.CloudWatchLoggingOptions) string {
	if options == nil || !aws.ToBool(options.Enabled) {
		return ""
	}
	return strings.TrimSpace(aws.ToString(options.LogGroupName))
}

// transformLambdaARNs extracts the data-transformation Lambda function ARNs
// from a destination processing configuration. Only the LambdaArn parameter of
// each Lambda processor is read; no other processor parameters are mapped, so
// the processing-configuration body never leaves AWS.
func transformLambdaARNs(config *awsfirehosetypes.ProcessingConfiguration) []string {
	if config == nil || !aws.ToBool(config.Enabled) || len(config.Processors) == 0 {
		return nil
	}
	var arns []string
	for _, processor := range config.Processors {
		if processor.Type != awsfirehosetypes.ProcessorTypeLambda {
			continue
		}
		for _, parameter := range processor.Parameters {
			if parameter.ParameterName != awsfirehosetypes.ProcessorParameterNameLambdaArn {
				continue
			}
			if arn := strings.TrimSpace(aws.ToString(parameter.ParameterValue)); arn != "" {
				arns = append(arns, arn)
			}
		}
	}
	if len(arns) == 0 {
		return nil
	}
	return arns
}

// redshiftClusterIdentifier parses the cluster identifier from a Firehose
// Redshift destination JDBC URL host. AWS reports a JDBC URL, not an ARN, so the
// scanner keys the Redshift edge by the cluster identifier the Redshift scanner
// publishes as its resource Name. The Redshift password embedded in the
// destination configuration is never mapped.
func redshiftClusterIdentifier(jdbcURL string) string {
	trimmed := strings.TrimSpace(jdbcURL)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, "://"); idx >= 0 {
		trimmed = trimmed[idx+len("://"):]
	} else if idx := strings.Index(trimmed, "//"); idx >= 0 {
		trimmed = trimmed[idx+len("//"):]
	}
	if idx := strings.IndexAny(trimmed, ":/"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return ""
	}
	if dot := strings.IndexByte(trimmed, '.'); dot >= 0 {
		return strings.TrimSpace(trimmed[:dot])
	}
	return trimmed
}
