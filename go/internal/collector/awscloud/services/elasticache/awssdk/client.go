// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk //nolint:filelength // 527 lines: ElastiCache SDK pagination, KMS + subnet resolution caching, and per-call telemetry. The per-service awssdk client intentionally owns the full surface so scanner.go can stay a thin fact selector.

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselasticache "github.com/aws/aws-sdk-go-v2/service/elasticache"
	awselasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	elasticacheservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elasticache"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeMaxRecords int32 = 100

type apiClient interface {
	DescribeCacheClusters(
		context.Context,
		*awselasticache.DescribeCacheClustersInput,
		...func(*awselasticache.Options),
	) (*awselasticache.DescribeCacheClustersOutput, error)
	DescribeReplicationGroups(
		context.Context,
		*awselasticache.DescribeReplicationGroupsInput,
		...func(*awselasticache.Options),
	) (*awselasticache.DescribeReplicationGroupsOutput, error)
	DescribeCacheSubnetGroups(
		context.Context,
		*awselasticache.DescribeCacheSubnetGroupsInput,
		...func(*awselasticache.Options),
	) (*awselasticache.DescribeCacheSubnetGroupsOutput, error)
	DescribeCacheParameterGroups(
		context.Context,
		*awselasticache.DescribeCacheParameterGroupsInput,
		...func(*awselasticache.Options),
	) (*awselasticache.DescribeCacheParameterGroupsOutput, error)
	DescribeUsers(
		context.Context,
		*awselasticache.DescribeUsersInput,
		...func(*awselasticache.Options),
	) (*awselasticache.DescribeUsersOutput, error)
	DescribeUserGroups(
		context.Context,
		*awselasticache.DescribeUserGroupsInput,
		...func(*awselasticache.Options),
	) (*awselasticache.DescribeUserGroupsOutput, error)
	DescribeSnapshots(
		context.Context,
		*awselasticache.DescribeSnapshotsInput,
		...func(*awselasticache.Options),
	) (*awselasticache.DescribeSnapshotsOutput, error)
	ListTagsForResource(
		context.Context,
		*awselasticache.ListTagsForResourceInput,
		...func(*awselasticache.Options),
	) (*awselasticache.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK ElastiCache control-plane calls into scanner-owned
// metadata. It never reads cache contents, AUTH token values, user passwords,
// user access strings, snapshot node payloads, or any mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments

	replicationGroupsOnce  sync.Once
	replicationGroupsCache []elasticacheservice.ReplicationGroup
	replicationGroupsErr   error

	subnetGroupsOnce  sync.Once
	subnetGroupsCache []elasticacheservice.SubnetGroup
	subnetGroupsErr   error
}

// NewClient builds an ElastiCache SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awselasticache.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListCacheClusters returns ElastiCache cache cluster metadata visible to the
// configured AWS credentials. The adapter resolves the cluster's VPC, subnet
// IDs, and KMS key by joining against the cache subnet group and the cluster's
// replication group; AWS's CacheCluster response does not include those fields
// directly.
func (c *Client) ListCacheClusters(ctx context.Context) ([]elasticacheservice.CacheCluster, error) {
	subnetGroups, err := c.listSubnetGroupsForResolution(ctx)
	if err != nil {
		return nil, err
	}
	replicationGroupKMS, err := c.listReplicationGroupKMSForResolution(ctx)
	if err != nil {
		return nil, err
	}
	var clusters []elasticacheservice.CacheCluster
	var marker *string
	for {
		var page *awselasticache.DescribeCacheClustersOutput
		err := c.recordAPICall(ctx, "DescribeCacheClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeCacheClusters(callCtx, &awselasticache.DescribeCacheClustersInput{
				Marker:            marker,
				MaxRecords:        aws.Int32(describeMaxRecords),
				ShowCacheNodeInfo: aws.Bool(false),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return clusters, nil
		}
		for _, raw := range page.CacheClusters {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, mapCacheCluster(raw, tags, subnetGroups, replicationGroupKMS))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return clusters, nil
		}
	}
}

// listReplicationGroupKMSForResolution fetches the at-rest encryption KMS key
// id reported by each replication group so cache cluster facts can record
// their KMS dependency. AWS's CacheCluster response does not expose KmsKeyId
// directly; the replication group is the source of truth for managed Redis or
// Valkey clusters. The lookup reuses the per-client cache primed by
// ListReplicationGroups.
func (c *Client) listReplicationGroupKMSForResolution(ctx context.Context) (map[string]string, error) {
	groups, err := c.ListReplicationGroups(ctx)
	if err != nil {
		return nil, err
	}
	index := make(map[string]string, len(groups))
	for _, group := range groups {
		id := strings.TrimSpace(group.ID)
		if id == "" {
			continue
		}
		index[id] = strings.TrimSpace(group.KMSKeyID)
	}
	return index, nil
}

// ListReplicationGroups returns ElastiCache replication group metadata visible
// to the configured AWS credentials. The result is cached on the per-claim
// client so cluster KMS resolution and the scanner's replication group pass
// share a single AWS pagination cycle.
func (c *Client) ListReplicationGroups(ctx context.Context) ([]elasticacheservice.ReplicationGroup, error) {
	c.replicationGroupsOnce.Do(func() {
		c.replicationGroupsCache, c.replicationGroupsErr = c.fetchReplicationGroups(ctx)
	})
	if c.replicationGroupsErr != nil {
		return nil, c.replicationGroupsErr
	}
	return c.replicationGroupsCache, nil
}

func (c *Client) fetchReplicationGroups(ctx context.Context) ([]elasticacheservice.ReplicationGroup, error) {
	var groups []elasticacheservice.ReplicationGroup
	var marker *string
	for {
		var page *awselasticache.DescribeReplicationGroupsOutput
		err := c.recordAPICall(ctx, "DescribeReplicationGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeReplicationGroups(callCtx, &awselasticache.DescribeReplicationGroupsInput{
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
		for _, raw := range page.ReplicationGroups {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapReplicationGroup(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

// ListCacheSubnetGroups returns ElastiCache cache subnet group metadata
// visible to the configured AWS credentials. The result is cached on the
// per-claim client so cluster VPC/subnet resolution and the scanner's
// subnet group pass share a single AWS pagination cycle.
func (c *Client) ListCacheSubnetGroups(ctx context.Context) ([]elasticacheservice.SubnetGroup, error) {
	c.subnetGroupsOnce.Do(func() {
		c.subnetGroupsCache, c.subnetGroupsErr = c.fetchSubnetGroups(ctx)
	})
	if c.subnetGroupsErr != nil {
		return nil, c.subnetGroupsErr
	}
	return c.subnetGroupsCache, nil
}

// ListCacheParameterGroups returns ElastiCache parameter group metadata
// visible to the configured AWS credentials. The adapter does not call
// DescribeCacheParameters so individual parameter values are never persisted.
func (c *Client) ListCacheParameterGroups(ctx context.Context) ([]elasticacheservice.ParameterGroup, error) {
	var groups []elasticacheservice.ParameterGroup
	var marker *string
	for {
		var page *awselasticache.DescribeCacheParameterGroupsOutput
		err := c.recordAPICall(ctx, "DescribeCacheParameterGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeCacheParameterGroups(callCtx, &awselasticache.DescribeCacheParameterGroupsInput{
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
		for _, raw := range page.CacheParameterGroups {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapParameterGroup(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

// ListUsers returns ElastiCache user metadata visible to the configured AWS
// credentials. The adapter intentionally drops the AWS-returned Passwords and
// AccessString fields before scanner code sees them so password material and
// ACL grant strings can never reach facts or logs.
func (c *Client) ListUsers(ctx context.Context) ([]elasticacheservice.User, error) {
	var users []elasticacheservice.User
	var marker *string
	for {
		var page *awselasticache.DescribeUsersOutput
		err := c.recordAPICall(ctx, "DescribeUsers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeUsers(callCtx, &awselasticache.DescribeUsersInput{
				Marker:     marker,
				MaxRecords: aws.Int32(describeMaxRecords),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return users, nil
		}
		for _, raw := range page.Users {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			users = append(users, mapUser(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return users, nil
		}
	}
}

// ListUserGroups returns ElastiCache user group metadata visible to the
// configured AWS credentials.
func (c *Client) ListUserGroups(ctx context.Context) ([]elasticacheservice.UserGroup, error) {
	var groups []elasticacheservice.UserGroup
	var marker *string
	for {
		var page *awselasticache.DescribeUserGroupsOutput
		err := c.recordAPICall(ctx, "DescribeUserGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeUserGroups(callCtx, &awselasticache.DescribeUserGroupsInput{
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
		for _, raw := range page.UserGroups {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapUserGroup(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

// ListSnapshots returns ElastiCache snapshot metadata (name, source, status)
// visible to the configured AWS credentials. Node-snapshot detail, engine
// version, KMS key, and AUTH token state stay outside the adapter contract.
func (c *Client) ListSnapshots(ctx context.Context) ([]elasticacheservice.SnapshotMetadata, error) {
	var snapshots []elasticacheservice.SnapshotMetadata
	var marker *string
	for {
		var page *awselasticache.DescribeSnapshotsOutput
		err := c.recordAPICall(ctx, "DescribeSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeSnapshots(callCtx, &awselasticache.DescribeSnapshotsInput{
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
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			snapshots = append(snapshots, mapSnapshot(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return snapshots, nil
		}
	}
}

// listSubnetGroupsForResolution returns all subnet groups so ListCacheClusters
// can join cluster.CacheSubnetGroupName to the group's VPC and subnet IDs in
// the same scan window. ElastiCache CacheCluster responses do not include
// vpc_id or subnet_ids directly; the subnet group is the only safe source.
// The lookup reuses the per-client cache primed by ListCacheSubnetGroups.
func (c *Client) listSubnetGroupsForResolution(ctx context.Context) (map[string]elasticacheservice.SubnetGroup, error) {
	groups, err := c.ListCacheSubnetGroups(ctx)
	if err != nil {
		return nil, err
	}
	index := make(map[string]elasticacheservice.SubnetGroup, len(groups))
	for _, group := range groups {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			continue
		}
		index[name] = group
	}
	return index, nil
}

func (c *Client) fetchSubnetGroups(ctx context.Context) ([]elasticacheservice.SubnetGroup, error) {
	var groups []elasticacheservice.SubnetGroup
	var marker *string
	for {
		var page *awselasticache.DescribeCacheSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeCacheSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeCacheSubnetGroups(callCtx, &awselasticache.DescribeCacheSubnetGroupsInput{
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
		for _, raw := range page.CacheSubnetGroups {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapSubnetGroup(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return groups, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awselasticache.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awselasticache.ListTagsForResourceInput{
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

func mapTags(tags []awselasticachetypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

var _ elasticacheservice.Client = (*Client)(nil)

var _ apiClient = (*awselasticache.Client)(nil)
