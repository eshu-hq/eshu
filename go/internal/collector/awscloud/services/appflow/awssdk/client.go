// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappflow "github.com/aws/aws-sdk-go-v2/service/appflow"
	awsappflowtypes "github.com/aws/aws-sdk-go-v2/service/appflow/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	appflowservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appflow"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the subset of the AWS SDK AppFlow client the adapter uses. It is
// deliberately limited to the three read-only metadata operations the scanner
// needs. It excludes StartFlow, StopFlow, DescribeFlowExecutionRecords (flow run
// records), and every Create/Update/Delete operation by construction, so the
// metadata-only and no-data-pull contract is enforced at the type level.
type apiClient interface {
	ListFlows(context.Context, *awsappflow.ListFlowsInput, ...func(*awsappflow.Options)) (*awsappflow.ListFlowsOutput, error)
	DescribeFlow(context.Context, *awsappflow.DescribeFlowInput, ...func(*awsappflow.Options)) (*awsappflow.DescribeFlowOutput, error)
	DescribeConnectorProfiles(context.Context, *awsappflow.DescribeConnectorProfilesInput, ...func(*awsappflow.Options)) (*awsappflow.DescribeConnectorProfilesOutput, error)
}

// Client adapts the AWS SDK for Go v2 AppFlow client into the metadata-only
// AppFlow scanner interface. It never reads flow run records, field mappings
// (DescribeFlow Tasks are ignored), connector credentials, or OAuth tokens. The
// only credential reference forwarded is the connector profile's Secrets Manager
// credentials ARN.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an AppFlow SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsappflow.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListFlows reads the account's AppFlow flows with ListFlows for the connector
// types, status, and trigger type, then DescribeFlow per flow for the source and
// destination S3 bucket references, connector profile names, and the customer
// KMS key ARN. The DescribeFlow Tasks list (field mappings) and flow run records
// are never read.
func (c *Client) ListFlows(ctx context.Context) ([]appflowservice.Flow, error) {
	var flows []appflowservice.Flow
	var nextToken *string
	for {
		var page *awsappflow.ListFlowsOutput
		err := c.recordAPICall(ctx, "ListFlows", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListFlows(callCtx, &awsappflow.ListFlowsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return flows, nil
		}
		for _, definition := range page.Flows {
			flow, err := c.describeFlow(ctx, definition)
			if err != nil {
				return nil, err
			}
			flows = append(flows, flow)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return flows, nil
		}
	}
}

func (c *Client) describeFlow(
	ctx context.Context,
	definition awsappflowtypes.FlowDefinition,
) (appflowservice.Flow, error) {
	name := strings.TrimSpace(aws.ToString(definition.FlowName))
	flow := appflowservice.Flow{
		ARN:                      strings.TrimSpace(aws.ToString(definition.FlowArn)),
		Name:                     name,
		Description:              strings.TrimSpace(aws.ToString(definition.Description)),
		Status:                   strings.TrimSpace(string(definition.FlowStatus)),
		SourceConnectorType:      strings.TrimSpace(string(definition.SourceConnectorType)),
		DestinationConnectorType: strings.TrimSpace(string(definition.DestinationConnectorType)),
		TriggerType:              strings.TrimSpace(string(definition.TriggerType)),
		CreatedAt:                aws.ToTime(definition.CreatedAt),
		LastUpdatedAt:            aws.ToTime(definition.LastUpdatedAt),
	}
	if name == "" {
		return flow, nil
	}

	var detail *awsappflow.DescribeFlowOutput
	err := c.recordAPICall(ctx, "DescribeFlow", func(callCtx context.Context) error {
		var err error
		detail, err = c.client.DescribeFlow(callCtx, &awsappflow.DescribeFlowInput{
			FlowName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return appflowservice.Flow{}, err
	}
	if detail == nil {
		return flow, nil
	}

	if arn := strings.TrimSpace(aws.ToString(detail.FlowArn)); arn != "" {
		flow.ARN = arn
	}
	if status := strings.TrimSpace(string(detail.FlowStatus)); status != "" {
		flow.Status = status
	}
	flow.KMSKeyARN = strings.TrimSpace(aws.ToString(detail.KmsArn))
	if detail.TriggerConfig != nil {
		if triggerType := strings.TrimSpace(string(detail.TriggerConfig.TriggerType)); triggerType != "" {
			flow.TriggerType = triggerType
		}
	}
	applySourceFlowConfig(&flow, detail.SourceFlowConfig)
	applyDestinationFlowConfig(&flow, detail.DestinationFlowConfigList)
	return flow, nil
}

// applySourceFlowConfig copies the safe source-side references (connector
// profile name and S3 source bucket) into the scanner-owned flow view. The
// source connector properties for non-S3 connectors carry object/entity
// selectors that are not read; only the S3 bucket name is extracted.
func applySourceFlowConfig(flow *appflowservice.Flow, config *awsappflowtypes.SourceFlowConfig) {
	if config == nil {
		return
	}
	if connectorType := strings.TrimSpace(string(config.ConnectorType)); connectorType != "" {
		flow.SourceConnectorType = connectorType
	}
	flow.SourceConnectorProfileName = strings.TrimSpace(aws.ToString(config.ConnectorProfileName))
	if props := config.SourceConnectorProperties; props != nil && props.S3 != nil {
		flow.SourceS3Bucket = strings.TrimSpace(aws.ToString(props.S3.BucketName))
	}
}

// applyDestinationFlowConfig copies the safe destination-side references from
// every destination config (connector kind, connector profile name, and S3
// destination bucket) into the scanner-owned flow view. AppFlow supports fan-out
// flows whose DestinationFlowConfigList carries multiple destinations, so each
// destination is preserved in flow.Destinations to drive one graph edge per
// destination. The scalar summary fields mirror the first destination for the
// resource attributes. Destination connector properties beyond the S3 bucket
// name (object/entity selectors, error handling) are not read.
func applyDestinationFlowConfig(flow *appflowservice.Flow, configs []awsappflowtypes.DestinationFlowConfig) {
	for _, config := range configs {
		destination := appflowservice.FlowDestination{
			ConnectorType:        strings.TrimSpace(string(config.ConnectorType)),
			ConnectorProfileName: strings.TrimSpace(aws.ToString(config.ConnectorProfileName)),
		}
		if props := config.DestinationConnectorProperties; props != nil && props.S3 != nil {
			destination.S3Bucket = strings.TrimSpace(aws.ToString(props.S3.BucketName))
		}
		flow.Destinations = append(flow.Destinations, destination)

		if destination.ConnectorType != "" && flow.DestinationConnectorType == "" {
			flow.DestinationConnectorType = destination.ConnectorType
		}
		if flow.DestinationConnectorProfileName == "" {
			flow.DestinationConnectorProfileName = destination.ConnectorProfileName
		}
		if destination.S3Bucket != "" && flow.DestinationS3Bucket == "" {
			flow.DestinationS3Bucket = destination.S3Bucket
		}
	}
}

// ListConnectorProfiles reads the account's AppFlow connector profiles with
// DescribeConnectorProfiles. The response carries the Secrets Manager
// credentials ARN reference but never the credential values; this adapter
// forwards only the ARN.
func (c *Client) ListConnectorProfiles(ctx context.Context) ([]appflowservice.ConnectorProfile, error) {
	var profiles []appflowservice.ConnectorProfile
	var nextToken *string
	for {
		var page *awsappflow.DescribeConnectorProfilesOutput
		err := c.recordAPICall(ctx, "DescribeConnectorProfiles", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeConnectorProfiles(callCtx, &awsappflow.DescribeConnectorProfilesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return profiles, nil
		}
		for _, profile := range page.ConnectorProfileDetails {
			profiles = append(profiles, mapConnectorProfile(profile))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return profiles, nil
		}
	}
}

func mapConnectorProfile(profile awsappflowtypes.ConnectorProfile) appflowservice.ConnectorProfile {
	return appflowservice.ConnectorProfile{
		ARN:            strings.TrimSpace(aws.ToString(profile.ConnectorProfileArn)),
		Name:           strings.TrimSpace(aws.ToString(profile.ConnectorProfileName)),
		ConnectorType:  strings.TrimSpace(string(profile.ConnectorType)),
		ConnectorLabel: strings.TrimSpace(aws.ToString(profile.ConnectorLabel)),
		ConnectionMode: strings.TrimSpace(string(profile.ConnectionMode)),
		CredentialsARN: strings.TrimSpace(aws.ToString(profile.CredentialsArn)),
		CreatedAt:      aws.ToTime(profile.CreatedAt),
		LastUpdatedAt:  aws.ToTime(profile.LastUpdatedAt),
	}
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

var _ appflowservice.Client = (*Client)(nil)

var _ apiClient = (*awsappflow.Client)(nil)
