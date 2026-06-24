// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	apigatewayv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigatewayv2"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// pageLimit bounds each API Gateway v2 list page. The v2 list APIs cap
// MaxResults at "100".
const pageLimit = "100"

// apiClient is the API Gateway v2 read surface the adapter uses. It
// deliberately omits ExportApi (dumps the full OpenAPI body),
// GetIntegrationResponse/GetIntegrationResponses and
// GetRouteResponse/GetRouteResponses (surface response mapping templates),
// GetModel/GetModels/GetModelTemplate (surface request models/templates), and
// every Create/Update/Delete/Import operation. The integration list response
// does include RequestTemplates and RequestParameters fields, but the mapper
// never reads or copies them, so no template reaches a fact. A reflection test
// asserts the forbidden methods stay absent from this interface.
type apiClient interface {
	GetApis(context.Context, *awsapigatewayv2.GetApisInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApisOutput, error)
	GetStages(context.Context, *awsapigatewayv2.GetStagesInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetStagesOutput, error)
	GetRoutes(context.Context, *awsapigatewayv2.GetRoutesInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetRoutesOutput, error)
	GetIntegrations(context.Context, *awsapigatewayv2.GetIntegrationsInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetIntegrationsOutput, error)
	GetAuthorizers(context.Context, *awsapigatewayv2.GetAuthorizersInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetAuthorizersOutput, error)
	GetDomainNames(context.Context, *awsapigatewayv2.GetDomainNamesInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetDomainNamesOutput, error)
	GetApiMappings(context.Context, *awsapigatewayv2.GetApiMappingsInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApiMappingsOutput, error)
	GetVpcLinks(context.Context, *awsapigatewayv2.GetVpcLinksInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetVpcLinksOutput, error)
}

// Client adapts AWS SDK API Gateway v2 read-only calls into scanner-owned
// metadata. It never calls ExportApi, any integration/route response reader,
// any model/template reader, or any mutation API, and it never maps request or
// response mapping templates, authorizer invocation URIs, or credential ARNs.
type Client struct {
	api         apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an API Gateway v2 SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		api:         awsapigatewayv2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// Snapshot returns HTTP/WebSocket API, stage, route, integration, authorizer,
// custom-domain, and VPC link metadata visible to the configured AWS
// credentials.
func (c *Client) Snapshot(ctx context.Context) (apigatewayv2service.Snapshot, error) {
	apis, err := c.listAPIs(ctx)
	if err != nil {
		return apigatewayv2service.Snapshot{}, err
	}
	for i := range apis {
		if err := c.hydrateAPI(ctx, &apis[i]); err != nil {
			return apigatewayv2service.Snapshot{}, err
		}
	}
	vpcLinks, err := c.listVPCLinks(ctx)
	if err != nil {
		return apigatewayv2service.Snapshot{}, err
	}
	domains, err := c.listDomains(ctx)
	if err != nil {
		return apigatewayv2service.Snapshot{}, err
	}
	return apigatewayv2service.Snapshot{APIs: apis, VPCLinks: vpcLinks, Domains: domains}, nil
}

// hydrateAPI loads the stages, routes, integrations, and authorizers for one
// API.
func (c *Client) hydrateAPI(ctx context.Context, api *apigatewayv2service.API) error {
	apiID := strings.TrimSpace(api.ID)
	if apiID == "" {
		return nil
	}
	var err error
	if api.Stages, err = c.listStages(ctx, apiID); err != nil {
		return err
	}
	if api.Routes, err = c.listRoutes(ctx, apiID); err != nil {
		return err
	}
	if api.Integrations, err = c.listIntegrations(ctx, apiID); err != nil {
		return err
	}
	if api.Authorizers, err = c.listAuthorizers(ctx, apiID); err != nil {
		return err
	}
	return nil
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
	return strings.Contains(code, "throttl") ||
		strings.Contains(code, "rate") ||
		code == "requestlimitexceeded" ||
		code == "toomanyrequestsexception" ||
		code == "slowdown"
}

var _ apigatewayv2service.Client = (*Client)(nil)

var _ apiClient = (*awsapigatewayv2.Client)(nil)
