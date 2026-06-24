// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmemorydb "github.com/aws/aws-sdk-go-v2/service/memorydb"
	awsmemorydbtypes "github.com/aws/aws-sdk-go-v2/service/memorydb/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	memorydbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/memorydb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeMaxResults int32 = 100

// apiClient is the read-only slice of the AWS MemoryDB SDK the adapter depends
// on. It intentionally lists only Describe and ListTags operations so the
// adapter cannot reach a Create/Delete/Update/Modify/Reset/Failover/Tag/Untag
// mutation API. The exclusion test in client_test.go guards this surface.
type apiClient interface {
	DescribeClusters(
		context.Context,
		*awsmemorydb.DescribeClustersInput,
		...func(*awsmemorydb.Options),
	) (*awsmemorydb.DescribeClustersOutput, error)
	DescribeSubnetGroups(
		context.Context,
		*awsmemorydb.DescribeSubnetGroupsInput,
		...func(*awsmemorydb.Options),
	) (*awsmemorydb.DescribeSubnetGroupsOutput, error)
	DescribeParameterGroups(
		context.Context,
		*awsmemorydb.DescribeParameterGroupsInput,
		...func(*awsmemorydb.Options),
	) (*awsmemorydb.DescribeParameterGroupsOutput, error)
	DescribeUsers(
		context.Context,
		*awsmemorydb.DescribeUsersInput,
		...func(*awsmemorydb.Options),
	) (*awsmemorydb.DescribeUsersOutput, error)
	DescribeACLs(
		context.Context,
		*awsmemorydb.DescribeACLsInput,
		...func(*awsmemorydb.Options),
	) (*awsmemorydb.DescribeACLsOutput, error)
	DescribeSnapshots(
		context.Context,
		*awsmemorydb.DescribeSnapshotsInput,
		...func(*awsmemorydb.Options),
	) (*awsmemorydb.DescribeSnapshotsOutput, error)
	ListTags(
		context.Context,
		*awsmemorydb.ListTagsInput,
		...func(*awsmemorydb.Options),
	) (*awsmemorydb.ListTagsOutput, error)
}

// Client adapts AWS SDK MemoryDB control-plane calls into scanner-owned
// metadata. It never reads cache contents, AUTH token values, user passwords,
// the raw user access string, snapshot node payloads, or any mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a MemoryDB SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsmemorydb.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListClusters returns MemoryDB cluster metadata visible to the configured AWS
// credentials. ShowShardDetails is requested so the adapter can derive the
// per-shard replica count from the shard node counts; MemoryDB's Cluster
// response does not expose a replica count field directly.
func (c *Client) ListClusters(ctx context.Context) ([]memorydbservice.Cluster, error) {
	var clusters []memorydbservice.Cluster
	var token *string
	for {
		var page *awsmemorydb.DescribeClustersOutput
		err := c.recordAPICall(ctx, "DescribeClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeClusters(callCtx, &awsmemorydb.DescribeClustersInput{
				NextToken:        token,
				MaxResults:       aws.Int32(describeMaxResults),
				ShowShardDetails: aws.Bool(true),
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
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, mapCluster(raw, tags))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return clusters, nil
		}
	}
}

// ListSubnetGroups returns MemoryDB subnet group metadata visible to the
// configured AWS credentials.
func (c *Client) ListSubnetGroups(ctx context.Context) ([]memorydbservice.SubnetGroup, error) {
	var groups []memorydbservice.SubnetGroup
	var token *string
	for {
		var page *awsmemorydb.DescribeSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeSubnetGroups(callCtx, &awsmemorydb.DescribeSubnetGroupsInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxResults),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return groups, nil
		}
		for _, raw := range page.SubnetGroups {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapSubnetGroup(raw, tags))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return groups, nil
		}
	}
}

// ListParameterGroups returns MemoryDB parameter group metadata visible to the
// configured AWS credentials. The adapter does not call DescribeParameters so
// individual parameter values are never persisted.
func (c *Client) ListParameterGroups(ctx context.Context) ([]memorydbservice.ParameterGroup, error) {
	var groups []memorydbservice.ParameterGroup
	var token *string
	for {
		var page *awsmemorydb.DescribeParameterGroupsOutput
		err := c.recordAPICall(ctx, "DescribeParameterGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeParameterGroups(callCtx, &awsmemorydb.DescribeParameterGroupsInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxResults),
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
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			groups = append(groups, mapParameterGroup(raw, tags))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return groups, nil
		}
	}
}

// ListUsers returns MemoryDB user metadata visible to the configured AWS
// credentials. The adapter intentionally drops the AWS-returned AccessString
// (the raw ACL grant string) before scanner code sees it; only a non-secret
// presence signal survives. Password material is never exposed by the SDK
// describe response and is never persisted.
func (c *Client) ListUsers(ctx context.Context) ([]memorydbservice.User, error) {
	var users []memorydbservice.User
	var token *string
	for {
		var page *awsmemorydb.DescribeUsersOutput
		err := c.recordAPICall(ctx, "DescribeUsers", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeUsers(callCtx, &awsmemorydb.DescribeUsersInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxResults),
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
		token = page.NextToken
		if aws.ToString(token) == "" {
			return users, nil
		}
	}
}

// ListACLs returns MemoryDB Access Control List metadata visible to the
// configured AWS credentials.
func (c *Client) ListACLs(ctx context.Context) ([]memorydbservice.ACL, error) {
	var acls []memorydbservice.ACL
	var token *string
	for {
		var page *awsmemorydb.DescribeACLsOutput
		err := c.recordAPICall(ctx, "DescribeACLs", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeACLs(callCtx, &awsmemorydb.DescribeACLsInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxResults),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return acls, nil
		}
		for _, raw := range page.ACLs {
			arn := aws.ToString(raw.ARN)
			tags, err := c.listTags(ctx, arn)
			if err != nil {
				return nil, err
			}
			acls = append(acls, mapACL(raw, tags))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return acls, nil
		}
	}
}

// ListSnapshots returns MemoryDB snapshot metadata (name, source cluster,
// source, status) visible to the configured AWS credentials. Cluster
// configuration detail, shard sizes, engine version, and KMS key stay outside
// the adapter contract.
func (c *Client) ListSnapshots(ctx context.Context) ([]memorydbservice.SnapshotMetadata, error) {
	var snapshots []memorydbservice.SnapshotMetadata
	var token *string
	for {
		var page *awsmemorydb.DescribeSnapshotsOutput
		err := c.recordAPICall(ctx, "DescribeSnapshots", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeSnapshots(callCtx, &awsmemorydb.DescribeSnapshotsInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxResults),
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
		token = page.NextToken
		if aws.ToString(token) == "" {
			return snapshots, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsmemorydb.ListTagsOutput
	err := c.recordAPICall(ctx, "ListTags", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTags(callCtx, &awsmemorydb.ListTagsInput{
			ResourceArn: aws.String(resourceARN),
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

func mapTags(tags []awsmemorydbtypes.Tag) map[string]string {
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

var _ memorydbservice.Client = (*Client)(nil)

var _ apiClient = (*awsmemorydb.Client)(nil)
