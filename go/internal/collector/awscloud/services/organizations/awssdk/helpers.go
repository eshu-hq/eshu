// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsorg "github.com/aws/aws-sdk-go-v2/service/organizations"
	awsorgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	organizationsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/organizations"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (c *Client) listTags(ctx context.Context, resourceID string) (map[string]string, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, nil
	}
	var tags []awsorgtypes.Tag
	var nextToken *string
	for {
		var output *awsorg.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTagsForResource(callCtx, &awsorg.ListTagsForResourceInput{
				NextToken:  nextToken,
				ResourceId: aws.String(resourceID),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return tagsMap(tags), nil
		}
		tags = append(tags, output.Tags...)
		nextToken = output.NextToken
		if aws.ToString(nextToken) == "" {
			return tagsMap(tags), nil
		}
	}
}

func policyTypeSummaries(values []awsorgtypes.PolicyTypeSummary) []organizationsservice.PolicyTypeSummary {
	if len(values) == 0 {
		return nil
	}
	output := make([]organizationsservice.PolicyTypeSummary, 0, len(values))
	for _, value := range values {
		output = append(output, organizationsservice.PolicyTypeSummary{
			Type:   strings.TrimSpace(string(value.Type)),
			Status: strings.TrimSpace(string(value.Status)),
		})
	}
	return output
}

func tagsMap(tags []awsorgtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func (c *Client) skippedSnapshot(reason string) organizationsservice.Snapshot {
	return organizationsservice.Snapshot{
		Warnings: []awscloud.WarningObservation{{
			WarningKind:    awscloud.WarningOrganizationsOrgAccessSkipped,
			ErrorClass:     "org_access_skipped",
			Message:        "AWS Organizations metadata scan skipped because credentials are not management or delegated-admin credentials",
			SourceRecordID: "organizations:org-aware-skip",
			Attributes: map[string]any{
				"skip_reason": reason,
			},
		}},
	}
}

func isOrgAccessSkipError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.ErrorCode() {
	case "AccessDeniedException", "AWSOrganizationsNotInUseException":
		return true
	default:
		return false
	}
}

func skipReason(err error) string {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return "unknown"
	}
	switch apiErr.ErrorCode() {
	case "AWSOrganizationsNotInUseException":
		return "not_in_organization"
	case "AccessDeniedException":
		return "org_access_denied"
	default:
		return "unknown"
	}
}

func isPolicyTypeUnavailableError(err error) bool {
	var notEnabled *awsorgtypes.PolicyTypeNotEnabledException
	if errors.As(err, &notEnabled) {
		return true
	}
	var notAvailable *awsorgtypes.PolicyTypeNotAvailableForOrganizationException
	return errors.As(err, &notAvailable)
}

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
