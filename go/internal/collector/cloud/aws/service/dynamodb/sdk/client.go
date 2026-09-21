// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"strings"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	dynamodbservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/dynamodb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const listTablesLimit int32 = 100

type apiClient interface {
	ListTables(
		context.Context,
		*awsdynamodb.ListTablesInput,
		...func(*awsdynamodb.Options),
	) (*awsdynamodb.ListTablesOutput, error)
	DescribeTable(
		context.Context,
		*awsdynamodb.DescribeTableInput,
		...func(*awsdynamodb.Options),
	) (*awsdynamodb.DescribeTableOutput, error)
	ListTagsOfResource(
		context.Context,
		*awsdynamodb.ListTagsOfResourceInput,
		...func(*awsdynamodb.Options),
	) (*awsdynamodb.ListTagsOfResourceOutput, error)
	DescribeTimeToLive(
		context.Context,
		*awsdynamodb.DescribeTimeToLiveInput,
		...func(*awsdynamodb.Options),
	) (*awsdynamodb.DescribeTimeToLiveOutput, error)
	DescribeContinuousBackups(
		context.Context,
		*awsdynamodb.DescribeContinuousBackupsInput,
		...func(*awsdynamodb.Options),
	) (*awsdynamodb.DescribeContinuousBackupsOutput, error)
}

// Client adapts AWS SDK DynamoDB control-plane calls into scanner-owned
// metadata. It never reads items, queries or scans tables, reads stream
// records, fetches exports/backups/policies, or calls mutation APIs.
type Client struct {
	client      apiClient
	boundary    aws.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DynamoDB SDK adapter for one claimed AWS boundary.
func NewClient(
	config awsv2.Config,
	boundary aws.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdynamodb.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns DynamoDB table metadata and non-fatal partial-scan warnings
// visible to the configured AWS credentials.
func (c *Client) Snapshot(ctx context.Context) (dynamodbservice.Snapshot, error) {
	var tables []dynamodbservice.Table
	var warnings []aws.WarningObservation
	var startName *string
	var ttlThrottled bool
	for {
		var page *awsdynamodb.ListTablesOutput
		err := c.recordAPICall(ctx, "ListTables", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTables(callCtx, &awsdynamodb.ListTablesInput{
				ExclusiveStartTableName: startName,
				Limit:                   awsv2.Int32(listTablesLimit),
			})
			return err
		})
		if err != nil {
			return dynamodbservice.Snapshot{}, err
		}
		if page == nil {
			return dynamodbservice.Snapshot{Tables: tables, Warnings: warnings}, nil
		}
		for _, tableName := range page.TableNames {
			table, ok, tableWarnings, tableTTLThrottled, err := c.describeTable(ctx, tableName, ttlThrottled)
			if err != nil {
				return dynamodbservice.Snapshot{}, err
			}
			ttlThrottled = ttlThrottled || tableTTLThrottled
			warnings = appendDynamoDBWarningOnce(warnings, tableWarnings...)
			if ok {
				tables = append(tables, table)
			}
		}
		startName = page.LastEvaluatedTableName
		if awsv2.ToString(startName) == "" {
			return dynamodbservice.Snapshot{Tables: tables, Warnings: warnings}, nil
		}
	}
}

func (c *Client) describeTable(
	ctx context.Context,
	tableName string,
	skipTTL bool,
) (dynamodbservice.Table, bool, []aws.WarningObservation, bool, error) {
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		return dynamodbservice.Table{}, false, nil, false, nil
	}
	var output *awsdynamodb.DescribeTableOutput
	err := c.recordAPICall(ctx, "DescribeTable", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeTable(callCtx, &awsdynamodb.DescribeTableInput{
			TableName: awsv2.String(tableName),
		})
		return err
	})
	if err != nil {
		return dynamodbservice.Table{}, false, nil, false, err
	}
	if output == nil || output.Table == nil {
		return dynamodbservice.Table{}, false, nil, false, nil
	}
	tableARN := awsv2.ToString(output.Table.TableArn)
	tags, err := c.listTags(ctx, tableARN)
	if err != nil {
		return dynamodbservice.Table{}, false, nil, false, err
	}
	ttl, warnings, ttlThrottled, err := c.describeTimeToLive(ctx, tableName, skipTTL)
	if err != nil {
		return dynamodbservice.Table{}, false, nil, false, err
	}
	backups, err := c.describeContinuousBackups(ctx, tableName)
	if err != nil {
		return dynamodbservice.Table{}, false, nil, false, err
	}
	return mapTable(*output.Table, tags, ttl, backups), true, warnings, ttlThrottled, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var tags map[string]string
	var nextToken *string
	for {
		var output *awsdynamodb.ListTagsOfResourceOutput
		err := c.recordAPICall(ctx, "ListTagsOfResource", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTagsOfResource(callCtx, &awsdynamodb.ListTagsOfResourceInput{
				ResourceArn: awsv2.String(resourceARN),
				NextToken:   nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			return tags, nil
		}
		for key, value := range mapTags(output.Tags) {
			if tags == nil {
				tags = map[string]string{}
			}
			tags[key] = value
		}
		nextToken = output.NextToken
		if awsv2.ToString(nextToken) == "" {
			return tags, nil
		}
	}
}

func (c *Client) describeTimeToLive(
	ctx context.Context,
	tableName string,
	skipTTL bool,
) (dynamodbservice.TTL, []aws.WarningObservation, bool, error) {
	if skipTTL {
		return dynamodbservice.TTL{}, nil, false, nil
	}
	var output *awsdynamodb.DescribeTimeToLiveOutput
	err := c.recordAPICall(ctx, "DescribeTimeToLive", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeTimeToLive(callCtx, &awsdynamodb.DescribeTimeToLiveInput{
			TableName: awsv2.String(tableName),
		})
		return err
	})
	if isThrottleError(err) {
		return dynamodbservice.TTL{}, []aws.WarningObservation{c.ttlThrottleWarning()}, true, nil
	}
	if err != nil || output == nil {
		return dynamodbservice.TTL{}, nil, false, err
	}
	return mapTTL(output.TimeToLiveDescription), nil, false, nil
}

func appendDynamoDBWarningOnce(
	warnings []aws.WarningObservation,
	candidates ...aws.WarningObservation,
) []aws.WarningObservation {
	for _, candidate := range candidates {
		if candidate.WarningKind != aws.WarningThrottleSustained {
			warnings = append(warnings, candidate)
			continue
		}
		seen := false
		for _, warning := range warnings {
			if warning.WarningKind == aws.WarningThrottleSustained &&
				warning.ErrorClass == candidate.ErrorClass {
				seen = true
				break
			}
		}
		if !seen {
			warnings = append(warnings, candidate)
		}
	}
	return warnings
}

func (c *Client) ttlThrottleWarning() aws.WarningObservation {
	return aws.WarningObservation{
		Boundary:    c.boundary,
		WarningKind: aws.WarningThrottleSustained,
		ErrorClass:  "throttled",
		Message:     "DynamoDB DescribeTimeToLive throttled after SDK retries; TTL metadata omitted for this scan",
		Attributes: map[string]any{
			"operation":         "DescribeTimeToLive",
			"partial_component": "ttl",
		},
		SourceRecordID: "dynamodb_ttl_throttled",
	}
}

func (c *Client) describeContinuousBackups(
	ctx context.Context,
	tableName string,
) (dynamodbservice.ContinuousBackups, error) {
	var output *awsdynamodb.DescribeContinuousBackupsOutput
	err := c.recordAPICall(ctx, "DescribeContinuousBackups", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeContinuousBackups(
			callCtx,
			&awsdynamodb.DescribeContinuousBackupsInput{TableName: awsv2.String(tableName)},
		)
		return err
	})
	if err != nil || output == nil {
		return dynamodbservice.ContinuousBackups{}, err
	}
	return mapContinuousBackups(output.ContinuousBackupsDescription), nil
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

var _ dynamodbservice.Client = (*Client)(nil)

var _ apiClient = (*awsdynamodb.Client)(nil)
