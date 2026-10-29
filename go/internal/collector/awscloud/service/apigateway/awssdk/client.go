// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/apigateway"
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
// integration metadata visible to the configured AWS credentials. A
// dial-timeout or retry-exhausted transport error on any regional list read
// degrades that inventory slice to empty with a network-timeout warning
// instead of failing the whole regional read; authorization and contract
// errors stay terminal.
func (c *Client) Snapshot(ctx context.Context) (apigatewayservice.Snapshot, error) {
	restAPIs, warnings, err := c.listRESTAPIs(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	v2APIs, v2Warnings, err := c.listV2APIs(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	warnings = appendWarningOnce(warnings, v2Warnings...)
	restDomains, restDomainWarnings, err := c.listRESTDomains(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	warnings = appendWarningOnce(warnings, restDomainWarnings...)
	v2Domains, v2DomainWarnings, err := c.listV2Domains(ctx)
	if err != nil {
		return apigatewayservice.Snapshot{}, err
	}
	warnings = appendWarningOnce(warnings, v2DomainWarnings...)
	domains := append(restDomains, v2Domains...)
	return apigatewayservice.Snapshot{Warnings: warnings, RESTAPIs: restAPIs, V2APIs: v2APIs, Domains: domains}, nil
}

// networkTimeoutWarning builds the degrade-with-warning observation for one
// timed-out control-plane read. The message stays static so endpoint
// addresses from the transport error never reach persisted facts.
func (c *Client) networkTimeoutWarning(operation, apiKind, partialComponent, omitted, sourceRecordID string) awscloud.WarningObservation {
	return awscloud.WarningObservation{
		Boundary:    c.boundary,
		WarningKind: awscloud.WarningNetworkTimeout,
		ErrorClass:  "network_timeout",
		Message:     "API Gateway " + operation + " timed out after SDK retries; " + omitted + " omitted for this scan",
		Attributes: map[string]any{
			"api_kind":          apiKind,
			"operation":         operation,
			"partial_component": partialComponent,
		},
		SourceRecordID: sourceRecordID,
	}
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

// isNetworkTimeoutError reports whether err evidences an SDK-internal
// control-plane dial timeout or retry-exhausted transport timeout (issue
// #9535): a deadline the SDK did not get to answer while the caller context
// stays alive, a transport error whose Timeout reports true, or the
// flattened SDK message carrying dial/i-o timeout phrasing. A done caller
// context (cancellation or scan-scope deadline) always returns false so
// caller-scope termination propagates terminal instead of degrading to
// success with a warning. Authorization, throttle, validation, and other
// contract errors never match: none carries a deadline, a transport
// timeout, or timeout phrasing.
func isNetworkTimeoutError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "i/o timeout") {
		return true
	}
	return strings.Contains(message, "dial") && strings.Contains(message, "timeout")
}
