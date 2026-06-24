// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk //nolint:filelength // 631 lines: Glue SDK pagination, HidePassword/IncludeGraph enforcement, and safe metadata mapping. Per services/glue/awssdk/AGENTS.md the SDK adapter owns pagination, retries, throttling, and credential loading so scanner.go can stay a thin fact selector.

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	awsgluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	glueservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/glue"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	GetDatabases(context.Context, *awsglue.GetDatabasesInput, ...func(*awsglue.Options)) (*awsglue.GetDatabasesOutput, error)
	GetTables(context.Context, *awsglue.GetTablesInput, ...func(*awsglue.Options)) (*awsglue.GetTablesOutput, error)
	GetCrawlers(context.Context, *awsglue.GetCrawlersInput, ...func(*awsglue.Options)) (*awsglue.GetCrawlersOutput, error)
	GetJobs(context.Context, *awsglue.GetJobsInput, ...func(*awsglue.Options)) (*awsglue.GetJobsOutput, error)
	GetTriggers(context.Context, *awsglue.GetTriggersInput, ...func(*awsglue.Options)) (*awsglue.GetTriggersOutput, error)
	ListWorkflows(context.Context, *awsglue.ListWorkflowsInput, ...func(*awsglue.Options)) (*awsglue.ListWorkflowsOutput, error)
	GetWorkflow(context.Context, *awsglue.GetWorkflowInput, ...func(*awsglue.Options)) (*awsglue.GetWorkflowOutput, error)
	GetConnections(context.Context, *awsglue.GetConnectionsInput, ...func(*awsglue.Options)) (*awsglue.GetConnectionsOutput, error)
}

// Client adapts AWS SDK Glue pagination into scanner-owned metadata. The
// adapter never calls StartCrawler, StartJobRun, BatchStopJobRun, or any
// Create/Update/Delete API. GetConnections is always called with
// HidePassword=true so passwords stay inside AWS.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Glue SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsglue.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDatabases reads Glue Data Catalog databases and the catalog tables that
// live under each one. Column statistics with sample values, partition value
// samples, and any field that can leak row-level content are intentionally
// excluded.
func (c *Client) ListDatabases(ctx context.Context) ([]glueservice.Database, error) {
	var databases []glueservice.Database
	var nextToken *string
	for {
		var page *awsglue.GetDatabasesOutput
		err := c.recordAPICall(ctx, "GetDatabases", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetDatabases(callCtx, &awsglue.GetDatabasesInput{
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
		for _, database := range page.DatabaseList {
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
	database awsgluetypes.Database,
) (glueservice.Database, error) {
	name := aws.ToString(database.Name)
	tables, err := c.listTables(ctx, aws.ToString(database.CatalogId), name)
	if err != nil {
		return glueservice.Database{}, err
	}
	return glueservice.Database{
		CatalogID:   strings.TrimSpace(aws.ToString(database.CatalogId)),
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(aws.ToString(database.Description)),
		LocationURI: strings.TrimSpace(aws.ToString(database.LocationUri)),
		CreateTime:  aws.ToTime(database.CreateTime),
		Parameters:  cloneStringMap(database.Parameters),
		Tables:      tables,
	}, nil
}

func (c *Client) listTables(
	ctx context.Context,
	catalogID string,
	databaseName string,
) ([]glueservice.Table, error) {
	databaseName = strings.TrimSpace(databaseName)
	if databaseName == "" {
		return nil, nil
	}
	var tables []glueservice.Table
	var nextToken *string
	for {
		var page *awsglue.GetTablesOutput
		err := c.recordAPICall(ctx, "GetTables", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetTables(callCtx, &awsglue.GetTablesInput{
				CatalogId:    optionalString(catalogID),
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
		for _, table := range page.TableList {
			tables = append(tables, mapTable(table, databaseName))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return tables, nil
		}
	}
}

// ListCrawlers reads Glue crawler metadata. Custom-classifier patterns and raw
// S3 sample paths stay outside the scanner contract.
func (c *Client) ListCrawlers(ctx context.Context) ([]glueservice.Crawler, error) {
	var crawlers []glueservice.Crawler
	var nextToken *string
	for {
		var page *awsglue.GetCrawlersOutput
		err := c.recordAPICall(ctx, "GetCrawlers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetCrawlers(callCtx, &awsglue.GetCrawlersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return crawlers, nil
		}
		for _, crawler := range page.Crawlers {
			crawlers = append(crawlers, mapCrawler(crawler))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return crawlers, nil
		}
	}
}

// ListJobs reads Glue job metadata. Job script bodies, default-argument
// values, and security-configuration secret material stay outside the scanner
// contract; only argument key names survive.
func (c *Client) ListJobs(ctx context.Context) ([]glueservice.Job, error) {
	var jobs []glueservice.Job
	var nextToken *string
	for {
		var page *awsglue.GetJobsOutput
		err := c.recordAPICall(ctx, "GetJobs", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetJobs(callCtx, &awsglue.GetJobsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return jobs, nil
		}
		for _, job := range page.Jobs {
			jobs = append(jobs, mapJob(job))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return jobs, nil
		}
	}
}

// ListTriggers reads Glue trigger metadata including the per-action job names.
func (c *Client) ListTriggers(ctx context.Context) ([]glueservice.Trigger, error) {
	var triggers []glueservice.Trigger
	var nextToken *string
	for {
		var page *awsglue.GetTriggersOutput
		err := c.recordAPICall(ctx, "GetTriggers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetTriggers(callCtx, &awsglue.GetTriggersInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return triggers, nil
		}
		for _, trigger := range page.Triggers {
			triggers = append(triggers, mapTrigger(trigger))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return triggers, nil
		}
	}
}

// ListWorkflows reads Glue workflow names with ListWorkflows and follows up
// with GetWorkflow for safe metadata. The graph payload is requested with
// IncludeGraph=false so workflow-run state and edges stay outside the scanner
// contract.
func (c *Client) ListWorkflows(ctx context.Context) ([]glueservice.Workflow, error) {
	names, err := c.listWorkflowNames(ctx)
	if err != nil {
		return nil, err
	}
	workflows := make([]glueservice.Workflow, 0, len(names))
	for _, name := range names {
		workflow, err := c.getWorkflow(ctx, name)
		if err != nil {
			return nil, err
		}
		if workflow == nil {
			continue
		}
		workflows = append(workflows, *workflow)
	}
	return workflows, nil
}

func (c *Client) listWorkflowNames(ctx context.Context) ([]string, error) {
	var names []string
	var nextToken *string
	for {
		var page *awsglue.ListWorkflowsOutput
		err := c.recordAPICall(ctx, "ListWorkflows", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListWorkflows(callCtx, &awsglue.ListWorkflowsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return names, nil
		}
		for _, name := range page.Workflows {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				names = append(names, trimmed)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return names, nil
		}
	}
}

func (c *Client) getWorkflow(ctx context.Context, name string) (*glueservice.Workflow, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, nil
	}
	var output *awsglue.GetWorkflowOutput
	err := c.recordAPICall(ctx, "GetWorkflow", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetWorkflow(callCtx, &awsglue.GetWorkflowInput{
			Name:         aws.String(trimmed),
			IncludeGraph: aws.Bool(false),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.Workflow == nil {
		return &glueservice.Workflow{Name: trimmed}, nil
	}
	workflow := mapWorkflow(*output.Workflow)
	if workflow.Name == "" {
		workflow.Name = trimmed
	}
	return &workflow, nil
}

// ListConnections reads Glue connection metadata with HidePassword set so AWS
// strips PASSWORD and ENCRYPTED_PASSWORD before delivering the response. Only
// connection-property key names survive the adapter.
func (c *Client) ListConnections(ctx context.Context) ([]glueservice.Connection, error) {
	var connections []glueservice.Connection
	var nextToken *string
	for {
		var page *awsglue.GetConnectionsOutput
		err := c.recordAPICall(ctx, "GetConnections", func(callCtx context.Context) error {
			var err error
			page, err = c.client.GetConnections(callCtx, &awsglue.GetConnectionsInput{
				HidePassword: true,
				NextToken:    nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return connections, nil
		}
		for _, connection := range page.ConnectionList {
			connections = append(connections, mapConnection(connection))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return connections, nil
		}
	}
}

func mapTable(table awsgluetypes.Table, databaseName string) glueservice.Table {
	tableDatabase := strings.TrimSpace(aws.ToString(table.DatabaseName))
	if tableDatabase == "" {
		tableDatabase = strings.TrimSpace(databaseName)
	}
	mapped := glueservice.Table{
		CatalogID:        strings.TrimSpace(aws.ToString(table.CatalogId)),
		DatabaseName:     tableDatabase,
		Name:             strings.TrimSpace(aws.ToString(table.Name)),
		Owner:            strings.TrimSpace(aws.ToString(table.Owner)),
		TableType:        strings.TrimSpace(aws.ToString(table.TableType)),
		Description:      strings.TrimSpace(aws.ToString(table.Description)),
		CreateTime:       aws.ToTime(table.CreateTime),
		UpdateTime:       aws.ToTime(table.UpdateTime),
		LastAccessTime:   aws.ToTime(table.LastAccessTime),
		LastAnalyzedTime: aws.ToTime(table.LastAnalyzedTime),
		Retention:        table.Retention,
		Parameters:       cloneStringMap(table.Parameters),
		PartitionKeys:    columnNames(table.PartitionKeys),
	}
	if table.StorageDescriptor != nil {
		mapped.StorageLocation = strings.TrimSpace(aws.ToString(table.StorageDescriptor.Location))
		mapped.InputFormat = strings.TrimSpace(aws.ToString(table.StorageDescriptor.InputFormat))
		mapped.OutputFormat = strings.TrimSpace(aws.ToString(table.StorageDescriptor.OutputFormat))
		mapped.Compressed = table.StorageDescriptor.Compressed
		mapped.Columns = columnNames(table.StorageDescriptor.Columns)
		if table.StorageDescriptor.SerdeInfo != nil {
			mapped.SerdeName = strings.TrimSpace(aws.ToString(table.StorageDescriptor.SerdeInfo.Name))
			mapped.SerdeLibrary = strings.TrimSpace(aws.ToString(table.StorageDescriptor.SerdeInfo.SerializationLibrary))
		}
	}
	return mapped
}

func mapCrawler(crawler awsgluetypes.Crawler) glueservice.Crawler {
	mapped := glueservice.Crawler{
		Name:                 strings.TrimSpace(aws.ToString(crawler.Name)),
		Description:          strings.TrimSpace(aws.ToString(crawler.Description)),
		RoleARN:              strings.TrimSpace(aws.ToString(crawler.Role)),
		DatabaseName:         strings.TrimSpace(aws.ToString(crawler.DatabaseName)),
		TablePrefix:          strings.TrimSpace(aws.ToString(crawler.TablePrefix)),
		State:                strings.TrimSpace(string(crawler.State)),
		CreationTime:         aws.ToTime(crawler.CreationTime),
		LastUpdated:          aws.ToTime(crawler.LastUpdated),
		ConfigurationVersion: "",
	}
	if crawler.Schedule != nil {
		mapped.Schedule = strings.TrimSpace(aws.ToString(crawler.Schedule.ScheduleExpression))
	}
	if crawler.RecrawlPolicy != nil {
		mapped.RecrawlBehavior = strings.TrimSpace(string(crawler.RecrawlPolicy.RecrawlBehavior))
	}
	if crawler.Targets != nil {
		mapped.S3TargetCount = len(crawler.Targets.S3Targets)
		mapped.JDBCTargetCount = len(crawler.Targets.JdbcTargets)
		mapped.DynamoDBTargetCount = len(crawler.Targets.DynamoDBTargets)
		mapped.CatalogTargetCount = len(crawler.Targets.CatalogTargets)
		mapped.MongoDBTargetCount = len(crawler.Targets.MongoDBTargets)
		mapped.DeltaTargetCount = len(crawler.Targets.DeltaTargets)
		mapped.IcebergTargetCount = len(crawler.Targets.IcebergTargets)
		mapped.HudiTargetCount = len(crawler.Targets.HudiTargets)
	}
	return mapped
}

func mapJob(job awsgluetypes.Job) glueservice.Job {
	mapped := glueservice.Job{
		Name:                  strings.TrimSpace(aws.ToString(job.Name)),
		Description:           strings.TrimSpace(aws.ToString(job.Description)),
		RoleARN:               strings.TrimSpace(aws.ToString(job.Role)),
		GlueVersion:           strings.TrimSpace(aws.ToString(job.GlueVersion)),
		WorkerType:            strings.TrimSpace(string(job.WorkerType)),
		NumberOfWorkers:       aws.ToInt32(job.NumberOfWorkers),
		MaxCapacity:           aws.ToFloat64(job.MaxCapacity),
		MaxRetries:            job.MaxRetries,
		Timeout:               aws.ToInt32(job.Timeout),
		CreatedOn:             aws.ToTime(job.CreatedOn),
		LastModifiedOn:        aws.ToTime(job.LastModifiedOn),
		SecurityConfiguration: strings.TrimSpace(aws.ToString(job.SecurityConfiguration)),
		DefaultArgKeys:        mapKeys(job.DefaultArguments),
		NonOverridableArgKeys: mapKeys(job.NonOverridableArguments),
	}
	if job.Command != nil {
		mapped.CommandName = strings.TrimSpace(aws.ToString(job.Command.Name))
		mapped.ScriptLanguage = strings.TrimSpace(aws.ToString(job.Command.PythonVersion))
		mapped.ScriptLocation = strings.TrimSpace(aws.ToString(job.Command.ScriptLocation))
	}
	return mapped
}

func mapTrigger(trigger awsgluetypes.Trigger) glueservice.Trigger {
	mapped := glueservice.Trigger{
		Name:         strings.TrimSpace(aws.ToString(trigger.Name)),
		Type:         strings.TrimSpace(string(trigger.Type)),
		State:        strings.TrimSpace(string(trigger.State)),
		Description:  strings.TrimSpace(aws.ToString(trigger.Description)),
		Schedule:     strings.TrimSpace(aws.ToString(trigger.Schedule)),
		WorkflowName: strings.TrimSpace(aws.ToString(trigger.WorkflowName)),
	}
	for _, action := range trigger.Actions {
		if name := strings.TrimSpace(aws.ToString(action.JobName)); name != "" {
			mapped.ActionJobs = append(mapped.ActionJobs, name)
		}
	}
	return mapped
}

func mapWorkflow(workflow awsgluetypes.Workflow) glueservice.Workflow {
	mapped := glueservice.Workflow{
		Name:             strings.TrimSpace(aws.ToString(workflow.Name)),
		Description:      strings.TrimSpace(aws.ToString(workflow.Description)),
		CreatedOn:        aws.ToTime(workflow.CreatedOn),
		LastModifiedOn:   aws.ToTime(workflow.LastModifiedOn),
		MaxConcurrentRun: aws.ToInt32(workflow.MaxConcurrentRuns),
	}
	mapped.DefaultRunKeys = mapKeys(workflow.DefaultRunProperties)
	return mapped
}

func mapConnection(connection awsgluetypes.Connection) glueservice.Connection {
	mapped := glueservice.Connection{
		Name:            strings.TrimSpace(aws.ToString(connection.Name)),
		Description:     strings.TrimSpace(aws.ToString(connection.Description)),
		ConnectionType:  strings.TrimSpace(string(connection.ConnectionType)),
		CreationTime:    aws.ToTime(connection.CreationTime),
		LastUpdatedTime: aws.ToTime(connection.LastUpdatedTime),
		LastUpdatedBy:   strings.TrimSpace(aws.ToString(connection.LastUpdatedBy)),
		MatchCriteria:   cloneStringSlice(connection.MatchCriteria),
		PropertyKeys:    propertyKeysOnly(connection.ConnectionProperties),
	}
	if connection.PhysicalConnectionRequirements != nil {
		mapped.PhysicalRequirementsAZ = strings.TrimSpace(aws.ToString(connection.PhysicalConnectionRequirements.AvailabilityZone))
		mapped.SubnetID = strings.TrimSpace(aws.ToString(connection.PhysicalConnectionRequirements.SubnetId))
		mapped.SecurityGroupIDs = cloneStringSlice(connection.PhysicalConnectionRequirements.SecurityGroupIdList)
	}
	return mapped
}

func columnNames(columns []awsgluetypes.Column) []string {
	if len(columns) == 0 {
		return nil
	}
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		if name := strings.TrimSpace(aws.ToString(column.Name)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// mapKeys returns the trimmed, lexicographically sorted set of non-empty keys
// in input. Sorting is required because Go map iteration order is randomized,
// which would otherwise make `default_argument_keys`, workflow default keys,
// and connection `property_keys` fact payloads vary across scans for identical
// Glue state.
func mapKeys(input map[string]string) []string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}

func propertyKeysOnly(input map[string]string) []string {
	return mapKeys(input)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStringSlice(input []string) []string {
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

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return aws.String(value)
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

var _ glueservice.Client = (*Client)(nil)

var _ apiClient = (*awsglue.Client)(nil)
