package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudfront "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	awscloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	cloudfrontservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudfront"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const listDistributionsLimit int32 = 100

type apiClient interface {
	ListDistributions(
		context.Context,
		*awscloudfront.ListDistributionsInput,
		...func(*awscloudfront.Options),
	) (*awscloudfront.ListDistributionsOutput, error)
	ListTagsForResource(
		context.Context,
		*awscloudfront.ListTagsForResourceInput,
		...func(*awscloudfront.Options),
	) (*awscloudfront.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK CloudFront control-plane calls into scanner-owned
// metadata. It never reads objects, policy documents, certificate bodies, or
// calls CloudFront mutation APIs.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a CloudFront SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awscloudfront.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListDistributions returns CloudFront distribution metadata visible to the
// configured AWS credentials.
func (c *Client) ListDistributions(ctx context.Context) ([]cloudfrontservice.Distribution, error) {
	var distributions []cloudfrontservice.Distribution
	var marker *string
	for {
		var page *awscloudfront.ListDistributionsOutput
		err := c.recordAPICall(ctx, "ListDistributions", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListDistributions(callCtx, &awscloudfront.ListDistributionsInput{
				Marker:   marker,
				MaxItems: aws.Int32(listDistributionsLimit),
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil || page.DistributionList == nil {
			return distributions, nil
		}
		for _, distribution := range page.DistributionList.Items {
			mapped, err := c.distributionMetadata(ctx, distribution)
			if err != nil {
				return nil, err
			}
			distributions = append(distributions, mapped)
		}
		marker = page.DistributionList.NextMarker
		if aws.ToString(marker) == "" {
			return distributions, nil
		}
	}
}

func (c *Client) distributionMetadata(
	ctx context.Context,
	distribution awscloudfronttypes.DistributionSummary,
) (cloudfrontservice.Distribution, error) {
	distributionARN := aws.ToString(distribution.ARN)
	tags, err := c.listTags(ctx, distributionARN)
	if err != nil {
		return cloudfrontservice.Distribution{}, err
	}
	return mapDistribution(distribution, tags), nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awscloudfront.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awscloudfront.ListTagsForResourceInput{
			Resource: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil || output.Tags == nil {
		return nil, err
	}
	return mapTags(output.Tags.Items), nil
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
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := strings.ToLower(apiErr.ErrorCode())
	return strings.Contains(code, "throttl") || strings.Contains(code, "rate")
}
