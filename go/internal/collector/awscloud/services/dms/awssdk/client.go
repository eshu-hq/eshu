// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdms "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice"
	awsdmstypes "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	dmsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dms"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the metadata-only subset of the AWS DMS API the adapter calls.
// It is deliberately limited to the control-plane describe reads for
// replication instances, replication subnet groups, endpoints, and replication
// tasks, plus the resource-tag read. It exposes no TestConnection, no
// RefreshSchemas, no ReloadTables, no Start/Stop task, and no Create/Update/
// Delete mutation, so the adapter cannot read migrated rows, test live
// connections, or mutate DMS state. The exclusion_test reflects over this
// interface to enforce that contract at build time.
type apiClient interface {
	DescribeReplicationInstances(
		context.Context,
		*awsdms.DescribeReplicationInstancesInput,
		...func(*awsdms.Options),
	) (*awsdms.DescribeReplicationInstancesOutput, error)
	DescribeReplicationSubnetGroups(
		context.Context,
		*awsdms.DescribeReplicationSubnetGroupsInput,
		...func(*awsdms.Options),
	) (*awsdms.DescribeReplicationSubnetGroupsOutput, error)
	DescribeEndpoints(
		context.Context,
		*awsdms.DescribeEndpointsInput,
		...func(*awsdms.Options),
	) (*awsdms.DescribeEndpointsOutput, error)
	DescribeReplicationTasks(
		context.Context,
		*awsdms.DescribeReplicationTasksInput,
		...func(*awsdms.Options),
	) (*awsdms.DescribeReplicationTasksOutput, error)
	ListTagsForResource(
		context.Context,
		*awsdms.ListTagsForResourceInput,
		...func(*awsdms.Options),
	) (*awsdms.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK DMS control-plane calls into scanner-owned metadata. It
// never reads migrated rows, never tests live endpoint connections, never reads
// endpoint credentials or task settings, and never calls a mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DMS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdms.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns DMS replication instance, replication subnet group,
// endpoint, and replication task metadata visible to the configured AWS
// credentials. Migrated rows, endpoint credentials, task settings, and
// table-mapping bodies are never read.
func (c *Client) Snapshot(ctx context.Context) (dmsservice.Snapshot, error) {
	subnetGroups, err := c.listSubnetGroups(ctx)
	if err != nil {
		return dmsservice.Snapshot{}, err
	}
	subnetGroupIndex := indexSubnetGroups(subnetGroups)
	instances, err := c.listInstances(ctx, subnetGroupIndex)
	if err != nil {
		return dmsservice.Snapshot{}, err
	}
	endpoints, err := c.listEndpoints(ctx)
	if err != nil {
		return dmsservice.Snapshot{}, err
	}
	tasks, err := c.listTasks(ctx)
	if err != nil {
		return dmsservice.Snapshot{}, err
	}
	return dmsservice.Snapshot{
		ReplicationInstances: instances,
		SubnetGroups:         subnetGroups,
		Endpoints:            endpoints,
		Tasks:                tasks,
	}, nil
}

func (c *Client) listSubnetGroups(ctx context.Context) ([]dmsservice.ReplicationSubnetGroup, error) {
	var groups []dmsservice.ReplicationSubnetGroup
	var marker *string
	for {
		var page *awsdms.DescribeReplicationSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeReplicationSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeReplicationSubnetGroups(callCtx, &awsdms.DescribeReplicationSubnetGroupsInput{
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, group := range page.ReplicationSubnetGroups {
			groups = append(groups, mapSubnetGroup(group))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

func (c *Client) listInstances(
	ctx context.Context,
	subnetGroups map[string]dmsservice.ReplicationSubnetGroup,
) ([]dmsservice.ReplicationInstance, error) {
	var instances []dmsservice.ReplicationInstance
	var marker *string
	for {
		var page *awsdms.DescribeReplicationInstancesOutput
		err := c.recordAPICall(ctx, "DescribeReplicationInstances", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeReplicationInstances(callCtx, &awsdms.DescribeReplicationInstancesInput{
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return instances, nil
		}
		for _, instance := range page.ReplicationInstances {
			mapped, err := c.mapInstance(ctx, instance, subnetGroups)
			if err != nil {
				return nil, err
			}
			instances = append(instances, mapped)
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return instances, nil
		}
	}
}

func (c *Client) listEndpoints(ctx context.Context) ([]dmsservice.Endpoint, error) {
	var endpoints []dmsservice.Endpoint
	var marker *string
	for {
		var page *awsdms.DescribeEndpointsOutput
		err := c.recordAPICall(ctx, "DescribeEndpoints", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeEndpoints(callCtx, &awsdms.DescribeEndpointsInput{
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return endpoints, nil
		}
		for _, endpoint := range page.Endpoints {
			endpoints = append(endpoints, mapEndpoint(endpoint))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return endpoints, nil
		}
	}
}

func (c *Client) listTasks(ctx context.Context) ([]dmsservice.ReplicationTask, error) {
	var tasks []dmsservice.ReplicationTask
	var marker *string
	for {
		var page *awsdms.DescribeReplicationTasksOutput
		err := c.recordAPICall(ctx, "DescribeReplicationTasks", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeReplicationTasks(callCtx, &awsdms.DescribeReplicationTasksInput{
				Marker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return tasks, nil
		}
		for _, task := range page.ReplicationTasks {
			mapped, err := c.mapTask(ctx, task)
			if err != nil {
				return nil, err
			}
			tasks = append(tasks, mapped)
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return tasks, nil
		}
	}
}

func (c *Client) mapInstance(
	ctx context.Context,
	instance awsdmstypes.ReplicationInstance,
	subnetGroups map[string]dmsservice.ReplicationSubnetGroup,
) (dmsservice.ReplicationInstance, error) {
	arn := strings.TrimSpace(aws.ToString(instance.ReplicationInstanceArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return dmsservice.ReplicationInstance{}, err
	}
	return buildInstance(instance, subnetGroups, tags), nil
}

func (c *Client) mapTask(
	ctx context.Context,
	task awsdmstypes.ReplicationTask,
) (dmsservice.ReplicationTask, error) {
	arn := strings.TrimSpace(aws.ToString(task.ReplicationTaskArn))
	tags, err := c.listTags(ctx, arn)
	if err != nil {
		return dmsservice.ReplicationTask{}, err
	}
	return buildTask(task, tags), nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsdms.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsdms.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	if len(output.TagList) == 0 {
		return nil, nil
	}
	tags := make(map[string]string, len(output.TagList))
	for _, tag := range output.TagList {
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

var (
	_ dmsservice.Client = (*Client)(nil)
	_ apiClient         = (*awsdms.Client)(nil)
)
