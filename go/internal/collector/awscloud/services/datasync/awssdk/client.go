// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatasync "github.com/aws/aws-sdk-go-v2/service/datasync"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	datasyncservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/datasync"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the minimal AWS SDK DataSync surface the adapter consumes. It is
// read-only by construction: only List and Describe operations appear, so no
// create, start, update, or delete API is reachable from this package. The
// per-flavor DescribeLocation* methods let the adapter resolve a location's
// backing AWS storage identity without a mutation path.
type apiClient interface {
	ListTasks(context.Context, *awsdatasync.ListTasksInput, ...func(*awsdatasync.Options)) (*awsdatasync.ListTasksOutput, error)
	DescribeTask(context.Context, *awsdatasync.DescribeTaskInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeTaskOutput, error)
	ListLocations(context.Context, *awsdatasync.ListLocationsInput, ...func(*awsdatasync.Options)) (*awsdatasync.ListLocationsOutput, error)
	DescribeLocationS3(context.Context, *awsdatasync.DescribeLocationS3Input, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationS3Output, error)
	DescribeLocationEfs(context.Context, *awsdatasync.DescribeLocationEfsInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationEfsOutput, error)
	DescribeLocationFsxLustre(context.Context, *awsdatasync.DescribeLocationFsxLustreInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxLustreOutput, error)
	DescribeLocationFsxOntap(context.Context, *awsdatasync.DescribeLocationFsxOntapInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxOntapOutput, error)
	DescribeLocationFsxOpenZfs(context.Context, *awsdatasync.DescribeLocationFsxOpenZfsInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxOpenZfsOutput, error)
	DescribeLocationFsxWindows(context.Context, *awsdatasync.DescribeLocationFsxWindowsInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxWindowsOutput, error)
	ListAgents(context.Context, *awsdatasync.ListAgentsInput, ...func(*awsdatasync.Options)) (*awsdatasync.ListAgentsOutput, error)
	DescribeAgent(context.Context, *awsdatasync.DescribeAgentInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeAgentOutput, error)
}

// Client adapts AWS SDK DataSync pagination and per-resource describe reads
// into scanner-owned metadata. The adapter never calls CreateTask, StartTaskExecution,
// CancelTaskExecution, UpdateTask, DeleteTask, CreateLocation*, CreateAgent, or
// any other mutation API. It reads only the safe location fields needed to join
// the backing S3 bucket, EFS file system, FSx file system, and IAM role; it
// never reads object-storage access keys, server certificates, or passwords.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DataSync SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdatasync.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListTasks reads DataSync task identities with ListTasks and resolves each
// task's source/destination location ARNs, CloudWatch log group ARN, schedule,
// and status with DescribeTask. The data a task transfers, include/exclude
// filter patterns, and manifest bodies stay outside the scanner contract.
func (c *Client) ListTasks(ctx context.Context) ([]datasyncservice.Task, error) {
	var tasks []datasyncservice.Task
	var nextToken *string
	for {
		var page *awsdatasync.ListTasksOutput
		err := c.recordAPICall(ctx, "ListTasks", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListTasks(callCtx, &awsdatasync.ListTasksInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return tasks, nil
		}
		for _, entry := range page.Tasks {
			arn := strings.TrimSpace(aws.ToString(entry.TaskArn))
			if arn == "" {
				continue
			}
			task, err := c.describeTask(ctx, arn)
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, task)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return tasks, nil
		}
	}
}

func (c *Client) describeTask(ctx context.Context, arn string) (datasyncservice.Task, error) {
	var output *awsdatasync.DescribeTaskOutput
	err := c.recordAPICall(ctx, "DescribeTask", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeTask(callCtx, &awsdatasync.DescribeTaskInput{TaskArn: aws.String(arn)})
		return err
	})
	if err != nil {
		return datasyncservice.Task{}, err
	}
	if output == nil {
		return datasyncservice.Task{ARN: arn}, nil
	}
	task := datasyncservice.Task{
		ARN:                    arn,
		Name:                   strings.TrimSpace(aws.ToString(output.Name)),
		Status:                 strings.TrimSpace(string(output.Status)),
		SourceLocationARN:      strings.TrimSpace(aws.ToString(output.SourceLocationArn)),
		DestinationLocationARN: strings.TrimSpace(aws.ToString(output.DestinationLocationArn)),
		CloudWatchLogGroupARN:  strings.TrimSpace(aws.ToString(output.CloudWatchLogGroupArn)),
		TaskMode:               strings.TrimSpace(string(output.TaskMode)),
		CreationTime:           aws.ToTime(output.CreationTime),
	}
	if output.Schedule != nil {
		task.ScheduleExpression = strings.TrimSpace(aws.ToString(output.Schedule.ScheduleExpression))
		task.ScheduleStatus = strings.TrimSpace(string(output.Schedule.Status))
	}
	return task, nil
}

// ListLocations reads DataSync location identities with ListLocations and
// resolves each location's backing storage identity with the flavor-specific
// DescribeLocation* read selected from the location URI scheme. Object-storage
// access keys, server certificates, and SMB/object-storage passwords are never
// read; for those flavors only the location identity and URI survive.
func (c *Client) ListLocations(ctx context.Context) ([]datasyncservice.Location, error) {
	var locations []datasyncservice.Location
	var nextToken *string
	for {
		var page *awsdatasync.ListLocationsOutput
		err := c.recordAPICall(ctx, "ListLocations", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListLocations(callCtx, &awsdatasync.ListLocationsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return locations, nil
		}
		for _, entry := range page.Locations {
			arn := strings.TrimSpace(aws.ToString(entry.LocationArn))
			if arn == "" {
				continue
			}
			location, err := c.describeLocation(ctx, arn, strings.TrimSpace(aws.ToString(entry.LocationUri)))
			if err != nil {
				return nil, err
			}
			locations = append(locations, location)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return locations, nil
		}
	}
}

// describeLocation resolves a location's backing-storage identity by dispatching
// on the URI scheme. The ARN and URI from ListLocations are always carried; the
// flavor-specific describe read fills the safe backing-resource identity fields.
func (c *Client) describeLocation(ctx context.Context, arn, listURI string) (datasyncservice.Location, error) {
	location := datasyncservice.Location{
		ARN:  arn,
		URI:  listURI,
		Type: locationTypeFromURI(listURI),
	}
	switch {
	case strings.HasPrefix(listURI, "s3://"):
		return c.describeLocationS3(ctx, location)
	case strings.HasPrefix(listURI, "efs://"):
		return c.describeLocationEFS(ctx, location)
	case strings.HasPrefix(listURI, "fsxl://"):
		return c.describeLocationFsxLustre(ctx, location)
	case strings.HasPrefix(listURI, "fsxn://"), strings.HasPrefix(listURI, "fsxo://"):
		return c.describeLocationFsxOntap(ctx, location)
	case strings.HasPrefix(listURI, "fsxz://"):
		return c.describeLocationFsxOpenZfs(ctx, location)
	case strings.HasPrefix(listURI, "fsxw://"):
		return c.describeLocationFsxWindows(ctx, location)
	default:
		return location, nil
	}
}

func (c *Client) describeLocationS3(ctx context.Context, location datasyncservice.Location) (datasyncservice.Location, error) {
	var output *awsdatasync.DescribeLocationS3Output
	err := c.recordAPICall(ctx, "DescribeLocationS3", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLocationS3(callCtx, &awsdatasync.DescribeLocationS3Input{LocationArn: aws.String(location.ARN)})
		return err
	})
	if err != nil {
		return datasyncservice.Location{}, err
	}
	if output == nil {
		return location, nil
	}
	if uri := strings.TrimSpace(aws.ToString(output.LocationUri)); uri != "" {
		location.URI = uri
	}
	if bucket, ok := bucketFromS3URI(location.URI); ok {
		location.S3BucketName = bucket
	}
	if output.S3Config != nil {
		location.IAMRoleARN = strings.TrimSpace(aws.ToString(output.S3Config.BucketAccessRoleArn))
	}
	return location, nil
}

func (c *Client) describeLocationEFS(ctx context.Context, location datasyncservice.Location) (datasyncservice.Location, error) {
	var output *awsdatasync.DescribeLocationEfsOutput
	err := c.recordAPICall(ctx, "DescribeLocationEfs", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLocationEfs(callCtx, &awsdatasync.DescribeLocationEfsInput{LocationArn: aws.String(location.ARN)})
		return err
	})
	if err != nil {
		return datasyncservice.Location{}, err
	}
	if output == nil {
		return location, nil
	}
	if uri := strings.TrimSpace(aws.ToString(output.LocationUri)); uri != "" {
		location.URI = uri
	}
	if fsID, ok := efsFileSystemIDFromURI(location.URI); ok {
		location.EFSFileSystemID = fsID
	}
	location.IAMRoleARN = strings.TrimSpace(aws.ToString(output.FileSystemAccessRoleArn))
	return location, nil
}

func (c *Client) describeLocationFsxLustre(ctx context.Context, location datasyncservice.Location) (datasyncservice.Location, error) {
	var output *awsdatasync.DescribeLocationFsxLustreOutput
	err := c.recordAPICall(ctx, "DescribeLocationFsxLustre", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLocationFsxLustre(callCtx, &awsdatasync.DescribeLocationFsxLustreInput{LocationArn: aws.String(location.ARN)})
		return err
	})
	if err != nil {
		return datasyncservice.Location{}, err
	}
	if output == nil {
		return location, nil
	}
	if uri := strings.TrimSpace(aws.ToString(output.LocationUri)); uri != "" {
		location.URI = uri
	}
	if fsID, ok := fsxFileSystemIDFromURI(location.URI); ok {
		location.FSxFileSystemID = fsID
	}
	return location, nil
}

func (c *Client) describeLocationFsxOntap(ctx context.Context, location datasyncservice.Location) (datasyncservice.Location, error) {
	var output *awsdatasync.DescribeLocationFsxOntapOutput
	err := c.recordAPICall(ctx, "DescribeLocationFsxOntap", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLocationFsxOntap(callCtx, &awsdatasync.DescribeLocationFsxOntapInput{LocationArn: aws.String(location.ARN)})
		return err
	})
	if err != nil {
		return datasyncservice.Location{}, err
	}
	if output == nil {
		return location, nil
	}
	if uri := strings.TrimSpace(aws.ToString(output.LocationUri)); uri != "" {
		location.URI = uri
	}
	location.FSxFileSystemARN = strings.TrimSpace(aws.ToString(output.FsxFilesystemArn))
	if fsID, ok := fsxFileSystemIDFromURI(location.URI); ok {
		location.FSxFileSystemID = fsID
	}
	return location, nil
}

func (c *Client) describeLocationFsxOpenZfs(ctx context.Context, location datasyncservice.Location) (datasyncservice.Location, error) {
	var output *awsdatasync.DescribeLocationFsxOpenZfsOutput
	err := c.recordAPICall(ctx, "DescribeLocationFsxOpenZfs", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLocationFsxOpenZfs(callCtx, &awsdatasync.DescribeLocationFsxOpenZfsInput{LocationArn: aws.String(location.ARN)})
		return err
	})
	if err != nil {
		return datasyncservice.Location{}, err
	}
	if output == nil {
		return location, nil
	}
	if uri := strings.TrimSpace(aws.ToString(output.LocationUri)); uri != "" {
		location.URI = uri
	}
	if fsID, ok := fsxFileSystemIDFromURI(location.URI); ok {
		location.FSxFileSystemID = fsID
	}
	return location, nil
}

func (c *Client) describeLocationFsxWindows(ctx context.Context, location datasyncservice.Location) (datasyncservice.Location, error) {
	var output *awsdatasync.DescribeLocationFsxWindowsOutput
	err := c.recordAPICall(ctx, "DescribeLocationFsxWindows", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeLocationFsxWindows(callCtx, &awsdatasync.DescribeLocationFsxWindowsInput{LocationArn: aws.String(location.ARN)})
		return err
	})
	if err != nil {
		return datasyncservice.Location{}, err
	}
	if output == nil {
		return location, nil
	}
	if uri := strings.TrimSpace(aws.ToString(output.LocationUri)); uri != "" {
		location.URI = uri
	}
	if fsID, ok := fsxFileSystemIDFromURI(location.URI); ok {
		location.FSxFileSystemID = fsID
	}
	return location, nil
}

// ListAgents reads DataSync agent identities with ListAgents and resolves each
// agent's status, endpoint type, and platform version with DescribeAgent.
// Private-link endpoint network details beyond the endpoint type stay outside
// the scanner contract.
func (c *Client) ListAgents(ctx context.Context) ([]datasyncservice.Agent, error) {
	var agents []datasyncservice.Agent
	var nextToken *string
	for {
		var page *awsdatasync.ListAgentsOutput
		err := c.recordAPICall(ctx, "ListAgents", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListAgents(callCtx, &awsdatasync.ListAgentsInput{NextToken: nextToken})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return agents, nil
		}
		for _, entry := range page.Agents {
			arn := strings.TrimSpace(aws.ToString(entry.AgentArn))
			if arn == "" {
				continue
			}
			agent, err := c.describeAgent(ctx, arn, strings.TrimSpace(aws.ToString(entry.Name)))
			if err != nil {
				return nil, err
			}
			agents = append(agents, agent)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return agents, nil
		}
	}
}

func (c *Client) describeAgent(ctx context.Context, arn, listName string) (datasyncservice.Agent, error) {
	var output *awsdatasync.DescribeAgentOutput
	err := c.recordAPICall(ctx, "DescribeAgent", func(callCtx context.Context) error {
		var err error
		output, err = c.client.DescribeAgent(callCtx, &awsdatasync.DescribeAgentInput{AgentArn: aws.String(arn)})
		return err
	})
	if err != nil {
		return datasyncservice.Agent{}, err
	}
	if output == nil {
		return datasyncservice.Agent{ARN: arn, Name: listName}, nil
	}
	agent := datasyncservice.Agent{
		ARN:          arn,
		Name:         firstNonEmpty(strings.TrimSpace(aws.ToString(output.Name)), listName),
		Status:       strings.TrimSpace(string(output.Status)),
		EndpointType: strings.TrimSpace(string(output.EndpointType)),
		CreationTime: aws.ToTime(output.CreationTime),
	}
	if output.Platform != nil {
		agent.PlatformVersion = strings.TrimSpace(aws.ToString(output.Platform.Version))
	}
	return agent, nil
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

var _ datasyncservice.Client = (*Client)(nil)

var _ apiClient = (*awsdatasync.Client)(nil)
