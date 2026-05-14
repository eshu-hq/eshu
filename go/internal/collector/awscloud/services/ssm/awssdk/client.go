package awssdk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	awsssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	ssmservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/ssm"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeParametersLimit int32 = 50

type apiClient interface {
	DescribeParameters(
		context.Context,
		*awsssm.DescribeParametersInput,
		...func(*awsssm.Options),
	) (*awsssm.DescribeParametersOutput, error)
	ListTagsForResource(
		context.Context,
		*awsssm.ListTagsForResourceInput,
		...func(*awsssm.Options),
	) (*awsssm.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK SSM control-plane calls into scanner-owned metadata. It
// never calls GetParameter, GetParameters, GetParametersByPath,
// GetParameterHistory, decryption, or mutation APIs.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an SSM SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsssm.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListParameters returns Parameter Store metadata visible to the configured
// AWS credentials.
func (c *Client) ListParameters(ctx context.Context) ([]ssmservice.Parameter, error) {
	var parameters []ssmservice.Parameter
	var nextToken *string
	for {
		var page *awsssm.DescribeParametersOutput
		err := c.recordAPICall(ctx, "DescribeParameters", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeParameters(callCtx, &awsssm.DescribeParametersInput{
				MaxResults: aws.Int32(describeParametersLimit),
				NextToken:  nextToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return parameters, nil
		}
		for _, raw := range page.Parameters {
			parameter := mapParameter(raw)
			tags, err := c.listTags(ctx, parameter.Name)
			if err != nil {
				return nil, err
			}
			parameter.Tags = tags
			parameters = append(parameters, parameter)
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return parameters, nil
		}
	}
}

func (c *Client) listTags(ctx context.Context, resourceID string) (map[string]string, error) {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return nil, nil
	}
	var output *awsssm.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsssm.ListTagsForResourceInput{
			ResourceId:   aws.String(resourceID),
			ResourceType: awsssmtypes.ResourceTypeForTaggingParameter,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list tags for SSM parameter %q: %w", resourceID, err)
	}
	if output == nil {
		return nil, nil
	}
	return tags(output.TagList), nil
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

var _ ssmservice.Client = (*Client)(nil)

var _ apiClient = (*awsssm.Client)(nil)
