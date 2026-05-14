package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigateway"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	restPageLimit int32 = 100
	v2PageLimit         = "100"
)

// Client adapts API Gateway v1 and v2 SDK read-only calls into metadata. It
// never calls API execution paths, API key APIs, authorizer secret APIs,
// mutation APIs, or export APIs.
type Client struct {
	rest        restAPIClient
	v2          v2APIClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an API Gateway SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		rest:        awsapigateway.NewFromConfig(config),
		v2:          awsapigatewayv2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns REST, HTTP, WebSocket, custom-domain, mapping, stage, and
// integration metadata visible to the configured AWS credentials.
func (c *Client) Snapshot(ctx context.Context) (apigatewayservice.Snapshot, error) {
	restAPIs, err := c.listRESTAPIs(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	v2APIs, err := c.listV2APIs(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	restDomains, err := c.listRESTDomains(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	v2Domains, err := c.listV2Domains(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	domains := append(restDomains, v2Domains...)
	return apigatewayservice.Snapshot{RESTAPIs: restAPIs, V2APIs: v2APIs, Domains: domains}, nil
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
