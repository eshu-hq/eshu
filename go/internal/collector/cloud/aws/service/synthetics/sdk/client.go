// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awssynthetics "github.com/aws/aws-sdk-go-v2/service/synthetics"
	awssyntheticstypes "github.com/aws/aws-sdk-go-v2/service/synthetics/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	syntheticsservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/synthetics"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS CloudWatch Synthetics API the
// adapter calls. It is deliberately limited to DescribeCanaries, which returns
// canary control-plane metadata. It exposes no GetCanaryRuns, no
// DescribeCanariesLastRun, no GetCanary code read, and no Create/Update/Delete/
// Start/Stop mutation, so the adapter cannot read run artifacts, run results, or
// canary script source, and cannot mutate Synthetics state. The exclusion_test
// reflects over this interface to enforce that contract at build time.
type apiClient interface {
	DescribeCanaries(
		context.Context,
		*awssynthetics.DescribeCanariesInput,
		...func(*awssynthetics.Options),
	) (*awssynthetics.DescribeCanariesOutput, error)
}

// Client adapts AWS SDK CloudWatch Synthetics control-plane calls into
// scanner-owned metadata. It never reads canary script source code, run
// artifacts, or run results, and never calls a mutation or run-control API.
type Client struct {
	client      apiClient
	boundary    aws.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Synthetics SDK adapter for one claimed AWS boundary.
func NewClient(
	config awsv2.Config,
	boundary aws.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssynthetics.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Synthetics canary metadata visible to the configured AWS
// credentials. Canary script source code, run artifacts, and run results are
// never read.
func (c *Client) Snapshot(ctx context.Context) (syntheticsservice.Snapshot, error) {
	canaries, err := c.describeCanaries(ctx)
	if err != nil {
		return syntheticsservice.Snapshot{}, err
	}
	return syntheticsservice.Snapshot{Canaries: canaries}, nil
}

func (c *Client) describeCanaries(ctx context.Context) ([]syntheticsservice.Canary, error) {
	var canaries []syntheticsservice.Canary
	var nextToken *string
	for {
		var page *awssynthetics.DescribeCanariesOutput
		err := c.recordAPICall(ctx, "DescribeCanaries", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeCanaries(callCtx, &awssynthetics.DescribeCanariesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return canaries, nil
		}
		for _, canary := range page.Canaries {
			canaries = append(canaries, c.mapCanary(canary))
		}
		nextToken = page.NextToken
		if awsv2.ToString(nextToken) == "" {
			return canaries, nil
		}
	}
}

func (c *Client) mapCanary(canary awssyntheticstypes.Canary) syntheticsservice.Canary {
	name := strings.TrimSpace(awsv2.ToString(canary.Name))
	mapped := syntheticsservice.Canary{
		ARN:                          c.canaryARN(name),
		ID:                           strings.TrimSpace(awsv2.ToString(canary.Id)),
		Name:                         name,
		RuntimeVersion:               strings.TrimSpace(awsv2.ToString(canary.RuntimeVersion)),
		EngineARN:                    strings.TrimSpace(awsv2.ToString(canary.EngineArn)),
		SuccessRetentionPeriodInDays: awsv2.ToInt32(canary.SuccessRetentionPeriodInDays),
		FailureRetentionPeriodInDays: awsv2.ToInt32(canary.FailureRetentionPeriodInDays),
		ArtifactS3Location:           strings.TrimSpace(awsv2.ToString(canary.ArtifactS3Location)),
		ExecutionRoleARN:             strings.TrimSpace(awsv2.ToString(canary.ExecutionRoleArn)),
		Tags:                         cloneTags(canary.Tags),
	}
	if status := canary.Status; status != nil {
		mapped.State = strings.TrimSpace(string(status.State))
		mapped.StateReasonCode = strings.TrimSpace(string(status.StateReasonCode))
	}
	if schedule := canary.Schedule; schedule != nil {
		mapped.ScheduleExpression = strings.TrimSpace(awsv2.ToString(schedule.Expression))
		mapped.ScheduleDurationInSeconds = awsv2.ToInt64(schedule.DurationInSeconds)
	}
	if run := canary.RunConfig; run != nil {
		mapped.RunTimeoutInSeconds = awsv2.ToInt32(run.TimeoutInSeconds)
		mapped.RunMemoryInMB = awsv2.ToInt32(run.MemoryInMB)
		mapped.RunActiveTracing = awsv2.ToBool(run.ActiveTracing)
	}
	applyArtifactEncryption(&mapped, canary.ArtifactConfig)
	applyVPCConfig(&mapped, canary.VpcConfig)
	if timeline := canary.Timeline; timeline != nil {
		mapped.Created = awsv2.ToTime(timeline.Created)
		mapped.LastModified = awsv2.ToTime(timeline.LastModified)
	}
	return mapped
}

// canaryARN synthesizes the partition-aware canary ARN from the scan boundary,
// because DescribeCanaries does not return an ARN field. The partition is
// derived from the boundary region so GovCloud and China canaries carry the
// correct partition instead of a hardcoded commercial ARN.
func (c *Client) canaryARN(name string) string {
	name = strings.TrimSpace(name)
	account := strings.TrimSpace(c.boundary.AccountID)
	region := strings.TrimSpace(c.boundary.Region)
	if name == "" || account == "" || region == "" {
		return ""
	}
	partition := aws.PartitionForBoundary(c.boundary)
	return "arn:" + partition + ":synthetics:" + region + ":" + account + ":canary:" + name
}

// applyArtifactEncryption copies the run-artifact S3 encryption mode and the
// customer-managed KMS key ARN onto the canary. It never reads the artifacts
// themselves; only the encryption configuration is metadata.
func applyArtifactEncryption(
	canary *syntheticsservice.Canary,
	config *awssyntheticstypes.ArtifactConfigOutput,
) {
	if config == nil || config.S3Encryption == nil {
		return
	}
	s3 := config.S3Encryption
	canary.ArtifactEncryptionMode = strings.TrimSpace(string(s3.EncryptionMode))
	canary.ArtifactKMSKeyARN = strings.TrimSpace(awsv2.ToString(s3.KmsKeyArn))
}

// applyVPCConfig copies the canary VPC id, subnet ids, and security group ids
// onto the canary when the canary runs in a VPC.
func applyVPCConfig(
	canary *syntheticsservice.Canary,
	config *awssyntheticstypes.VpcConfigOutput,
) {
	if config == nil {
		return
	}
	canary.VPCID = strings.TrimSpace(awsv2.ToString(config.VpcId))
	canary.SubnetIDs = trimAll(config.SubnetIds)
	canary.SecurityGroupIDs = trimAll(config.SecurityGroupIds)
}

func trimAll(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneTags(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	tags := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		tags[trimmed] = value
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
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
	aws.RecordAPICall(ctx, aws.APICallEvent{
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

var _ syntheticsservice.Client = (*Client)(nil)

var _ apiClient = (*awssynthetics.Client)(nil)
