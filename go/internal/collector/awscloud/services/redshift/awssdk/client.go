// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsredshift "github.com/aws/aws-sdk-go-v2/service/redshift"
	awsserverless "github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	redshiftservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/redshift"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeMaxRecords int32 = 100

type provisionedAPI interface {
	DescribeClusters(context.Context, *awsredshift.DescribeClustersInput, ...func(*awsredshift.Options)) (*awsredshift.DescribeClustersOutput, error)
	DescribeClusterParameterGroups(context.Context, *awsredshift.DescribeClusterParameterGroupsInput, ...func(*awsredshift.Options)) (*awsredshift.DescribeClusterParameterGroupsOutput, error)
	DescribeClusterSubnetGroups(context.Context, *awsredshift.DescribeClusterSubnetGroupsInput, ...func(*awsredshift.Options)) (*awsredshift.DescribeClusterSubnetGroupsOutput, error)
	DescribeClusterSnapshots(context.Context, *awsredshift.DescribeClusterSnapshotsInput, ...func(*awsredshift.Options)) (*awsredshift.DescribeClusterSnapshotsOutput, error)
	DescribeScheduledActions(context.Context, *awsredshift.DescribeScheduledActionsInput, ...func(*awsredshift.Options)) (*awsredshift.DescribeScheduledActionsOutput, error)
}

type serverlessAPI interface {
	ListNamespaces(context.Context, *awsserverless.ListNamespacesInput, ...func(*awsserverless.Options)) (*awsserverless.ListNamespacesOutput, error)
	ListWorkgroups(context.Context, *awsserverless.ListWorkgroupsInput, ...func(*awsserverless.Options)) (*awsserverless.ListWorkgroupsOutput, error)
	ListTagsForResource(context.Context, *awsserverless.ListTagsForResourceInput, ...func(*awsserverless.Options)) (*awsserverless.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Redshift and Redshift Serverless control-plane calls
// into scanner-owned metadata. It never connects to warehouses, runs queries,
// reads snapshot contents, reads master user passwords, or calls any mutation
// API.
type Client struct {
	provisioned provisionedAPI
	serverless  serverlessAPI
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Redshift SDK adapter for one claimed AWS boundary covering
// provisioned Redshift and Redshift Serverless.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		provisioned: awsredshift.NewFromConfig(config),
		serverless:  awsserverless.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListClusters returns provisioned Redshift cluster metadata visible to the
// configured AWS credentials.
func (c *Client) ListClusters(ctx context.Context) ([]redshiftservice.Cluster, error) {
	var clusters []redshiftservice.Cluster
	var marker *string
	for {
		var page *awsredshift.DescribeClustersOutput
		err := c.recordAPICall(ctx, "DescribeClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.provisioned.DescribeClusters(callCtx, &awsredshift.DescribeClustersInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return clusters, nil
		}
		for _, raw := range page.Clusters {
			clusters = append(clusters, mapCluster(raw, c.boundary))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return clusters, nil
		}
	}
}

// ListClusterParameterGroups returns cluster parameter group metadata visible
// to the configured AWS credentials.
func (c *Client) ListClusterParameterGroups(ctx context.Context) ([]redshiftservice.ClusterParameterGroup, error) {
	var groups []redshiftservice.ClusterParameterGroup
	var marker *string
	for {
		var page *awsredshift.DescribeClusterParameterGroupsOutput
		err := c.recordAPICall(ctx, "DescribeClusterParameterGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.provisioned.DescribeClusterParameterGroups(callCtx, &awsredshift.DescribeClusterParameterGroupsInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, raw := range page.ParameterGroups {
			groups = append(groups, mapClusterParameterGroup(c.boundary, raw))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

// ListClusterSubnetGroups returns cluster subnet group metadata visible to the
// configured AWS credentials.
func (c *Client) ListClusterSubnetGroups(ctx context.Context) ([]redshiftservice.ClusterSubnetGroup, error) {
	var groups []redshiftservice.ClusterSubnetGroup
	var marker *string
	for {
		var page *awsredshift.DescribeClusterSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeClusterSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.provisioned.DescribeClusterSubnetGroups(callCtx, &awsredshift.DescribeClusterSubnetGroupsInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, raw := range page.ClusterSubnetGroups {
			groups = append(groups, mapClusterSubnetGroup(c.boundary, raw))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

// ListClusterSnapshots returns cluster snapshot metadata visible to the
// configured AWS credentials. The adapter never reads snapshot contents.
func (c *Client) ListClusterSnapshots(ctx context.Context) ([]redshiftservice.ClusterSnapshot, error) {
	var snapshots []redshiftservice.ClusterSnapshot
	var marker *string
	for {
		var page *awsredshift.DescribeClusterSnapshotsOutput
		err := c.recordAPICall(ctx, "DescribeClusterSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = c.provisioned.DescribeClusterSnapshots(callCtx, &awsredshift.DescribeClusterSnapshotsInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return snapshots, nil
		}
		for _, raw := range page.Snapshots {
			snapshots = append(snapshots, mapClusterSnapshot(c.boundary, raw))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return snapshots, nil
		}
	}
}

// ListScheduledActions returns Redshift scheduled action metadata visible to
// the configured AWS credentials.
func (c *Client) ListScheduledActions(ctx context.Context) ([]redshiftservice.ScheduledAction, error) {
	var actions []redshiftservice.ScheduledAction
	var marker *string
	for {
		var page *awsredshift.DescribeScheduledActionsOutput
		err := c.recordAPICall(ctx, "DescribeScheduledActions", func(callCtx context.Context) error {
			var err error
			page, err = c.provisioned.DescribeScheduledActions(callCtx, &awsredshift.DescribeScheduledActionsInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return actions, nil
		}
		for _, raw := range page.ScheduledActions {
			actions = append(actions, mapScheduledAction(raw))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return actions, nil
		}
	}
}

// ListServerlessNamespaces returns Redshift Serverless namespace metadata
// visible to the configured AWS credentials.
func (c *Client) ListServerlessNamespaces(ctx context.Context) ([]redshiftservice.ServerlessNamespace, error) {
	var namespaces []redshiftservice.ServerlessNamespace
	var nextToken *string
	for {
		var page *awsserverless.ListNamespacesOutput
		err := c.recordAPICall(ctx, "ListNamespaces", func(callCtx context.Context) error {
			var err error
			page, err = c.serverless.ListNamespaces(callCtx, &awsserverless.ListNamespacesInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return namespaces, nil
		}
		for _, raw := range page.Namespaces {
			tags, err := c.serverlessTags(ctx, aws.ToString(raw.NamespaceArn))
			if err != nil {
				return nil, err
			}
			namespaces = append(namespaces, mapServerlessNamespace(raw, tags))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return namespaces, nil
		}
	}
}

// ListServerlessWorkgroups returns Redshift Serverless workgroup metadata
// visible to the configured AWS credentials.
func (c *Client) ListServerlessWorkgroups(ctx context.Context) ([]redshiftservice.ServerlessWorkgroup, error) {
	var workgroups []redshiftservice.ServerlessWorkgroup
	var nextToken *string
	for {
		var page *awsserverless.ListWorkgroupsOutput
		err := c.recordAPICall(ctx, "ListWorkgroups", func(callCtx context.Context) error {
			var err error
			page, err = c.serverless.ListWorkgroups(callCtx, &awsserverless.ListWorkgroupsInput{
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return workgroups, nil
		}
		for _, raw := range page.Workgroups {
			tags, err := c.serverlessTags(ctx, aws.ToString(raw.WorkgroupArn))
			if err != nil {
				return nil, err
			}
			workgroups = append(workgroups, mapServerlessWorkgroup(raw, tags))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return workgroups, nil
		}
	}
}

func (c *Client) serverlessTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsserverless.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.serverless.ListTagsForResource(callCtx, &awsserverless.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return serverlessTagsMap(output.Tags), nil
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

var _ redshiftservice.Client = (*Client)(nil)

var _ provisionedAPI = (*awsredshift.Client)(nil)

var _ serverlessAPI = (*awsserverless.Client)(nil)
