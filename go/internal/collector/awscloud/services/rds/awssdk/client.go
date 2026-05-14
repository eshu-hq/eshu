package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	rdsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/rds"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeMaxRecords int32 = 100

type apiClient interface {
	DescribeDBInstances(
		context.Context,
		*awsrds.DescribeDBInstancesInput,
		...func(*awsrds.Options),
	) (*awsrds.DescribeDBInstancesOutput, error)
	DescribeDBClusters(
		context.Context,
		*awsrds.DescribeDBClustersInput,
		...func(*awsrds.Options),
	) (*awsrds.DescribeDBClustersOutput, error)
	DescribeDBSubnetGroups(
		context.Context,
		*awsrds.DescribeDBSubnetGroupsInput,
		...func(*awsrds.Options),
	) (*awsrds.DescribeDBSubnetGroupsOutput, error)
	ListTagsForResource(
		context.Context,
		*awsrds.ListTagsForResourceInput,
		...func(*awsrds.Options),
	) (*awsrds.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK RDS control-plane calls into scanner-owned metadata. It
// never connects to databases, reads snapshots or log contents, fetches
// Performance Insights samples, reads schemas or tables, or calls mutation APIs.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an RDS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsrds.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDBInstances returns RDS DB instance metadata visible to the configured
// AWS credentials.
func (c *Client) ListDBInstances(ctx context.Context) ([]rdsservice.DBInstance, error) {
	var instances []rdsservice.DBInstance
	var marker *string
	for {
		var page *awsrds.DescribeDBInstancesOutput
		err := c.recordAPICall(ctx, "DescribeDBInstances", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBInstances(callCtx, &awsrds.DescribeDBInstancesInput{
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
			instances = append(instances, mapDBInstance(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return instances, nil
		}
	}
}

// ListDBClusters returns RDS DB cluster metadata visible to the configured AWS
// credentials.
func (c *Client) ListDBClusters(ctx context.Context) ([]rdsservice.DBCluster, error) {
	var clusters []rdsservice.DBCluster
	var marker *string
	for {
		var page *awsrds.DescribeDBClustersOutput
		err := c.recordAPICall(ctx, "DescribeDBClusters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBClusters(callCtx, &awsrds.DescribeDBClustersInput{
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

// ListDBSubnetGroups returns RDS DB subnet group metadata visible to the
// configured AWS credentials.
func (c *Client) ListDBSubnetGroups(ctx context.Context) ([]rdsservice.DBSubnetGroup, error) {
	var subnetGroups []rdsservice.DBSubnetGroup
	var marker *string
	for {
		var page *awsrds.DescribeDBSubnetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeDBSubnetGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeDBSubnetGroups(callCtx, &awsrds.DescribeDBSubnetGroupsInput{
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
			subnetGroups = append(subnetGroups, mapDBSubnetGroup(raw, tags))
		}
		marker = page.Marker
		if aws.ToString(marker) == "" {
			return subnetGroups, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awsrds.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsrds.ListTagsForResourceInput{
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

var _ rdsservice.Client = (*Client)(nil)

var _ apiClient = (*awsrds.Client)(nil)
