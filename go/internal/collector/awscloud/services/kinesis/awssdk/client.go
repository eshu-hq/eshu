// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfirehose "github.com/aws/aws-sdk-go-v2/service/firehose"
	awskinesis "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awskinesisvideo "github.com/aws/aws-sdk-go-v2/service/kinesisvideo"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	kinesisservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kinesis"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// dataStreamsAPI is the metadata-only Kinesis Data Streams surface the adapter
// is allowed to call. It excludes every record-plane API (PutRecord,
// PutRecords, GetRecords, GetShardIterator), every shard-mutation API
// (MergeShards, SplitShard), and every lifecycle mutation API
// (CreateStream, DeleteStream, UpdateStream).
type dataStreamsAPI interface {
	ListStreams(context.Context, *awskinesis.ListStreamsInput, ...func(*awskinesis.Options)) (*awskinesis.ListStreamsOutput, error)
	DescribeStreamSummary(context.Context, *awskinesis.DescribeStreamSummaryInput, ...func(*awskinesis.Options)) (*awskinesis.DescribeStreamSummaryOutput, error)
	ListTagsForStream(context.Context, *awskinesis.ListTagsForStreamInput, ...func(*awskinesis.Options)) (*awskinesis.ListTagsForStreamOutput, error)
}

// firehoseAPI is the metadata-only Kinesis Data Firehose surface the adapter is
// allowed to call. It excludes every delivery-stream mutation API
// (CreateDeliveryStream, UpdateDestination, DeleteDeliveryStream,
// PutDeliveryStreamEncryptionConfiguration) and every record-plane API
// (PutRecord, PutRecordBatch).
type firehoseAPI interface {
	ListDeliveryStreams(context.Context, *awsfirehose.ListDeliveryStreamsInput, ...func(*awsfirehose.Options)) (*awsfirehose.ListDeliveryStreamsOutput, error)
	DescribeDeliveryStream(context.Context, *awsfirehose.DescribeDeliveryStreamInput, ...func(*awsfirehose.Options)) (*awsfirehose.DescribeDeliveryStreamOutput, error)
	ListTagsForDeliveryStream(context.Context, *awsfirehose.ListTagsForDeliveryStreamInput, ...func(*awsfirehose.Options)) (*awsfirehose.ListTagsForDeliveryStreamOutput, error)
}

// videoAPI is the metadata-only Kinesis Video Streams surface the adapter is
// allowed to call. It excludes every media-plane API (GetMedia, PutMedia,
// GetMediaForFragmentList) and every lifecycle mutation API (CreateStream,
// UpdateStream, DeleteStream).
type videoAPI interface {
	ListStreams(context.Context, *awskinesisvideo.ListStreamsInput, ...func(*awskinesisvideo.Options)) (*awskinesisvideo.ListStreamsOutput, error)
	ListTagsForStream(context.Context, *awskinesisvideo.ListTagsForStreamInput, ...func(*awskinesisvideo.Options)) (*awskinesisvideo.ListTagsForStreamOutput, error)
}

// Client adapts AWS SDK Kinesis Data Streams, Firehose, and Video Streams
// responses to the metadata-only Kinesis scanner contract.
type Client struct {
	dataStreams dataStreamsAPI
	firehose    firehoseAPI
	video       videoAPI
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Kinesis SDK adapter for one claimed AWS boundary. It
// constructs one client per sub-service (Data Streams, Firehose, Video
// Streams) from the same AWS config.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		dataStreams: awskinesis.NewFromConfig(config),
		firehose:    awsfirehose.NewFromConfig(config),
		video:       awskinesisvideo.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDataStreams returns Kinesis Data Streams metadata visible to the
// configured AWS credentials. It paginates ListStreams for discovery and
// enriches each stream with DescribeStreamSummary (shard count, retention,
// encryption) and ListTagsForStream. It never reads records.
func (c *Client) ListDataStreams(ctx context.Context) ([]kinesisservice.DataStream, error) {
	names, err := c.listDataStreamNames(ctx)
	if err != nil {
		return nil, err
	}
	var streams []kinesisservice.DataStream
	for _, name := range names {
		summary, err := c.describeDataStreamSummary(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("describe Kinesis data stream %q: %w", name, err)
		}
		tags, err := c.listDataStreamTags(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("list tags for Kinesis data stream %q: %w", name, err)
		}
		streams = append(streams, mapDataStream(summary, tags))
	}
	return streams, nil
}

func (c *Client) listDataStreamNames(ctx context.Context) ([]string, error) {
	var names []string
	var nextToken *string
	var exclusiveStartName *string
	for {
		var page *awskinesis.ListStreamsOutput
		err := c.recordAPICall(ctx, "ListStreams", func(callCtx context.Context) error {
			var err error
			page, err = c.dataStreams.ListStreams(callCtx, &awskinesis.ListStreamsInput{
				NextToken:                nextToken,
				ExclusiveStartStreamName: exclusiveStartName,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return dedupeNames(names), nil
		}
		lastName := ""
		for _, summary := range page.StreamSummaries {
			if name := strings.TrimSpace(aws.ToString(summary.StreamName)); name != "" {
				names = append(names, name)
				lastName = name
			}
		}
		for _, name := range page.StreamNames {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				names = append(names, trimmed)
				lastName = trimmed
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" && !aws.ToBool(page.HasMoreStreams) {
			return dedupeNames(names), nil
		}
		// AWS reports more streams via one of two mechanisms. Prefer the opaque
		// NextToken when present; otherwise fall back to the documented
		// ExclusiveStartStreamName continuation keyed by the last stream name.
		// A stream name must never be sent in NextToken.
		if aws.ToString(nextToken) != "" {
			exclusiveStartName = nil
			continue
		}
		if lastName == "" {
			// HasMoreStreams=true but the page produced no usable name; stop to
			// avoid resending the same request forever.
			return dedupeNames(names), nil
		}
		exclusiveStartName = aws.String(lastName)
	}
}

func (c *Client) describeDataStreamSummary(ctx context.Context, name string) (*awskinesis.DescribeStreamSummaryOutput, error) {
	var output *awskinesis.DescribeStreamSummaryOutput
	err := c.recordAPICall(ctx, "DescribeStreamSummary", func(callCtx context.Context) error {
		var err error
		output, err = c.dataStreams.DescribeStreamSummary(callCtx, &awskinesis.DescribeStreamSummaryInput{
			StreamName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awskinesis.DescribeStreamSummaryOutput{}, nil
	}
	return output, nil
}

func (c *Client) listDataStreamTags(ctx context.Context, name string) (map[string]string, error) {
	tags := map[string]string{}
	var startTagKey *string
	for {
		var output *awskinesis.ListTagsForStreamOutput
		err := c.recordAPICall(ctx, "ListTagsForStream", func(callCtx context.Context) error {
			var err error
			output, err = c.dataStreams.ListTagsForStream(callCtx, &awskinesis.ListTagsForStreamInput{
				StreamName:           aws.String(name),
				ExclusiveStartTagKey: startTagKey,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return tags, nil
		}
		for _, tag := range output.Tags {
			key := strings.TrimSpace(aws.ToString(tag.Key))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
			startTagKey = tag.Key
		}
		if !aws.ToBool(output.HasMoreTags) {
			return tags, nil
		}
	}
}

// ListFirehoseDeliveryStreams returns Kinesis Data Firehose delivery stream
// metadata visible to the configured AWS credentials. It paginates
// ListDeliveryStreams for discovery and enriches each stream with
// DescribeDeliveryStream (source, destination type, encryption, IAM role,
// transform Lambda ARN) and ListTagsForDeliveryStream. It never persists the
// processing Lambda body or destination secret material.
func (c *Client) ListFirehoseDeliveryStreams(ctx context.Context) ([]kinesisservice.FirehoseDeliveryStream, error) {
	names, err := c.listDeliveryStreamNames(ctx)
	if err != nil {
		return nil, err
	}
	var streams []kinesisservice.FirehoseDeliveryStream
	for _, name := range names {
		description, err := c.describeDeliveryStream(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("describe Firehose delivery stream %q: %w", name, err)
		}
		tags, err := c.listDeliveryStreamTags(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("list tags for Firehose delivery stream %q: %w", name, err)
		}
		streams = append(streams, mapDeliveryStream(description, tags))
	}
	return streams, nil
}

func (c *Client) listDeliveryStreamNames(ctx context.Context) ([]string, error) {
	var names []string
	var startName *string
	for {
		var page *awsfirehose.ListDeliveryStreamsOutput
		err := c.recordAPICall(ctx, "ListDeliveryStreams", func(callCtx context.Context) error {
			var err error
			page, err = c.firehose.ListDeliveryStreams(callCtx, &awsfirehose.ListDeliveryStreamsInput{
				ExclusiveStartDeliveryStreamName: startName,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return names, nil
		}
		for _, name := range page.DeliveryStreamNames {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				names = append(names, trimmed)
			}
		}
		if !aws.ToBool(page.HasMoreDeliveryStreams) || len(page.DeliveryStreamNames) == 0 {
			return names, nil
		}
		startName = aws.String(page.DeliveryStreamNames[len(page.DeliveryStreamNames)-1])
	}
}

func (c *Client) describeDeliveryStream(ctx context.Context, name string) (*awsfirehose.DescribeDeliveryStreamOutput, error) {
	var output *awsfirehose.DescribeDeliveryStreamOutput
	err := c.recordAPICall(ctx, "DescribeDeliveryStream", func(callCtx context.Context) error {
		var err error
		output, err = c.firehose.DescribeDeliveryStream(callCtx, &awsfirehose.DescribeDeliveryStreamInput{
			DeliveryStreamName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return &awsfirehose.DescribeDeliveryStreamOutput{}, nil
	}
	return output, nil
}

func (c *Client) listDeliveryStreamTags(ctx context.Context, name string) (map[string]string, error) {
	tags := map[string]string{}
	var startTagKey *string
	for {
		var output *awsfirehose.ListTagsForDeliveryStreamOutput
		err := c.recordAPICall(ctx, "ListTagsForDeliveryStream", func(callCtx context.Context) error {
			var err error
			output, err = c.firehose.ListTagsForDeliveryStream(callCtx, &awsfirehose.ListTagsForDeliveryStreamInput{
				DeliveryStreamName:   aws.String(name),
				ExclusiveStartTagKey: startTagKey,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return tags, nil
		}
		for _, tag := range output.Tags {
			key := strings.TrimSpace(aws.ToString(tag.Key))
			if key == "" {
				continue
			}
			tags[key] = aws.ToString(tag.Value)
			startTagKey = tag.Key
		}
		if !aws.ToBool(output.HasMoreTags) {
			return tags, nil
		}
	}
}

// ListVideoStreams returns Kinesis Video Streams metadata visible to the
// configured AWS credentials. ListStreams returns full StreamInfo (status, KMS
// key, retention), so no per-stream describe is required. It never reads media
// fragments.
func (c *Client) ListVideoStreams(ctx context.Context) ([]kinesisservice.VideoStream, error) {
	var streams []kinesisservice.VideoStream
	var nextToken *string
	for {
		var page *awskinesisvideo.ListStreamsOutput
		err := c.recordAPICall(ctx, "ListStreams", func(callCtx context.Context) error {
			var err error
			page, err = c.video.ListStreams(callCtx, &awskinesisvideo.ListStreamsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return streams, nil
		}
		for _, info := range page.StreamInfoList {
			name := strings.TrimSpace(aws.ToString(info.StreamName))
			tags, err := c.listVideoStreamTags(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("list tags for Kinesis video stream %q: %w", name, err)
			}
			streams = append(streams, mapVideoStream(info, tags))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return streams, nil
		}
	}
}

func (c *Client) listVideoStreamTags(ctx context.Context, name string) (map[string]string, error) {
	if name == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var nextToken *string
	for {
		var output *awskinesisvideo.ListTagsForStreamOutput
		err := c.recordAPICall(ctx, "ListTagsForStream", func(callCtx context.Context) error {
			var err error
			output, err = c.video.ListTagsForStream(callCtx, &awskinesisvideo.ListTagsForStreamInput{
				StreamName: aws.String(name),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return tags, nil
		}
		for key, value := range output.Tags {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			tags[key] = value
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return tags, nil
		}
	}
}

var _ kinesisservice.Client = (*Client)(nil)

var (
	_ dataStreamsAPI = (*awskinesis.Client)(nil)
	_ firehoseAPI    = (*awsfirehose.Client)(nil)
	_ videoAPI       = (*awskinesisvideo.Client)(nil)
)
