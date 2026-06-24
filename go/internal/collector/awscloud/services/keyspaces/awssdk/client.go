// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskeyspaces "github.com/aws/aws-sdk-go-v2/service/keyspaces"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	keyspacesservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/keyspaces"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const listLimit int32 = 100

// apiClient is the metadata-only subset of the AWS Keyspaces control-plane API
// the adapter calls. It is deliberately limited to ListKeyspaces, GetKeyspace,
// ListTables, GetTable, and ListTagsForResource: no ExecuteStatement,
// BatchStatement, Select, RestoreTable, or any keyspace/table mutation API is
// reachable through this interface, so the adapter cannot read table rows or
// cells or mutate resources.
type apiClient interface {
	ListKeyspaces(
		context.Context,
		*awskeyspaces.ListKeyspacesInput,
		...func(*awskeyspaces.Options),
	) (*awskeyspaces.ListKeyspacesOutput, error)
	GetKeyspace(
		context.Context,
		*awskeyspaces.GetKeyspaceInput,
		...func(*awskeyspaces.Options),
	) (*awskeyspaces.GetKeyspaceOutput, error)
	ListTables(
		context.Context,
		*awskeyspaces.ListTablesInput,
		...func(*awskeyspaces.Options),
	) (*awskeyspaces.ListTablesOutput, error)
	GetTable(
		context.Context,
		*awskeyspaces.GetTableInput,
		...func(*awskeyspaces.Options),
	) (*awskeyspaces.GetTableOutput, error)
	ListTagsForResource(
		context.Context,
		*awskeyspaces.ListTagsForResourceInput,
		...func(*awskeyspaces.Options),
	) (*awskeyspaces.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Amazon Keyspaces control-plane calls into scanner-owned
// metadata. It never executes CQL, never runs ExecuteStatement, BatchStatement,
// or Select, never reads table rows or cells, never restores or mutates tables,
// and never mutates keyspaces. Schema column names and types come from the
// control-plane GetTable response and are structural metadata only.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an Amazon Keyspaces SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awskeyspaces.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Amazon Keyspaces keyspace and table metadata visible to the
// configured AWS credentials. It lists keyspaces, then lists and describes each
// table, attaching the parent keyspace ARN so the table-in-keyspace edge joins
// the keyspace node by its published ARN.
func (c *Client) Snapshot(ctx context.Context) (keyspacesservice.Snapshot, error) {
	keyspaces, keyspaceARNByName, err := c.listKeyspaces(ctx)
	if err != nil {
		return keyspacesservice.Snapshot{}, err
	}
	var tables []keyspacesservice.Table
	for _, keyspace := range keyspaces {
		keyspaceTables, err := c.listTables(ctx, keyspace.Name, keyspaceARNByName[keyspace.Name])
		if err != nil {
			return keyspacesservice.Snapshot{}, err
		}
		tables = append(tables, keyspaceTables...)
	}
	return keyspacesservice.Snapshot{Keyspaces: keyspaces, Tables: tables}, nil
}

func (c *Client) listKeyspaces(
	ctx context.Context,
) ([]keyspacesservice.Keyspace, map[string]string, error) {
	var keyspaces []keyspacesservice.Keyspace
	keyspaceARNByName := map[string]string{}
	var nextToken *string
	for {
		var page *awskeyspaces.ListKeyspacesOutput
		err := c.recordAPICall(ctx, "ListKeyspaces", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListKeyspaces(callCtx, &awskeyspaces.ListKeyspacesInput{
				MaxResults: aws.Int32(listLimit),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return keyspaces, keyspaceARNByName, nil
		}
		for _, summary := range page.Keyspaces {
			name := strings.TrimSpace(aws.ToString(summary.KeyspaceName))
			if name == "" {
				continue
			}
			keyspace, err := c.describeKeyspace(ctx, name, summary)
			if err != nil {
				return nil, nil, err
			}
			keyspaces = append(keyspaces, keyspace)
			keyspaceARNByName[keyspace.Name] = keyspace.ARN
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return keyspaces, keyspaceARNByName, nil
		}
	}
}

func (c *Client) describeKeyspace(
	ctx context.Context,
	name string,
	summary keyspacesSummary,
) (keyspacesservice.Keyspace, error) {
	var output *awskeyspaces.GetKeyspaceOutput
	err := c.recordAPICall(ctx, "GetKeyspace", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetKeyspace(callCtx, &awskeyspaces.GetKeyspaceInput{
			KeyspaceName: aws.String(name),
		})
		return err
	})
	if err != nil {
		return keyspacesservice.Keyspace{}, err
	}
	return mapKeyspace(name, summary, output), nil
}

func (c *Client) listTables(
	ctx context.Context,
	keyspaceName string,
	keyspaceARN string,
) ([]keyspacesservice.Table, error) {
	var tables []keyspacesservice.Table
	var nextToken *string
	for {
		var page *awskeyspaces.ListTablesOutput
		err := c.recordAPICall(ctx, "ListTables", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTables(callCtx, &awskeyspaces.ListTablesInput{
				KeyspaceName: aws.String(keyspaceName),
				MaxResults:   aws.Int32(listLimit),
				NextToken:    nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return tables, nil
		}
		for _, summary := range page.Tables {
			tableName := tableNameFromARN(aws.ToString(summary.ResourceArn))
			if tableName == "" {
				continue
			}
			table, ok, err := c.describeTable(ctx, keyspaceName, keyspaceARN, tableName)
			if err != nil {
				return nil, err
			}
			if ok {
				tables = append(tables, table)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return tables, nil
		}
	}
}

func (c *Client) describeTable(
	ctx context.Context,
	keyspaceName string,
	keyspaceARN string,
	tableName string,
) (keyspacesservice.Table, bool, error) {
	var output *awskeyspaces.GetTableOutput
	err := c.recordAPICall(ctx, "GetTable", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetTable(callCtx, &awskeyspaces.GetTableInput{
			KeyspaceName: aws.String(keyspaceName),
			TableName:    aws.String(tableName),
		})
		return err
	})
	if err != nil {
		return keyspacesservice.Table{}, false, err
	}
	if output == nil {
		return keyspacesservice.Table{}, false, nil
	}
	tags, err := c.listTags(ctx, aws.ToString(output.ResourceArn))
	if err != nil {
		return keyspacesservice.Table{}, false, err
	}
	return mapTable(keyspaceARN, output, tags), true, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var tags map[string]string
	var nextToken *string
	for {
		var output *awskeyspaces.ListTagsForResourceOutput
		err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTagsForResource(callCtx, &awskeyspaces.ListTagsForResourceInput{
				ResourceArn: aws.String(resourceARN),
				MaxResults:  aws.Int32(listLimit),
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
		if aws.ToString(nextToken) == "" {
			return tags, nil
		}
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

var _ keyspacesservice.Client = (*Client)(nil)

var _ apiClient = (*awskeyspaces.Client)(nil)
