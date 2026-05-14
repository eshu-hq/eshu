package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudwatchlogs "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cloudwatchlogsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudwatchlogs"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeLogGroupsLimit int32 = 50

type apiClient interface {
	DescribeLogGroups(
		context.Context,
		*awscloudwatchlogs.DescribeLogGroupsInput,
		...func(*awscloudwatchlogs.Options),
	) (*awscloudwatchlogs.DescribeLogGroupsOutput, error)
	ListTagsForResource(
		context.Context,
		*awscloudwatchlogs.ListTagsForResourceInput,
		...func(*awscloudwatchlogs.Options),
	) (*awscloudwatchlogs.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK CloudWatch Logs control-plane calls into scanner-owned
// metadata. It never reads log events, log stream payloads, Insights query
// results, export payloads, resource policies, subscription payloads, or calls
// mutation APIs.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CloudWatch Logs SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscloudwatchlogs.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListLogGroups returns CloudWatch Logs log group metadata visible to the
// configured AWS credentials.
func (c *Client) ListLogGroups(ctx context.Context) ([]cloudwatchlogsservice.LogGroup, error) {
	var logGroups []cloudwatchlogsservice.LogGroup
	var nextToken *string
	for {
		var page *awscloudwatchlogs.DescribeLogGroupsOutput
		err := c.recordAPICall(ctx, "DescribeLogGroups", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeLogGroups(callCtx, &awscloudwatchlogs.DescribeLogGroupsInput{
				Limit:     aws.Int32(describeLogGroupsLimit),
				NextToken: nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return logGroups, nil
		}
		for _, raw := range page.LogGroups {
			tags, err := c.listTags(ctx, tagResourceARN(raw))
			if err != nil {
				return nil, err
			}
			logGroups = append(logGroups, mapLogGroup(raw, tags))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return logGroups, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awscloudwatchlogs.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awscloudwatchlogs.ListTagsForResourceInput{
			ResourceArn: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil {
		return nil, err
	}
	return cloneTags(output.Tags), nil
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

var _ cloudwatchlogsservice.Client = (*Client)(nil)

var _ apiClient = (*awscloudwatchlogs.Client)(nil)
