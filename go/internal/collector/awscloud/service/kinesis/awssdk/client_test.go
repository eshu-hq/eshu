// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfirehose "github.com/aws/aws-sdk-go-v2/service/firehose"
	awsfirehosetypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"
	awskinesis "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awskinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	awskinesisvideo "github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	awskinesisvideotypes "github.com/aws/aws-sdk-go-v2/service/kinesisvideo/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListDataStreamsMapsSummaryAndTags(t *testing.T) {
	fake := &fakeDataStreamsAPI{
		listPages: []*awskinesis.ListStreamsOutput{{
			StreamNames: []string{"orders"},
		}},
		summary: &awskinesistypes.StreamDescriptionSummary{
			StreamARN:            aws.String("arn:aws:kinesis:us-east-1:123456789012:stream/orders"),
			StreamName:           aws.String("orders"),
			StreamStatus:         awskinesistypes.StreamStatusActive,
			OpenShardCount:       aws.Int32(8),
			RetentionPeriodHours: aws.Int32(72),
			EncryptionType:       awskinesistypes.EncryptionTypeKms,
			KeyId:                aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
			StreamModeDetails:    &awskinesistypes.StreamModeDetails{StreamMode: awskinesistypes.StreamModeOnDemand},
		},
		tags: []awskinesistypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
	}
	adapter := &Client{dataStreams: fake, boundary: testBoundary()}

	streams, err := adapter.ListDataStreams(context.Background())
	if err != nil {
		t.Fatalf("ListDataStreams() error = %v, want nil", err)
	}
	if got, want := len(streams), 1; got != want {
		t.Fatalf("len(streams) = %d, want %d", got, want)
	}
	stream := streams[0]
	if stream.OpenShardCount != 8 {
		t.Fatalf("OpenShardCount = %d, want 8", stream.OpenShardCount)
	}
	if stream.RetentionHours != 72 {
		t.Fatalf("RetentionHours = %d, want 72", stream.RetentionHours)
	}
	if stream.StreamMode != "ON_DEMAND" {
		t.Fatalf("StreamMode = %q, want ON_DEMAND", stream.StreamMode)
	}
	if stream.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/abc" {
		t.Fatalf("KMSKeyID = %q", stream.KMSKeyID)
	}
	if stream.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", stream.Tags)
	}
}

func TestClientListDataStreamsContinuesWithExclusiveStartStreamName(t *testing.T) {
	// AWS Kinesis ListStreams reports HasMoreStreams=true without a NextToken
	// when the account predates the opaque-token pagination model. The documented
	// continuation is to pass the last stream name in ExclusiveStartStreamName,
	// not in NextToken. Feeding the name into NextToken makes the API reject the
	// request, so the second page must carry ExclusiveStartStreamName.
	fake := &fakeDataStreamsAPI{
		listPages: []*awskinesis.ListStreamsOutput{
			{
				StreamNames:    []string{"alpha", "bravo"},
				HasMoreStreams: aws.Bool(true),
			},
			{
				StreamNames:    []string{"charlie"},
				HasMoreStreams: aws.Bool(false),
			},
		},
		summary: &awskinesistypes.StreamDescriptionSummary{
			StreamName:   aws.String("placeholder"),
			StreamStatus: awskinesistypes.StreamStatusActive,
		},
	}
	adapter := &Client{dataStreams: fake, boundary: testBoundary()}

	streams, err := adapter.ListDataStreams(context.Background())
	if err != nil {
		t.Fatalf("ListDataStreams() error = %v, want nil", err)
	}
	if got, want := len(streams), 3; got != want {
		t.Fatalf("len(streams) = %d, want %d", got, want)
	}
	if got, want := len(fake.listInputs), 2; got != want {
		t.Fatalf("ListStreams call count = %d, want %d", got, want)
	}
	second := fake.listInputs[1]
	if got := aws.ToString(second.ExclusiveStartStreamName); got != "bravo" {
		t.Fatalf("second ExclusiveStartStreamName = %q, want bravo", got)
	}
	if got := aws.ToString(second.NextToken); got != "" {
		t.Fatalf("second NextToken = %q, want empty; stream name must not be sent as an opaque token", got)
	}
}

func TestClientListFirehoseMapsDestinationsWithoutSecretsAndExtractsLambda(t *testing.T) {
	fake := &fakeFirehoseAPI{
		listPages: []*awsfirehose.ListDeliveryStreamsOutput{{
			DeliveryStreamNames: []string{"ingest"},
		}},
		description: &awsfirehosetypes.DeliveryStreamDescription{
			DeliveryStreamARN:    aws.String("arn:aws:firehose:us-east-1:123456789012:deliverystream/ingest"),
			DeliveryStreamName:   aws.String("ingest"),
			DeliveryStreamStatus: awsfirehosetypes.DeliveryStreamStatusActive,
			DeliveryStreamType:   awsfirehosetypes.DeliveryStreamTypeKinesisStreamAsSource,
			Source: &awsfirehosetypes.SourceDescription{
				KinesisStreamSourceDescription: &awsfirehosetypes.KinesisStreamSourceDescription{
					KinesisStreamARN: aws.String("arn:aws:kinesis:us-east-1:123456789012:stream/orders"),
				},
			},
			DeliveryStreamEncryptionConfiguration: &awsfirehosetypes.DeliveryStreamEncryptionConfiguration{
				Status:  awsfirehosetypes.DeliveryStreamEncryptionStatusEnabled,
				KeyType: awsfirehosetypes.KeyTypeAwsOwnedCmk,
			},
			Destinations: []awsfirehosetypes.DestinationDescription{
				{
					DestinationId: aws.String("destinationId-000000000001"),
					ExtendedS3DestinationDescription: &awsfirehosetypes.ExtendedS3DestinationDescription{
						BucketARN: aws.String("arn:aws:s3:::ingest-bucket"),
						RoleARN:   aws.String("arn:aws:iam::123456789012:role/firehose-delivery"),
						ProcessingConfiguration: &awsfirehosetypes.ProcessingConfiguration{
							Enabled: aws.Bool(true),
							Processors: []awsfirehosetypes.Processor{{
								Type: awsfirehosetypes.ProcessorTypeLambda,
								Parameters: []awsfirehosetypes.ProcessorParameter{
									{
										ParameterName:  awsfirehosetypes.ProcessorParameterNameLambdaArn,
										ParameterValue: aws.String("arn:aws:lambda:us-east-1:123456789012:function:transform"),
									},
									{
										ParameterName:  awsfirehosetypes.ProcessorParameterNameBufferSizeInMb,
										ParameterValue: aws.String("3"),
									},
								},
							}},
						},
					},
				},
			},
		},
		tags: []awsfirehosetypes.Tag{{Key: aws.String("team"), Value: aws.String("data")}},
	}
	adapter := &Client{firehose: fake, boundary: testBoundary()}

	streams, err := adapter.ListFirehoseDeliveryStreams(context.Background())
	if err != nil {
		t.Fatalf("ListFirehoseDeliveryStreams() error = %v, want nil", err)
	}
	if got, want := len(streams), 1; got != want {
		t.Fatalf("len(streams) = %d, want %d", got, want)
	}
	stream := streams[0]
	if stream.EncryptionStatus != "ENABLED" {
		t.Fatalf("EncryptionStatus = %q, want ENABLED", stream.EncryptionStatus)
	}
	if stream.SourceKinesisStream != "arn:aws:kinesis:us-east-1:123456789012:stream/orders" {
		t.Fatalf("SourceKinesisStream = %q", stream.SourceKinesisStream)
	}
	if got, want := len(stream.Destinations), 1; got != want {
		t.Fatalf("len(Destinations) = %d, want %d", got, want)
	}
	destination := stream.Destinations[0]
	if destination.Kind != "s3" {
		t.Fatalf("destination Kind = %q, want s3", destination.Kind)
	}
	if destination.S3BucketARN != "arn:aws:s3:::ingest-bucket" {
		t.Fatalf("S3BucketARN = %q", destination.S3BucketARN)
	}
	if got, want := len(destination.TransformLambdaARNs), 1; got != want {
		t.Fatalf("TransformLambdaARNs = %#v, want one ARN", destination.TransformLambdaARNs)
	}
	if destination.TransformLambdaARNs[0] != "arn:aws:lambda:us-east-1:123456789012:function:transform" {
		t.Fatalf("TransformLambdaARN = %q", destination.TransformLambdaARNs[0])
	}
}

func TestClientListFirehoseRedshiftDoesNotMapUsernameOrPassword(t *testing.T) {
	fake := &fakeFirehoseAPI{
		listPages: []*awsfirehose.ListDeliveryStreamsOutput{{
			DeliveryStreamNames: []string{"warehouse"},
		}},
		description: &awsfirehosetypes.DeliveryStreamDescription{
			DeliveryStreamARN:  aws.String("arn:aws:firehose:us-east-1:123456789012:deliverystream/warehouse"),
			DeliveryStreamName: aws.String("warehouse"),
			Destinations: []awsfirehosetypes.DestinationDescription{{
				DestinationId: aws.String("destinationId-000000000001"),
				RedshiftDestinationDescription: &awsfirehosetypes.RedshiftDestinationDescription{
					RoleARN:        aws.String("arn:aws:iam::123456789012:role/firehose-redshift"),
					ClusterJDBCURL: aws.String("jdbc:redshift://analytics-cluster.abc123.us-east-1.redshift.amazonaws.com:5439/dev"),
					Username:       aws.String("masteruser"),
				},
			}},
		},
	}
	adapter := &Client{firehose: fake, boundary: testBoundary()}

	streams, err := adapter.ListFirehoseDeliveryStreams(context.Background())
	if err != nil {
		t.Fatalf("ListFirehoseDeliveryStreams() error = %v, want nil", err)
	}
	destination := streams[0].Destinations[0]
	if destination.Kind != "redshift" {
		t.Fatalf("destination Kind = %q, want redshift", destination.Kind)
	}
	if destination.RedshiftClusterID != "analytics-cluster" {
		t.Fatalf("RedshiftClusterID = %q, want analytics-cluster", destination.RedshiftClusterID)
	}
	// The scanner-owned destination type has no field for Username or password,
	// so the JDBC user and SecretsManager material cannot be persisted.
}

func TestClientListFirehoseHTTPEndpointMapsURLNotAccessKey(t *testing.T) {
	fake := &fakeFirehoseAPI{
		listPages: []*awsfirehose.ListDeliveryStreamsOutput{{
			DeliveryStreamNames: []string{"webhook"},
		}},
		description: &awsfirehosetypes.DeliveryStreamDescription{
			DeliveryStreamARN:  aws.String("arn:aws:firehose:us-east-1:123456789012:deliverystream/webhook"),
			DeliveryStreamName: aws.String("webhook"),
			Destinations: []awsfirehosetypes.DestinationDescription{{
				DestinationId: aws.String("destinationId-000000000001"),
				HttpEndpointDestinationDescription: &awsfirehosetypes.HttpEndpointDestinationDescription{
					RoleARN: aws.String("arn:aws:iam::123456789012:role/firehose-http"),
					EndpointConfiguration: &awsfirehosetypes.HttpEndpointDescription{
						Name: aws.String("partner"),
						Url:  aws.String("https://collector.example.com/ingest"),
					},
				},
			}},
		},
	}
	adapter := &Client{firehose: fake, boundary: testBoundary()}

	streams, err := adapter.ListFirehoseDeliveryStreams(context.Background())
	if err != nil {
		t.Fatalf("ListFirehoseDeliveryStreams() error = %v, want nil", err)
	}
	destination := streams[0].Destinations[0]
	if destination.Kind != "http_endpoint" {
		t.Fatalf("destination Kind = %q, want http_endpoint", destination.Kind)
	}
	if destination.HTTPEndpointURL != "https://collector.example.com/ingest" {
		t.Fatalf("HTTPEndpointURL = %q", destination.HTTPEndpointURL)
	}
	if destination.HTTPEndpointName != "partner" {
		t.Fatalf("HTTPEndpointName = %q", destination.HTTPEndpointName)
	}
	// HttpEndpointDescription exposes only Name and Url; the AccessKey lives on
	// the input-only HttpEndpointConfiguration and is never reachable here.
}

func TestClientListVideoStreamsMapsStreamInfoAndTags(t *testing.T) {
	fake := &fakeVideoAPI{
		listPages: []*awskinesisvideo.ListStreamsOutput{{
			StreamInfoList: []awskinesisvideotypes.StreamInfo{{
				StreamARN:            aws.String("arn:aws:kinesisvideo:us-east-1:123456789012:stream/door-cam/1"),
				StreamName:           aws.String("door-cam"),
				Status:               awskinesisvideotypes.StatusActive,
				KmsKeyId:             aws.String("arn:aws:kms:us-east-1:123456789012:key/video"),
				MediaType:            aws.String("video/h264"),
				DataRetentionInHours: aws.Int32(48),
			}},
		}},
		tags: map[string]string{"Site": "hq"},
	}
	adapter := &Client{video: fake, boundary: testBoundary()}

	streams, err := adapter.ListVideoStreams(context.Background())
	if err != nil {
		t.Fatalf("ListVideoStreams() error = %v, want nil", err)
	}
	if got, want := len(streams), 1; got != want {
		t.Fatalf("len(streams) = %d, want %d", got, want)
	}
	stream := streams[0]
	if stream.RetentionHours != 48 {
		t.Fatalf("RetentionHours = %d, want 48", stream.RetentionHours)
	}
	if stream.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/video" {
		t.Fatalf("KMSKeyID = %q", stream.KMSKeyID)
	}
	if stream.Status != "ACTIVE" {
		t.Fatalf("Status = %q, want ACTIVE", stream.Status)
	}
	if stream.Tags["Site"] != "hq" {
		t.Fatalf("Tags = %#v, want Site=hq", stream.Tags)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceKinesis}
}

type fakeDataStreamsAPI struct {
	listPages  []*awskinesis.ListStreamsOutput
	listCalls  int
	listInputs []*awskinesis.ListStreamsInput
	summary    *awskinesistypes.StreamDescriptionSummary
	tags       []awskinesistypes.Tag
}

func (f *fakeDataStreamsAPI) ListStreams(
	_ context.Context,
	input *awskinesis.ListStreamsInput,
	_ ...func(*awskinesis.Options),
) (*awskinesis.ListStreamsOutput, error) {
	f.listInputs = append(f.listInputs, input)
	if f.listCalls >= len(f.listPages) {
		return &awskinesis.ListStreamsOutput{}, nil
	}
	page := f.listPages[f.listCalls]
	f.listCalls++
	return page, nil
}

func (f *fakeDataStreamsAPI) DescribeStreamSummary(
	_ context.Context,
	_ *awskinesis.DescribeStreamSummaryInput,
	_ ...func(*awskinesis.Options),
) (*awskinesis.DescribeStreamSummaryOutput, error) {
	return &awskinesis.DescribeStreamSummaryOutput{StreamDescriptionSummary: f.summary}, nil
}

func (f *fakeDataStreamsAPI) ListTagsForStream(
	_ context.Context,
	_ *awskinesis.ListTagsForStreamInput,
	_ ...func(*awskinesis.Options),
) (*awskinesis.ListTagsForStreamOutput, error) {
	return &awskinesis.ListTagsForStreamOutput{Tags: f.tags}, nil
}

type fakeFirehoseAPI struct {
	listPages   []*awsfirehose.ListDeliveryStreamsOutput
	listCalls   int
	description *awsfirehosetypes.DeliveryStreamDescription
	tags        []awsfirehosetypes.Tag
}

func (f *fakeFirehoseAPI) ListDeliveryStreams(
	_ context.Context,
	_ *awsfirehose.ListDeliveryStreamsInput,
	_ ...func(*awsfirehose.Options),
) (*awsfirehose.ListDeliveryStreamsOutput, error) {
	if f.listCalls >= len(f.listPages) {
		return &awsfirehose.ListDeliveryStreamsOutput{}, nil
	}
	page := f.listPages[f.listCalls]
	f.listCalls++
	return page, nil
}

func (f *fakeFirehoseAPI) DescribeDeliveryStream(
	_ context.Context,
	_ *awsfirehose.DescribeDeliveryStreamInput,
	_ ...func(*awsfirehose.Options),
) (*awsfirehose.DescribeDeliveryStreamOutput, error) {
	return &awsfirehose.DescribeDeliveryStreamOutput{DeliveryStreamDescription: f.description}, nil
}

func (f *fakeFirehoseAPI) ListTagsForDeliveryStream(
	_ context.Context,
	_ *awsfirehose.ListTagsForDeliveryStreamInput,
	_ ...func(*awsfirehose.Options),
) (*awsfirehose.ListTagsForDeliveryStreamOutput, error) {
	return &awsfirehose.ListTagsForDeliveryStreamOutput{Tags: f.tags}, nil
}

type fakeVideoAPI struct {
	listPages []*awskinesisvideo.ListStreamsOutput
	listCalls int
	tags      map[string]string
}

func (f *fakeVideoAPI) ListStreams(
	_ context.Context,
	_ *awskinesisvideo.ListStreamsInput,
	_ ...func(*awskinesisvideo.Options),
) (*awskinesisvideo.ListStreamsOutput, error) {
	if f.listCalls >= len(f.listPages) {
		return &awskinesisvideo.ListStreamsOutput{}, nil
	}
	page := f.listPages[f.listCalls]
	f.listCalls++
	return page, nil
}

func (f *fakeVideoAPI) ListTagsForStream(
	_ context.Context,
	_ *awskinesisvideo.ListTagsForStreamInput,
	_ ...func(*awskinesisvideo.Options),
) (*awskinesisvideo.ListTagsForStreamOutput, error) {
	return &awskinesisvideo.ListTagsForStreamOutput{Tags: f.tags}, nil
}

var (
	_ dataStreamsAPI = (*fakeDataStreamsAPI)(nil)
	_ firehoseAPI    = (*fakeFirehoseAPI)(nil)
	_ videoAPI       = (*fakeVideoAPI)(nil)
)
