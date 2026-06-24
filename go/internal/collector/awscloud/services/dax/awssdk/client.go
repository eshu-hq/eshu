// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdax "github.com/aws/aws-sdk-go-v2/service/dax"
	awsdaxtypes "github.com/aws/aws-sdk-go-v2/service/dax/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	daxservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/dax"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// describeMaxResults caps each DAX describe page. DAX requires MaxResults to be
// between 20 and 100; 100 minimizes round trips for accounts with many clusters.
const describeMaxResults int32 = 100

// apiClient is the read-only slice of the AWS DAX SDK the adapter depends on. It
// intentionally lists only Describe and ListTags operations so the adapter
// cannot reach a Create/Delete/Update/Increase/Decrease/Reboot/Tag/Untag
// mutation API or DescribeParameters (which would expose individual parameter
// values). The exclusion test in client_test.go guards this surface.
type apiClient interface {
	DescribeClusters(
		context.Context,
		*awsdax.DescribeClustersInput,
		...func(*awsdax.Options),
	) (*awsdax.DescribeClustersOutput, error)
	DescribeSubnetGroups(
		context.Context,
		*awsdax.DescribeSubnetGroupsInput,
		...func(*awsdax.Options),
	) (*awsdax.DescribeSubnetGroupsOutput, error)
	DescribeParameterGroups(
		context.Context,
		*awsdax.DescribeParameterGroupsInput,
		...func(*awsdax.Options),
	) (*awsdax.DescribeParameterGroupsOutput, error)
	ListTags(
		context.Context,
		*awsdax.ListTagsInput,
		...func(*awsdax.Options),
	) (*awsdax.ListTagsOutput, error)
}

// Client adapts AWS SDK DAX control-plane calls into scanner-owned metadata. It
// never reads cached DynamoDB items, query results, node endpoint payloads,
// individual parameter values, or any mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a DAX SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsdax.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListClusters returns DAX cluster metadata visible to the configured AWS
// credentials, including resource tags read per cluster ARN.
func (c *Client) ListClusters(ctx context.Context) ([]daxservice.Cluster, error) {
	var clusters []daxservice.Cluster
	var token *string
	for {
		var page *awsdax.DescribeClustersOutput
		err := c.recordAPICall(ctx, "DescribeClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeClusters(callCtx, &awsdax.DescribeClustersInput{
				NextToken:  token,
				MaxResults: aws.Int32(describeMaxResults),
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
			tags, err := c.listTags(ctx, aws.ToString(raw.ClusterArn))
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

// ListSubnetGroups returns DAX subnet group metadata visible to the configured
// AWS credentials. DAX subnet groups have no ARN, so no tags are read for them.
func (c *Client) ListSubnetGroups(ctx context.Context) ([]daxservice.SubnetGroup, error) {
	var groups []daxservice.SubnetGroup
	var token *string
	for {
		var page *awsdax.DescribeSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeSubnetGroups(callCtx, &awsdax.DescribeSubnetGroupsInput{
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
			groups = append(groups, mapSubnetGroup(raw))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return groups, nil
		}
	}
}

// ListParameterGroups returns DAX parameter group metadata visible to the
// configured AWS credentials. The adapter does not call DescribeParameters so
// individual parameter values are never persisted.
func (c *Client) ListParameterGroups(ctx context.Context) ([]daxservice.ParameterGroup, error) {
	var groups []daxservice.ParameterGroup
	var token *string
	for {
		var page *awsdax.DescribeParameterGroupsOutput
		err := c.recordAPICall(ctx, "DescribeParameterGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeParameterGroups(callCtx, &awsdax.DescribeParameterGroupsInput{
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
			groups = append(groups, mapParameterGroup(raw))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return groups, nil
		}
	}
}

// listTags returns the DAX resource tags for a cluster ARN, paging the ListTags
// response to exhaustion. An empty ARN reads nothing.
func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var tags []awsdaxtypes.Tag
	var token *string
	for {
		var output *awsdax.ListTagsOutput
		err := c.recordAPICall(ctx, "ListTags", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListTags(callCtx, &awsdax.ListTagsInput{
				ResourceName: aws.String(resourceARN),
				NextToken:    token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			break
		}
		tags = append(tags, output.Tags...)
		token = output.NextToken
		if aws.ToString(token) == "" {
			break
		}
	}
	return mapTags(tags), nil
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

var _ daxservice.Client = (*Client)(nil)

var _ apiClient = (*awsdax.Client)(nil)
