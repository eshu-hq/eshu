// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awstimestreamwrite "github.com/aws/aws-sdk-go-v2/service/timestreamwrite"
	awstimestreamwritetypes "github.com/aws/aws-sdk-go-v2/service/timestreamwrite/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	timestreamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/timestream"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS Timestream-write API the
// adapter calls. It is deliberately limited to the database/table list reads
// and resource-tag reads. It exposes no WriteRecords, no Query (which lives in
// the separate timestream-query module this package never imports), and no
// Create/Update/Delete mutation, so the adapter cannot read time-series records
// or write Timestream state. The exclusion_test reflects over this interface to
// enforce that contract at build time.
type apiClient interface {
	ListDatabases(
		context.Context,
		*awstimestreamwrite.ListDatabasesInput,
		...func(*awstimestreamwrite.Options),
	) (*awstimestreamwrite.ListDatabasesOutput, error)
	ListTables(
		context.Context,
		*awstimestreamwrite.ListTablesInput,
		...func(*awstimestreamwrite.Options),
	) (*awstimestreamwrite.ListTablesOutput, error)
	ListTagsForResource(
		context.Context,
		*awstimestreamwrite.ListTagsForResourceInput,
		...func(*awstimestreamwrite.Options),
	) (*awstimestreamwrite.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Timestream-write control-plane calls into scanner-owned
// metadata. It never reads time-series records or measures, never runs queries,
// and never calls a Write or mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Timestream SDK adapter for one claimed AWS boundary. The
// Timestream-write management endpoint requires endpoint discovery, which the
// SDK enables automatically for the list operations, so no extra option is set.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awstimestreamwrite.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns Timestream database metadata and the tables under each
// database visible to the configured AWS credentials. Time-series records,
// measure values, and query results are never read.
func (c *Client) Snapshot(ctx context.Context) (timestreamservice.Snapshot, error) {
	databases, err := c.listDatabases(ctx)
	if err != nil {
		return timestreamservice.Snapshot{}, err
	}
	for i := range databases {
		tables, err := c.listTables(ctx, databases[i].Name)
		if err != nil {
			return timestreamservice.Snapshot{}, err
		}
		databases[i].Tables = tables
	}
	return timestreamservice.Snapshot{Databases: databases}, nil
}

func (c *Client) listDatabases(ctx context.Context) ([]timestreamservice.Database, error) {
	var databases []timestreamservice.Database
	var nextToken *string
	for {
		var page *awstimestreamwrite.ListDatabasesOutput
		err := c.recordAPICall(ctx, "ListDatabases", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDatabases(callCtx, &awstimestreamwrite.ListDatabasesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return databases, nil
		}
		for _, database := range page.Databases {
			mapped, err := c.mapDatabase(ctx, database)
			if err != nil {
				return nil, err
			}
			databases = append(databases, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return databases, nil
		}
	}
}

func (c *Client) mapDatabase(
	ctx context.Context,
	database awstimestreamwritetypes.Database,
) (timestreamservice.Database, error) {
	arn := strings.TrimSpace(aws.ToString(database.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return timestreamservice.Database{}, err
	}
	return timestreamservice.Database{
		ARN:             arn,
		Name:            strings.TrimSpace(aws.ToString(database.DatabaseName)),
		KMSKeyID:        strings.TrimSpace(aws.ToString(database.KmsKeyId)),
		TableCount:      database.TableCount,
		CreationTime:    aws.ToTime(database.CreationTime),
		LastUpdatedTime: aws.ToTime(database.LastUpdatedTime),
		Tags:            tags,
	}, nil
}

func (c *Client) listTables(ctx context.Context, databaseName string) ([]timestreamservice.Table, error) {
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		return nil, nil
	}
	var tables []timestreamservice.Table
	var nextToken *string
	for {
		var page *awstimestreamwrite.ListTablesOutput
		err := c.recordAPICall(ctx, "ListTables", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTables(callCtx, &awstimestreamwrite.ListTablesInput{
				DatabaseName: aws.String(databaseName),
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
		for _, table := range page.Tables {
			mapped, err := c.mapTable(ctx, table, databaseName)
			if err != nil {
				return nil, err
			}
			tables = append(tables, mapped)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return tables, nil
		}
	}
}

func (c *Client) mapTable(
	ctx context.Context,
	table awstimestreamwritetypes.Table,
	databaseName string,
) (timestreamservice.Table, error) {
	arn := strings.TrimSpace(aws.ToString(table.Arn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return timestreamservice.Table{}, err
	}
	tableDatabase := strings.TrimSpace(aws.ToString(table.DatabaseName))
	if tableDatabase == "" {
		tableDatabase = strings.TrimSpace(databaseName)
	}
	mapped := timestreamservice.Table{
		ARN:               arn,
		Name:              strings.TrimSpace(aws.ToString(table.TableName)),
		DatabaseName:      tableDatabase,
		State:             strings.TrimSpace(string(table.TableStatus)),
		PartitionKeyNames: partitionKeyNames(table.Schema),
		CreationTime:      aws.ToTime(table.CreationTime),
		LastUpdatedTime:   aws.ToTime(table.LastUpdatedTime),
		Tags:              tags,
	}
	if retention := table.RetentionProperties; retention != nil {
		mapped.MemoryStoreRetentionPeriodInHours = aws.ToInt64(retention.MemoryStoreRetentionPeriodInHours)
		mapped.MagneticStoreRetentionPeriodInDays = aws.ToInt64(retention.MagneticStoreRetentionPeriodInDays)
	}
	applyMagneticStore(&mapped, table.MagneticStoreWriteProperties)
	return mapped, nil
}

// applyMagneticStore copies the magnetic-store write flag and the rejected-data
// S3 report location (bucket name, prefix, encryption option) onto the table.
// It never reads the rejected records themselves; only the report location
// configuration is metadata.
func applyMagneticStore(
	table *timestreamservice.Table,
	properties *awstimestreamwritetypes.MagneticStoreWriteProperties,
) {
	if properties == nil {
		return
	}
	table.MagneticStoreWritesEnabled = aws.ToBool(properties.EnableMagneticStoreWrites)
	location := properties.MagneticStoreRejectedDataLocation
	if location == nil || location.S3Configuration == nil {
		return
	}
	s3Config := location.S3Configuration
	table.RejectedDataS3Bucket = strings.TrimSpace(aws.ToString(s3Config.BucketName))
	table.RejectedDataS3Prefix = strings.TrimSpace(aws.ToString(s3Config.ObjectKeyPrefix))
	table.RejectedDataS3EncryptionOption = strings.TrimSpace(string(s3Config.EncryptionOption))
}

func partitionKeyNames(schema *awstimestreamwritetypes.Schema) []string {
	if schema == nil || len(schema.CompositePartitionKey) == 0 {
		return nil
	}
	names := make([]string, 0, len(schema.CompositePartitionKey))
	for _, key := range schema.CompositePartitionKey {
		if name := strings.TrimSpace(aws.ToString(key.Name)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awstimestreamwrite.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awstimestreamwrite.ListTagsForResourceInput{
			ResourceARN: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.Tags) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.Tags))
	for _, tag := range output.Tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		tags[key] = aws.ToString(tag.Value)
	}
	if len(tags) == 0 {
		return nil, nil
	}
	return tags, nil
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

var _ timestreamservice.Client = (*Client)(nil)

var _ apiClient = (*awstimestreamwrite.Client)(nil)
