// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdocdb "github.com/aws/aws-sdk-go-v2/service/docdb"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	docdbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/docdb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeMaxRecords int32 = 100

type apiClient interface {
	DescribeDBClusters(
		context.Context,
		*awsdocdb.DescribeDBClustersInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeDBClustersOutput, error)
	DescribeDBInstances(
		context.Context,
		*awsdocdb.DescribeDBInstancesInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeDBInstancesOutput, error)
	DescribeDBClusterParameterGroups(
		context.Context,
		*awsdocdb.DescribeDBClusterParameterGroupsInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeDBClusterParameterGroupsOutput, error)
	DescribeDBClusterParameters(
		context.Context,
		*awsdocdb.DescribeDBClusterParametersInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeDBClusterParametersOutput, error)
	DescribeDBClusterSnapshots(
		context.Context,
		*awsdocdb.DescribeDBClusterSnapshotsInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeDBClusterSnapshotsOutput, error)
	DescribeDBSubnetGroups(
		context.Context,
		*awsdocdb.DescribeDBSubnetGroupsInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeDBSubnetGroupsOutput, error)
	DescribeGlobalClusters(
		context.Context,
		*awsdocdb.DescribeGlobalClustersInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeGlobalClustersOutput, error)
	DescribeEventSubscriptions(
		context.Context,
		*awsdocdb.DescribeEventSubscriptionsInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.DescribeEventSubscriptionsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsdocdb.ListTagsForResourceInput,
		...func(*awsdocdb.Options),
	) (*awsdocdb.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK DocumentDB control-plane calls into scanner-owned
// metadata. It never connects to a database, reads documents or collections,
// reads snapshot contents, persists master user passwords or secrets, reads
// cluster parameter values, or calls mutation APIs.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DocumentDB SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdocdb.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDBClusters returns DocumentDB DB cluster metadata visible to the
// configured AWS credentials.
func (c *Client) ListDBClusters(ctx context.Context) ([]docdbservice.DBCluster, error) {
	var clusters []docdbservice.DBCluster
	var marker *string
	for {
		var page *awsdocdb.DescribeDBClustersOutput
		err := c.recordAPICall(ctx, "DescribeDBClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBClusters(callCtx, &awsdocdb.DescribeDBClustersInput{
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
		for _, raw := range page.DBClusters {
			tags, err := c.listTags(ctx, aws.ToString(raw.DBClusterArn))
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, mapDBCluster(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return clusters, nil
		}
	}
}

// ListClusterInstances returns DocumentDB cluster instance metadata visible to
// the configured AWS credentials.
func (c *Client) ListClusterInstances(ctx context.Context) ([]docdbservice.ClusterInstance, error) {
	var instances []docdbservice.ClusterInstance
	var marker *string
	for {
		var page *awsdocdb.DescribeDBInstancesOutput
		err := c.recordAPICall(ctx, "DescribeDBInstances", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBInstances(callCtx, &awsdocdb.DescribeDBInstancesInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return instances, nil
		}
		for _, raw := range page.DBInstances {
			tags, err := c.listTags(ctx, aws.ToString(raw.DBInstanceArn))
			if err != nil {
				return nil, err
			}
			instances = append(instances, mapClusterInstance(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return instances, nil
		}
	}
}

// ListClusterParameterGroups returns DocumentDB cluster parameter group
// metadata. Only the group name, family, and parameter count are returned;
// parameter values are counted but never persisted.
func (c *Client) ListClusterParameterGroups(ctx context.Context) ([]docdbservice.ClusterParameterGroup, error) {
	var groups []docdbservice.ClusterParameterGroup
	var marker *string
	for {
		var page *awsdocdb.DescribeDBClusterParameterGroupsOutput
		err := c.recordAPICall(ctx, "DescribeDBClusterParameterGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBClusterParameterGroups(callCtx, &awsdocdb.DescribeDBClusterParameterGroupsInput{
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
		for _, raw := range page.DBClusterParameterGroups {
			name := strings.TrimSpace(aws.ToString(raw.DBClusterParameterGroupName))
			count, err := c.countParameters(ctx, name)
			if err != nil {
				return nil, err
			}
			tags, err := c.listTags(ctx, aws.ToString(raw.DBClusterParameterGroupArn))
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapClusterParameterGroup(raw, count, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

// countParameters counts the parameters in a DocumentDB cluster parameter
// group without persisting any parameter name or value. The scanner contract
// records the count only.
func (c *Client) countParameters(ctx context.Context, groupName string) (int, error) {
	if groupName == "" {
		return 0, nil
	}
	var count int
	var marker *string
	for {
		var page *awsdocdb.DescribeDBClusterParametersOutput
		err := c.recordAPICall(ctx, "DescribeDBClusterParameters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBClusterParameters(callCtx, &awsdocdb.DescribeDBClusterParametersInput{
				DBClusterParameterGroupName: aws.String(groupName),
				Marker:                      marker,
				MaxRecords:                  aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return 0, err
		}
		if page == nil {
			return count, nil
		}
		count += len(page.Parameters)
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return count, nil
		}
	}
}

// ListClusterSnapshots returns DocumentDB cluster snapshot metadata. Snapshot
// contents are never read.
func (c *Client) ListClusterSnapshots(ctx context.Context) ([]docdbservice.ClusterSnapshot, error) {
	var snapshots []docdbservice.ClusterSnapshot
	var marker *string
	for {
		var page *awsdocdb.DescribeDBClusterSnapshotsOutput
		err := c.recordAPICall(ctx, "DescribeDBClusterSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBClusterSnapshots(callCtx, &awsdocdb.DescribeDBClusterSnapshotsInput{
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
		for _, raw := range page.DBClusterSnapshots {
			tags, err := c.listTags(ctx, aws.ToString(raw.DBClusterSnapshotArn))
			if err != nil {
				return nil, err
			}
			snapshots = append(snapshots, mapClusterSnapshot(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return snapshots, nil
		}
	}
}

// ListSubnetGroups returns DocumentDB DB subnet group metadata visible to the
// configured AWS credentials.
func (c *Client) ListSubnetGroups(ctx context.Context) ([]docdbservice.SubnetGroup, error) {
	var subnetGroups []docdbservice.SubnetGroup
	var marker *string
	for {
		var page *awsdocdb.DescribeDBSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeDBSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBSubnetGroups(callCtx, &awsdocdb.DescribeDBSubnetGroupsInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return subnetGroups, nil
		}
		for _, raw := range page.DBSubnetGroups {
			tags, err := c.listTags(ctx, aws.ToString(raw.DBSubnetGroupArn))
			if err != nil {
				return nil, err
			}
			subnetGroups = append(subnetGroups, mapSubnetGroup(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return subnetGroups, nil
		}
	}
}

// ListGlobalClusters returns DocumentDB global cluster metadata visible to the
// configured AWS credentials.
func (c *Client) ListGlobalClusters(ctx context.Context) ([]docdbservice.GlobalCluster, error) {
	var globalClusters []docdbservice.GlobalCluster
	var marker *string
	for {
		var page *awsdocdb.DescribeGlobalClustersOutput
		err := c.recordAPICall(ctx, "DescribeGlobalClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeGlobalClusters(callCtx, &awsdocdb.DescribeGlobalClustersInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return globalClusters, nil
		}
		for _, raw := range page.GlobalClusters {
			globalClusters = append(globalClusters, mapGlobalCluster(raw))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return globalClusters, nil
		}
	}
}

// ListEventSubscriptions returns DocumentDB event subscription metadata
// visible to the configured AWS credentials.
func (c *Client) ListEventSubscriptions(ctx context.Context) ([]docdbservice.EventSubscription, error) {
	var subscriptions []docdbservice.EventSubscription
	var marker *string
	for {
		var page *awsdocdb.DescribeEventSubscriptionsOutput
		err := c.recordAPICall(ctx, "DescribeEventSubscriptions", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeEventSubscriptions(callCtx, &awsdocdb.DescribeEventSubscriptionsInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return subscriptions, nil
		}
		for _, raw := range page.EventSubscriptionsList {
			tags, err := c.listTags(ctx, aws.ToString(raw.EventSubscriptionArn))
			if err != nil {
				return nil, err
			}
			subscriptions = append(subscriptions, mapEventSubscription(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return subscriptions, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsdocdb.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsdocdb.ListTagsForResourceInput{
			ResourceName: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return mapTags(output.TagList), nil
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

var _ docdbservice.Client = (*Client)(nil)

var _ apiClient = (*awsdocdb.Client)(nil)
