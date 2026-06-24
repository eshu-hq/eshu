// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsfirehose "github.com/aws/aws-sdk-go-v2/service/firehose"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	firehoseservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/firehose"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK Firehose surface the adapter consumes. It
// lists only the read operations the scanner needs; mutation and record APIs
// (CreateDeliveryStream, DeleteDeliveryStream, UpdateDestination, PutRecord,
// PutRecordBatch, StartDeliveryStreamEncryption, StopDeliveryStreamEncryption,
// TagDeliveryStream, UntagDeliveryStream) are intentionally absent so they are
// unreachable through this adapter by construction.
type apiClient interface {
	ListDeliveryStreams(context.Context, *awsfirehose.ListDeliveryStreamsInput, ...func(*awsfirehose.Options)) (*awsfirehose.ListDeliveryStreamsOutput, error)
	DescribeDeliveryStream(context.Context, *awsfirehose.DescribeDeliveryStreamInput, ...func(*awsfirehose.Options)) (*awsfirehose.DescribeDeliveryStreamOutput, error)
	ListTagsForDeliveryStream(context.Context, *awsfirehose.ListTagsForDeliveryStreamInput, ...func(*awsfirehose.Options)) (*awsfirehose.ListTagsForDeliveryStreamOutput, error)
}

// Client adapts the AWS SDK Firehose read operations into the scanner-owned
// metadata-only Client interface. It lists delivery stream names, describes
// each stream, and maps the description into safe identity, source, encryption,
// and destination metadata. It never reads delivery records and never persists
// destination access keys, Splunk HEC tokens, Redshift passwords, or
// processing-configuration Lambda bodies.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Firehose SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsfirehose.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDeliveryStreams lists every Firehose delivery stream in the boundary with
// the paginated ListDeliveryStreams API, then describes each stream with
// DescribeDeliveryStream and maps the description into scanner-owned metadata.
// The adapter never calls PutRecord, PutRecordBatch, or any mutation API.
func (c *Client) ListDeliveryStreams(ctx context.Context) ([]firehoseservice.DeliveryStream, error) {
	names, err := c.listDeliveryStreamNames(ctx)
	if err != nil {
		return nil, err
	}
	streams := make([]firehoseservice.DeliveryStream, 0, len(names))
	for _, name := range names {
		stream, err := c.describeDeliveryStream(ctx, name)
		if err != nil {
			return nil, err
		}
		if stream == nil {
			continue
		}
		tags, err := c.listDeliveryStreamTags(ctx, name)
		if err != nil {
			return nil, err
		}
		stream.Tags = tags
		streams = append(streams, *stream)
	}
	return streams, nil
}

// listDeliveryStreamNames pages ListDeliveryStreams until AWS reports no more
// delivery streams, accumulating the trimmed, non-empty stream names.
func (c *Client) listDeliveryStreamNames(ctx context.Context) ([]string, error) {
	var names []string
	var exclusiveStart *string
	for {
		var page *awsfirehose.ListDeliveryStreamsOutput
		err := c.recordAPICall(ctx, "ListDeliveryStreams", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDeliveryStreams(callCtx, &awsfirehose.ListDeliveryStreamsInput{
				ExclusiveStartDeliveryStreamName: exclusiveStart,
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
		exclusiveStart = aws.String(page.DeliveryStreamNames[len(page.DeliveryStreamNames)-1])
	}
}

// listDeliveryStreamTags reads one delivery stream's AWS resource tags with the
// paginated ListTagsForDeliveryStream API. Tags are bounded key/value metadata;
// the call is read-only. It returns nil for an untagged stream.
func (c *Client) listDeliveryStreamTags(ctx context.Context, name string) (map[string]string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var exclusiveStart *string
	for {
		var output *awsfirehose.ListTagsForDeliveryStreamOutput
		err := c.recordAPICall(ctx, "ListTagsForDeliveryStream", func(callCtx context.Context) error {
			var callErr error
			output, callErr = c.client.ListTagsForDeliveryStream(callCtx, &awsfirehose.ListTagsForDeliveryStreamInput{
				DeliveryStreamName:   aws.String(trimmed),
				ExclusiveStartTagKey: exclusiveStart,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		for _, tag := range output.Tags {
			if key := strings.TrimSpace(aws.ToString(tag.Key)); key != "" {
				tags[key] = aws.ToString(tag.Value)
			}
		}
		if !aws.ToBool(output.HasMoreTags) || len(output.Tags) == 0 {
			break
		}
		exclusiveStart = output.Tags[len(output.Tags)-1].Key
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

// describeDeliveryStream reads one delivery stream's description and maps it
// into scanner-owned metadata. It returns a name-only stream when AWS omits the
// description so the scanner still records the stream's presence.
func (c *Client) describeDeliveryStream(
	ctx context.Context,
	name string,
) (*firehoseservice.DeliveryStream, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}
	var output *awsfirehose.DescribeDeliveryStreamOutput
	err := c.recordAPICall(ctx, "DescribeDeliveryStream", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeDeliveryStream(callCtx, &awsfirehose.DescribeDeliveryStreamInput{
			DeliveryStreamName: aws.String(trimmed),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.DeliveryStreamDescription == nil {
		return &firehoseservice.DeliveryStream{Name: trimmed}, nil
	}
	stream := mapDeliveryStream(*output.DeliveryStreamDescription)
	if strings.TrimSpace(stream.Name) == "" {
		stream.Name = trimmed
	}
	return &stream, nil
}

// recordAPICall wraps one Firehose read in a pagination span and the shared AWS
// API-call and throttle counters, attributing each call to the scan boundary.
func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

// isThrottleError reports whether err is an AWS throttle/rate-limit error so
// the adapter records it on the throttle counter without retrying here.
func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException" ||
		code == "LimitExceededException"
}

var _ firehoseservice.Client = (*Client)(nil)

var _ apiClient = (*awsfirehose.Client)(nil)
