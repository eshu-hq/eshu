// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfirehose "github.com/aws/aws-sdk-go-v2/service/firehose"
	awsfirehosetypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
	awskinesis "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awskinesisvideotypes "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"

	kinesisservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kinesis"
)

func mapDataStream(output *awskinesis.DescribeStreamSummaryOutput, tags map[string]string) kinesisservice.DataStream {
	if output == nil || output.StreamDescriptionSummary == nil {
		return kinesisservice.DataStream{Tags: tags}
	}
	summary := output.StreamDescriptionSummary
	streamMode := ""
	if summary.StreamModeDetails != nil {
		streamMode = string(summary.StreamModeDetails.StreamMode)
	}
	return kinesisservice.DataStream{
		ARN:               strings.TrimSpace(aws.ToString(summary.StreamARN)),
		Name:              strings.TrimSpace(aws.ToString(summary.StreamName)),
		Status:            string(summary.StreamStatus),
		StreamMode:        streamMode,
		OpenShardCount:    aws.ToInt32(summary.OpenShardCount),
		RetentionHours:    aws.ToInt32(summary.RetentionPeriodHours),
		EncryptionType:    string(summary.EncryptionType),
		KMSKeyID:          strings.TrimSpace(aws.ToString(summary.KeyId)),
		CreationTimestamp: aws.ToTime(summary.StreamCreationTimestamp),
		Tags:              tags,
	}
}

func mapVideoStream(info awskinesisvideotypes.StreamInfo, tags map[string]string) kinesisservice.VideoStream {
	return kinesisservice.VideoStream{
		ARN:               strings.TrimSpace(aws.ToString(info.StreamARN)),
		Name:              strings.TrimSpace(aws.ToString(info.StreamName)),
		Status:            string(info.Status),
		KMSKeyID:          strings.TrimSpace(aws.ToString(info.KmsKeyId)),
		MediaType:         strings.TrimSpace(aws.ToString(info.MediaType)),
		RetentionHours:    aws.ToInt32(info.DataRetentionInHours),
		CreationTimestamp: aws.ToTime(info.CreationTime),
		Tags:              tags,
	}
}

func mapDeliveryStream(output *awsfirehose.DescribeDeliveryStreamOutput, tags map[string]string) kinesisservice.FirehoseDeliveryStream {
	if output == nil || output.DeliveryStreamDescription == nil {
		return kinesisservice.FirehoseDeliveryStream{Tags: tags}
	}
	description := output.DeliveryStreamDescription
	stream := kinesisservice.FirehoseDeliveryStream{
		ARN:               strings.TrimSpace(aws.ToString(description.DeliveryStreamARN)),
		Name:              strings.TrimSpace(aws.ToString(description.DeliveryStreamName)),
		Status:            string(description.DeliveryStreamStatus),
		StreamType:        string(description.DeliveryStreamType),
		CreationTimestamp: aws.ToTime(description.CreateTimestamp),
		Tags:              tags,
	}
	if description.Source != nil && description.Source.KinesisStreamSourceDescription != nil {
		stream.SourceKinesisStream = strings.TrimSpace(aws.ToString(description.Source.KinesisStreamSourceDescription.KinesisStreamARN))
	}
	if encryption := description.DeliveryStreamEncryptionConfiguration; encryption != nil {
		stream.EncryptionStatus = string(encryption.Status)
		stream.EncryptionKeyType = string(encryption.KeyType)
		stream.EncryptionKMSKeyARN = strings.TrimSpace(aws.ToString(encryption.KeyARN))
	}
	for _, destination := range description.Destinations {
		stream.Destinations = append(stream.Destinations, mapDestination(destination))
	}
	return stream
}

// mapDestination extracts metadata-only destination identity for one Firehose
// destination. It maps at most one destination kind per description block and
// never reads the HTTP endpoint access key, Splunk HEC token, Redshift
// password, or processing-configuration body. Only the transform Lambda ARN is
// extracted from the processing configuration.
func mapDestination(description awsfirehosetypes.DestinationDescription) kinesisservice.FirehoseDestination {
	destination := kinesisservice.FirehoseDestination{
		DestinationID: strings.TrimSpace(aws.ToString(description.DestinationId)),
	}
	switch {
	case description.ExtendedS3DestinationDescription != nil:
		s3 := description.ExtendedS3DestinationDescription
		destination.Kind = kinesisservice.FirehoseDestinationKindS3
		destination.RoleARN = strings.TrimSpace(aws.ToString(s3.RoleARN))
		destination.S3BucketARN = strings.TrimSpace(aws.ToString(s3.BucketARN))
		destination.TransformLambdaARNs = processingLambdaARNs(s3.ProcessingConfiguration)
	case description.S3DestinationDescription != nil:
		s3 := description.S3DestinationDescription
		destination.Kind = kinesisservice.FirehoseDestinationKindS3
		destination.RoleARN = strings.TrimSpace(aws.ToString(s3.RoleARN))
		destination.S3BucketARN = strings.TrimSpace(aws.ToString(s3.BucketARN))
	case description.RedshiftDestinationDescription != nil:
		redshift := description.RedshiftDestinationDescription
		destination.Kind = kinesisservice.FirehoseDestinationKindRedshift
		destination.RoleARN = strings.TrimSpace(aws.ToString(redshift.RoleARN))
		destination.RedshiftClusterID = redshiftClusterID(aws.ToString(redshift.ClusterJDBCURL))
		destination.TransformLambdaARNs = processingLambdaARNs(redshift.ProcessingConfiguration)
	case description.AmazonopensearchserviceDestinationDescription != nil:
		opensearch := description.AmazonopensearchserviceDestinationDescription
		destination.Kind = kinesisservice.FirehoseDestinationKindOpenSearch
		destination.RoleARN = strings.TrimSpace(aws.ToString(opensearch.RoleARN))
		destination.OpenSearchDomainARN = strings.TrimSpace(aws.ToString(opensearch.DomainARN))
		destination.TransformLambdaARNs = processingLambdaARNs(opensearch.ProcessingConfiguration)
	case description.ElasticsearchDestinationDescription != nil:
		elastic := description.ElasticsearchDestinationDescription
		destination.Kind = kinesisservice.FirehoseDestinationKindOpenSearch
		destination.RoleARN = strings.TrimSpace(aws.ToString(elastic.RoleARN))
		destination.OpenSearchDomainARN = strings.TrimSpace(aws.ToString(elastic.DomainARN))
		destination.TransformLambdaARNs = processingLambdaARNs(elastic.ProcessingConfiguration)
	case description.SplunkDestinationDescription != nil:
		splunk := description.SplunkDestinationDescription
		destination.Kind = kinesisservice.FirehoseDestinationKindSplunk
		destination.SplunkEndpoint = strings.TrimSpace(aws.ToString(splunk.HECEndpoint))
		destination.TransformLambdaARNs = processingLambdaARNs(splunk.ProcessingConfiguration)
	case description.HttpEndpointDestinationDescription != nil:
		httpEndpoint := description.HttpEndpointDestinationDescription
		destination.Kind = kinesisservice.FirehoseDestinationKindHTTPEndpoint
		destination.RoleARN = strings.TrimSpace(aws.ToString(httpEndpoint.RoleARN))
		destination.TransformLambdaARNs = processingLambdaARNs(httpEndpoint.ProcessingConfiguration)
		if endpoint := httpEndpoint.EndpointConfiguration; endpoint != nil {
			destination.HTTPEndpointURL = strings.TrimSpace(aws.ToString(endpoint.Url))
			destination.HTTPEndpointName = strings.TrimSpace(aws.ToString(endpoint.Name))
		}
	}
	return destination
}

// processingLambdaARNs returns the ARNs of any AWS Lambda data-transformation
// processors. It reads only the LambdaArn processor parameter; the rest of the
// processing configuration body is intentionally ignored.
func processingLambdaARNs(config *awsfirehosetypes.ProcessingConfiguration) []string {
	if config == nil {
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
	return arns
}

// redshiftClusterID extracts the Redshift cluster identifier from a JDBC URL of
// the form jdbc:redshift://<cluster-id>.<account>.<region>.redshift.amazonaws.com:5439/db.
// Firehose does not report a Redshift cluster ARN, so the cluster identifier is
// the strongest correlation anchor available. An empty result means no edge is
// emitted.
func redshiftClusterID(jdbcURL string) string {
	jdbcURL = strings.TrimSpace(jdbcURL)
	if jdbcURL == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(jdbcURL, "jdbc:")
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := parsed.Hostname()
	if host == "" {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

// dedupeNames returns names with duplicates removed while preserving order. The
// Data Streams discovery loop may append the same name twice when both
// StreamSummaries and StreamNames are populated.
func dedupeNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
