// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfirehose "github.com/aws/aws-sdk-go-v2/service/firehose"
	awsfirehosetypes "github.com/aws/aws-sdk-go-v2/service/firehose/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientExcludesMutationAndRecordOperations is the metadata-only
// exclusion guard. It reflects over the adapter's apiClient interface and fails
// if any mutation, encryption-toggle, tag, or record-write operation is
// reachable. The adapter must never expose the Firehose write path, so the
// interface is the proof: a method that is not on it cannot be called.
func TestAPIClientExcludesMutationAndRecordOperations(t *testing.T) {
	forbidden := []string{
		"PutRecord",
		"PutRecordBatch",
		"CreateDeliveryStream",
		"DeleteDeliveryStream",
		"UpdateDestination",
		"StartDeliveryStreamEncryption",
		"StopDeliveryStreamEncryption",
		"TagDeliveryStream",
		"UntagDeliveryStream",
	}
	apiType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < apiType.NumMethod(); i++ {
		name := apiType.Method(i).Name
		for _, banned := range forbidden {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden operation %q; the metadata-only adapter must not reach mutation or record APIs", name)
			}
			if strings.HasPrefix(name, "Put") ||
				strings.HasPrefix(name, "Create") ||
				strings.HasPrefix(name, "Update") ||
				strings.HasPrefix(name, "Delete") ||
				strings.HasPrefix(name, "Tag") ||
				strings.HasPrefix(name, "Untag") ||
				strings.HasPrefix(name, "Start") ||
				strings.HasPrefix(name, "Stop") {
				t.Fatalf("apiClient exposes mutation-shaped operation %q; only List/Describe reads are allowed", name)
			}
		}
	}

	allowed := map[string]struct{}{"ListDeliveryStreams": {}, "DescribeDeliveryStream": {}, "ListTagsForDeliveryStream": {}}
	if got := apiType.NumMethod(); got != len(allowed) {
		t.Fatalf("apiClient has %d methods, want %d (only the read surface)", got, len(allowed))
	}
	for i := 0; i < apiType.NumMethod(); i++ {
		name := apiType.Method(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("apiClient exposes unexpected operation %q; the read surface is List/DescribeDeliveryStream only", name)
		}
	}
}

func TestClientListDeliveryStreamsMapsSafeMetadata(t *testing.T) {
	client := &fakeFirehoseAPI{
		namePages: []*awsfirehose.ListDeliveryStreamsOutput{{
			DeliveryStreamNames:    []string{"orders-firehose"},
			HasMoreDeliveryStreams: aws.Bool(false),
		}},
		describe: map[string]*awsfirehose.DescribeDeliveryStreamOutput{
			"orders-firehose": {
				DeliveryStreamDescription: &awsfirehosetypes.DeliveryStreamDescription{
					DeliveryStreamName:   aws.String("orders-firehose"),
					DeliveryStreamARN:    aws.String("arn:aws:firehose:us-east-1:123456789012:deliverystream/orders-firehose"),
					DeliveryStreamStatus: awsfirehosetypes.DeliveryStreamStatusActive,
					DeliveryStreamType:   awsfirehosetypes.DeliveryStreamTypeKinesisStreamAsSource,
					CreateTimestamp:      aws.Time(time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC)),
					Source: &awsfirehosetypes.SourceDescription{
						KinesisStreamSourceDescription: &awsfirehosetypes.KinesisStreamSourceDescription{
							KinesisStreamARN: aws.String("arn:aws:kinesis:us-east-1:123456789012:stream/orders-raw"),
							RoleARN:          aws.String("arn:aws:iam::123456789012:role/firehose-source"),
						},
					},
					DeliveryStreamEncryptionConfiguration: &awsfirehosetypes.DeliveryStreamEncryptionConfiguration{
						KeyType: awsfirehosetypes.KeyTypeCustomerManagedCmk,
						Status:  awsfirehosetypes.DeliveryStreamEncryptionStatusEnabled,
						KeyARN:  aws.String("arn:aws:kms:us-east-1:123456789012:key/abc"),
					},
					Destinations: []awsfirehosetypes.DestinationDescription{{
						DestinationId: aws.String("destinationId-1"),
						ExtendedS3DestinationDescription: &awsfirehosetypes.ExtendedS3DestinationDescription{
							BucketARN: aws.String("arn:aws:s3:::orders-firehose-dest"),
							RoleARN:   aws.String("arn:aws:iam::123456789012:role/firehose-delivery"),
							CloudWatchLoggingOptions: &awsfirehosetypes.CloudWatchLoggingOptions{
								Enabled:       aws.Bool(true),
								LogGroupName:  aws.String("/aws/kinesisfirehose/orders-firehose"),
								LogStreamName: aws.String("DestinationDelivery"),
							},
							ProcessingConfiguration: &awsfirehosetypes.ProcessingConfiguration{
								Enabled: aws.Bool(true),
								Processors: []awsfirehosetypes.Processor{{
									Type: awsfirehosetypes.ProcessorTypeLambda,
									Parameters: []awsfirehosetypes.ProcessorParameter{{
										ParameterName:  awsfirehosetypes.ProcessorParameterNameLambdaArn,
										ParameterValue: aws.String("arn:aws:lambda:us-east-1:123456789012:function:orders-transform"),
									}},
								}},
							},
						},
					}},
				},
			},
		},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	streams, err := adapter.ListDeliveryStreams(context.Background())
	if err != nil {
		t.Fatalf("ListDeliveryStreams() error = %v, want nil", err)
	}
	if got, want := len(streams), 1; got != want {
		t.Fatalf("len(streams) = %d, want %d", got, want)
	}
	stream := streams[0]
	if stream.Name != "orders-firehose" {
		t.Fatalf("stream.Name = %q, want orders-firehose", stream.Name)
	}
	if stream.SourceType != "kinesis_stream" {
		t.Fatalf("stream.SourceType = %q, want kinesis_stream", stream.SourceType)
	}
	if stream.SourceKinesisStreamARN != "arn:aws:kinesis:us-east-1:123456789012:stream/orders-raw" {
		t.Fatalf("stream.SourceKinesisStreamARN = %q", stream.SourceKinesisStreamARN)
	}
	if stream.EncryptionKMSKeyARN != "arn:aws:kms:us-east-1:123456789012:key/abc" {
		t.Fatalf("stream.EncryptionKMSKeyARN = %q", stream.EncryptionKMSKeyARN)
	}
	if got, want := len(stream.Destinations), 1; got != want {
		t.Fatalf("len(destinations) = %d, want %d", got, want)
	}
	destination := stream.Destinations[0]
	if destination.Kind != destinationKindS3 {
		t.Fatalf("destination.Kind = %q, want %q", destination.Kind, destinationKindS3)
	}
	if destination.S3BucketARN != "arn:aws:s3:::orders-firehose-dest" {
		t.Fatalf("destination.S3BucketARN = %q", destination.S3BucketARN)
	}
	if destination.LogGroupName != "/aws/kinesisfirehose/orders-firehose" {
		t.Fatalf("destination.LogGroupName = %q", destination.LogGroupName)
	}
	if got, want := len(destination.TransformLambdaARNs), 1; got != want {
		t.Fatalf("len(TransformLambdaARNs) = %d, want %d", got, want)
	}
	if destination.TransformLambdaARNs[0] != "arn:aws:lambda:us-east-1:123456789012:function:orders-transform" {
		t.Fatalf("destination.TransformLambdaARNs[0] = %q", destination.TransformLambdaARNs[0])
	}
}

func TestClientMapsAWSOwnedKeyWithoutKMSKeyARN(t *testing.T) {
	client := &fakeFirehoseAPI{
		namePages: []*awsfirehose.ListDeliveryStreamsOutput{{
			DeliveryStreamNames:    []string{"aws-owned"},
			HasMoreDeliveryStreams: aws.Bool(false),
		}},
		describe: map[string]*awsfirehose.DescribeDeliveryStreamOutput{
			"aws-owned": {
				DeliveryStreamDescription: &awsfirehosetypes.DeliveryStreamDescription{
					DeliveryStreamName: aws.String("aws-owned"),
					DeliveryStreamARN:  aws.String("arn:aws:firehose:us-east-1:123456789012:deliverystream/aws-owned"),
					DeliveryStreamType: awsfirehosetypes.DeliveryStreamTypeDirectPut,
					DeliveryStreamEncryptionConfiguration: &awsfirehosetypes.DeliveryStreamEncryptionConfiguration{
						KeyType: awsfirehosetypes.KeyTypeAwsOwnedCmk,
						Status:  awsfirehosetypes.DeliveryStreamEncryptionStatusEnabled,
						KeyARN:  aws.String("arn:aws:kms:us-east-1:123456789012:key/should-not-be-mapped"),
					},
				},
			},
		},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	streams, err := adapter.ListDeliveryStreams(context.Background())
	if err != nil {
		t.Fatalf("ListDeliveryStreams() error = %v, want nil", err)
	}
	if streams[0].EncryptionKMSKeyARN != "" {
		t.Fatalf("EncryptionKMSKeyARN = %q, want empty for an AWS-owned key", streams[0].EncryptionKMSKeyARN)
	}
	if streams[0].SourceType != "direct_put" {
		t.Fatalf("SourceType = %q, want direct_put", streams[0].SourceType)
	}
}

func TestClientParsesRedshiftClusterIdentifierFromJDBCURL(t *testing.T) {
	client := &fakeFirehoseAPI{
		namePages: []*awsfirehose.ListDeliveryStreamsOutput{{
			DeliveryStreamNames:    []string{"to-redshift"},
			HasMoreDeliveryStreams: aws.Bool(false),
		}},
		describe: map[string]*awsfirehose.DescribeDeliveryStreamOutput{
			"to-redshift": {
				DeliveryStreamDescription: &awsfirehosetypes.DeliveryStreamDescription{
					DeliveryStreamName: aws.String("to-redshift"),
					DeliveryStreamARN:  aws.String("arn:aws:firehose:us-east-1:123456789012:deliverystream/to-redshift"),
					DeliveryStreamType: awsfirehosetypes.DeliveryStreamTypeDirectPut,
					Destinations: []awsfirehosetypes.DestinationDescription{{
						RedshiftDestinationDescription: &awsfirehosetypes.RedshiftDestinationDescription{
							RoleARN:        aws.String("arn:aws:iam::123456789012:role/firehose-redshift"),
							ClusterJDBCURL: aws.String("jdbc:redshift://warehouse-prod.abc123.us-east-1.redshift.amazonaws.com:5439/analytics"),
						},
					}},
				},
			},
		},
	}
	adapter := &Client{client: client, boundary: testBoundary()}

	streams, err := adapter.ListDeliveryStreams(context.Background())
	if err != nil {
		t.Fatalf("ListDeliveryStreams() error = %v, want nil", err)
	}
	destination := streams[0].Destinations[0]
	if destination.Kind != destinationKindRedshift {
		t.Fatalf("destination.Kind = %q, want %q", destination.Kind, destinationKindRedshift)
	}
	if destination.RedshiftClusterIdentifier != "warehouse-prod" {
		t.Fatalf("RedshiftClusterIdentifier = %q, want warehouse-prod", destination.RedshiftClusterIdentifier)
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

type fakeFirehoseAPI struct {
	namePages []*awsfirehose.ListDeliveryStreamsOutput
	nameCalls int
	describe  map[string]*awsfirehose.DescribeDeliveryStreamOutput
	tags      map[string]*awsfirehose.ListTagsForDeliveryStreamOutput
}

func (f *fakeFirehoseAPI) ListDeliveryStreams(
	_ context.Context,
	_ *awsfirehose.ListDeliveryStreamsInput,
	_ ...func(*awsfirehose.Options),
) (*awsfirehose.ListDeliveryStreamsOutput, error) {
	if f.nameCalls >= len(f.namePages) {
		return &awsfirehose.ListDeliveryStreamsOutput{HasMoreDeliveryStreams: aws.Bool(false)}, nil
	}
	page := f.namePages[f.nameCalls]
	f.nameCalls++
	return page, nil
}

func (f *fakeFirehoseAPI) DescribeDeliveryStream(
	_ context.Context,
	input *awsfirehose.DescribeDeliveryStreamInput,
	_ ...func(*awsfirehose.Options),
) (*awsfirehose.DescribeDeliveryStreamOutput, error) {
	if f.describe == nil {
		return &awsfirehose.DescribeDeliveryStreamOutput{}, nil
	}
	if output, ok := f.describe[aws.ToString(input.DeliveryStreamName)]; ok {
		return output, nil
	}
	return &awsfirehose.DescribeDeliveryStreamOutput{}, nil
}

func (f *fakeFirehoseAPI) ListTagsForDeliveryStream(
	_ context.Context,
	input *awsfirehose.ListTagsForDeliveryStreamInput,
	_ ...func(*awsfirehose.Options),
) (*awsfirehose.ListTagsForDeliveryStreamOutput, error) {
	if f.tags != nil {
		if output, ok := f.tags[aws.ToString(input.DeliveryStreamName)]; ok {
			return output, nil
		}
	}
	return &awsfirehose.ListTagsForDeliveryStreamOutput{HasMoreTags: aws.Bool(false)}, nil
}

var _ apiClient = (*fakeFirehoseAPI)(nil)
