package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsroute53 "github.com/aws/aws-sdk-go-v2/service/route53"
	awsroute53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	route53service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	awsroute53.ListHostedZonesAPIClient
	awsroute53.ListResourceRecordSetsAPIClient
	ListTagsForResource(
		context.Context,
		*awsroute53.ListTagsForResourceInput,
		...func(*awsroute53.Options),
	) (*awsroute53.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Route 53 pagination into scanner-owned Route 53
// records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Route 53 SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awsroute53.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListHostedZones returns all Route 53 hosted zones visible to the configured
// AWS credentials.
func (c *Client) ListHostedZones(ctx context.Context) ([]route53service.HostedZone, error) {
	paginator := awsroute53.NewListHostedZonesPaginator(c.client, &awsroute53.ListHostedZonesInput{})
	var zones []route53service.HostedZone
	for paginator.HasMorePages() {
		var page *awsroute53.ListHostedZonesOutput
		err := c.recordAPICall(ctx, "ListHostedZones", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, hostedZone := range page.HostedZones {
			tags, err := c.listHostedZoneTags(ctx, aws.ToString(hostedZone.Id))
			if err != nil {
				return nil, err
			}
			zones = append(zones, mapHostedZone(hostedZone, tags))
		}
	}
	return zones, nil
}

// ListResourceRecordSets returns all record sets for one Route 53 hosted zone.
func (c *Client) ListResourceRecordSets(
	ctx context.Context,
	hostedZone route53service.HostedZone,
) ([]route53service.RecordSet, error) {
	paginator := awsroute53.NewListResourceRecordSetsPaginator(
		c.client,
		&awsroute53.ListResourceRecordSetsInput{
			HostedZoneId: aws.String(hostedZone.ID),
		},
	)
	var records []route53service.RecordSet
	for paginator.HasMorePages() {
		var page *awsroute53.ListResourceRecordSetsOutput
		err := c.recordAPICall(ctx, "ListResourceRecordSets", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, record := range page.ResourceRecordSets {
			records = append(records, mapRecordSet(record))
		}
	}
	return records, nil
}

func (c *Client) listHostedZoneTags(ctx context.Context, hostedZoneID string) (map[string]string, error) {
	trimmed := trimHostedZonePrefix(hostedZoneID)
	if trimmed == "" {
		return nil, nil
	}
	var output *awsroute53.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awsroute53.ListTagsForResourceInput{
			ResourceId:   aws.String(trimmed),
			ResourceType: awsroute53types.TagResourceTypeHostedzone,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.ResourceTagSet == nil {
		return nil, nil
	}
	return mapTags(output.ResourceTagSet.Tags), nil
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
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if isThrottleError(err) {
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

var _ route53service.Client = (*Client)(nil)

var _ apiClient = (*awsroute53.Client)(nil)
