// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudtrail "github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cttypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

// tagsFor paginates `ListTags` for one ARN. CloudTrail's `ListTags` returns
// the tag list per ARN; the adapter normalizes it into a metadata-only map.
func (c *Client) tagsFor(ctx context.Context, arn string) (map[string]string, error) {
	if strings.TrimSpace(arn) == "" {
		return nil, nil
	}
	tags := map[string]string{}
	var nextToken *string
	for {
		var output *awscloudtrail.ListTagsOutput
		err := c.recordAPICall(ctx, "ListTags", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTags(callCtx, &awscloudtrail.ListTagsInput{
				ResourceIdList: []string{arn},
				NextToken:      nextToken,
			})
			return err
		})
		if err != nil {
			if isResourceMissing(err) {
				return nil, nil
			}
			return nil, err
		}
		if output == nil {
			break
		}
		for _, resource := range output.ResourceTagList {
			for _, tag := range resource.TagsList {
				key := strings.TrimSpace(aws.ToString(tag.Key))
				if key == "" {
					continue
				}
				tags[key] = aws.ToString(tag.Value)
			}
		}
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			break
		}
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
}

func isResourceMissing(err error) bool {
	if err == nil {
		return false
	}
	var trailNotFound *cttypes.TrailNotFoundException
	if asErr(err, &trailNotFound) {
		return true
	}
	var resourceNotFound *cttypes.ResourceTypeNotSupportedException
	return asErr(err, &resourceNotFound)
}

// asErr wraps errors.As so callers can use it without importing errors in
// every file.
func asErr(err error, target any) bool {
	return errors.As(err, target)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
