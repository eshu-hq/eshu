// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsneptune "github.com/aws/aws-sdk-go-v2/service/neptune"
	awsneptunegraph "github.com/aws/aws-sdk-go-v2/service/neptunegraph"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	neptuneservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/neptune"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeMaxRecords int32 = 100

// neptuneAPI is the metadata-only subset of the Amazon Neptune
// (provisioned) control-plane the adapter consumes. It excludes every mutation,
// restore, failover, and reboot operation by construction.
type neptuneAPI interface {
	DescribeDBClusters(
		context.Context,
		*awsneptune.DescribeDBClustersInput,
		...func(*awsneptune.Options),
	) (*awsneptune.DescribeDBClustersOutput, error)
	DescribeDBInstances(
		context.Context,
		*awsneptune.DescribeDBInstancesInput,
		...func(*awsneptune.Options),
	) (*awsneptune.DescribeDBInstancesOutput, error)
	DescribeDBClusterParameterGroups(
		context.Context,
		*awsneptune.DescribeDBClusterParameterGroupsInput,
		...func(*awsneptune.Options),
	) (*awsneptune.DescribeDBClusterParameterGroupsOutput, error)
	DescribeDBClusterSnapshots(
		context.Context,
		*awsneptune.DescribeDBClusterSnapshotsInput,
		...func(*awsneptune.Options),
	) (*awsneptune.DescribeDBClusterSnapshotsOutput, error)
	DescribeDBSubnetGroups(
		context.Context,
		*awsneptune.DescribeDBSubnetGroupsInput,
		...func(*awsneptune.Options),
	) (*awsneptune.DescribeDBSubnetGroupsOutput, error)
	DescribeGlobalClusters(
		context.Context,
		*awsneptune.DescribeGlobalClustersInput,
		...func(*awsneptune.Options),
	) (*awsneptune.DescribeGlobalClustersOutput, error)
	ListTagsForResource(
		context.Context,
		*awsneptune.ListTagsForResourceInput,
		...func(*awsneptune.Options),
	) (*awsneptune.ListTagsForResourceOutput, error)
}

// neptuneGraphAPI is the metadata-only subset of the Neptune Analytics
// control-plane the adapter consumes. It excludes every graph data-plane call
// (ExecuteQuery, CancelQuery, GetQuery, ListQueries) and every mutation
// (CreateGraph, DeleteGraph, ResetGraph, UpdateGraph, RestoreGraphFromSnapshot,
// CreateGraphSnapshot, import/export tasks) by construction.
type neptuneGraphAPI interface {
	ListGraphs(
		context.Context,
		*awsneptunegraph.ListGraphsInput,
		...func(*awsneptunegraph.Options),
	) (*awsneptunegraph.ListGraphsOutput, error)
	GetGraph(
		context.Context,
		*awsneptunegraph.GetGraphInput,
		...func(*awsneptunegraph.Options),
	) (*awsneptunegraph.GetGraphOutput, error)
	ListGraphSnapshots(
		context.Context,
		*awsneptunegraph.ListGraphSnapshotsInput,
		...func(*awsneptunegraph.Options),
	) (*awsneptunegraph.ListGraphSnapshotsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsneptunegraph.ListTagsForResourceInput,
		...func(*awsneptunegraph.Options),
	) (*awsneptunegraph.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Neptune and Neptune Analytics control-plane calls into
// scanner-owned metadata. It never connects to a database or graph endpoint,
// runs a graph query, reads graph vertex or edge contents, reads snapshot
// contents, persists master user passwords or secrets, reads cluster parameter
// values, or calls mutation APIs.
type Client struct {
	neptune     neptuneAPI
	graph       neptuneGraphAPI
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Neptune SDK adapter for one claimed AWS boundary. It
// constructs both the Neptune (provisioned) and Neptune Analytics SDK clients
// from the shared config.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		neptune:     awsneptune.NewFromConfig(config),
		graph:       awsneptunegraph.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDBClusters returns Neptune DB cluster metadata visible to the configured
// AWS credentials.
func (c *Client) ListDBClusters(ctx context.Context) ([]neptuneservice.DBCluster, error) {
	var clusters []neptuneservice.DBCluster
	var marker *string
	for {
		var page *awsneptune.DescribeDBClustersOutput
		err := c.recordAPICall(ctx, "DescribeDBClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.neptune.DescribeDBClusters(callCtx, &awsneptune.DescribeDBClustersInput{
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
			tags, err := c.listNeptuneTags(ctx, aws.ToString(raw.DBClusterArn))
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

// ListClusterInstances returns Neptune cluster instance metadata visible to the
// configured AWS credentials.
func (c *Client) ListClusterInstances(ctx context.Context) ([]neptuneservice.ClusterInstance, error) {
	var instances []neptuneservice.ClusterInstance
	var marker *string
	for {
		var page *awsneptune.DescribeDBInstancesOutput
		err := c.recordAPICall(ctx, "DescribeDBInstances", func(callCtx context.Context) error {
			var err error
			page, err = c.neptune.DescribeDBInstances(callCtx, &awsneptune.DescribeDBInstancesInput{
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
			tags, err := c.listNeptuneTags(ctx, aws.ToString(raw.DBInstanceArn))
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

// ListClusterParameterGroups returns Neptune cluster parameter group metadata.
// Only the group name and family are returned; parameter values are never read
// or persisted.
func (c *Client) ListClusterParameterGroups(ctx context.Context) ([]neptuneservice.ClusterParameterGroup, error) {
	var groups []neptuneservice.ClusterParameterGroup
	var marker *string
	for {
		var page *awsneptune.DescribeDBClusterParameterGroupsOutput
		err := c.recordAPICall(ctx, "DescribeDBClusterParameterGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.neptune.DescribeDBClusterParameterGroups(callCtx, &awsneptune.DescribeDBClusterParameterGroupsInput{
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
			tags, err := c.listNeptuneTags(ctx, aws.ToString(raw.DBClusterParameterGroupArn))
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapClusterParameterGroup(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

// ListClusterSnapshots returns Neptune cluster snapshot metadata. Snapshot
// contents are never read.
func (c *Client) ListClusterSnapshots(ctx context.Context) ([]neptuneservice.ClusterSnapshot, error) {
	var snapshots []neptuneservice.ClusterSnapshot
	var marker *string
	for {
		var page *awsneptune.DescribeDBClusterSnapshotsOutput
		err := c.recordAPICall(ctx, "DescribeDBClusterSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = c.neptune.DescribeDBClusterSnapshots(callCtx, &awsneptune.DescribeDBClusterSnapshotsInput{
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
			tags, err := c.listNeptuneTags(ctx, aws.ToString(raw.DBClusterSnapshotArn))
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

// ListSubnetGroups returns Neptune DB subnet group metadata visible to the
// configured AWS credentials.
func (c *Client) ListSubnetGroups(ctx context.Context) ([]neptuneservice.SubnetGroup, error) {
	var subnetGroups []neptuneservice.SubnetGroup
	var marker *string
	for {
		var page *awsneptune.DescribeDBSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeDBSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.neptune.DescribeDBSubnetGroups(callCtx, &awsneptune.DescribeDBSubnetGroupsInput{
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
			tags, err := c.listNeptuneTags(ctx, aws.ToString(raw.DBSubnetGroupArn))
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

// ListGlobalClusters returns Neptune global cluster metadata visible to the
// configured AWS credentials.
func (c *Client) ListGlobalClusters(ctx context.Context) ([]neptuneservice.GlobalCluster, error) {
	var globalClusters []neptuneservice.GlobalCluster
	var marker *string
	for {
		var page *awsneptune.DescribeGlobalClustersOutput
		err := c.recordAPICall(ctx, "DescribeGlobalClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.neptune.DescribeGlobalClusters(callCtx, &awsneptune.DescribeGlobalClustersInput{
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

func (c *Client) listNeptuneTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsneptune.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.neptune.ListTagsForResource(callCtx, &awsneptune.ListTagsForResourceInput{
			ResourceName: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return mapNeptuneTags(output.TagList), nil
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

var _ neptuneservice.Client = (*Client)(nil)

var (
	_ neptuneAPI      = (*awsneptune.Client)(nil)
	_ neptuneGraphAPI = (*awsneptunegraph.Client)(nil)
)
