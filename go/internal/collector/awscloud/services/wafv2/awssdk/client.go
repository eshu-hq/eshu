// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awswafv2 "github.com/aws/aws-sdk-go-v2/service/wafv2"
	awswafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	wafv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/wafv2"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// cloudFrontEndpointRegion is the region the WAFv2 control plane requires for
// the global CLOUDFRONT scope. WAFv2 only serves CloudFront-scope reads from
// us-east-1, so a global-region claim is rebound to that endpoint.
const cloudFrontEndpointRegion = "us-east-1"

// globalRegionLabel is the canonical region label the AWS collector uses for
// global-style claims. A boundary carrying this label (or no region) scans the
// global CLOUDFRONT scope; any concrete region scans REGIONAL. It mirrors the
// label used by the rest of the collector (for example the S3 adapter) so a
// regional boundary can never be silently routed to the global control plane.
const globalRegionLabel = "aws-global"

// listLimit bounds each WAFv2 list page. WAFv2 list APIs are not standard
// paginators, so the adapter loops on NextMarker explicitly.
const listLimit int32 = 100

// regionalProtectedResourceTypes are the regional resource types the adapter
// queries through ListResourcesForWebACL. CloudFront associations are not
// returned by that API and are recorded by the CloudFront scanner instead.
var regionalProtectedResourceTypes = []awswafv2types.ResourceType{
	awswafv2types.ResourceTypeApplicationLoadBalancer,
	awswafv2types.ResourceTypeApiGateway,
	awswafv2types.ResourceTypeAppsync,
	awswafv2types.ResourceTypeCognitioUserPool,
	awswafv2types.ResourceTypeAppRunnerService,
	awswafv2types.ResourceTypeAmplify,
	awswafv2types.ResourceTypeVerifiedAccessInstance,
}

// apiClient is the read-only WAFv2 surface the adapter consumes. It exposes
// only List/Get operations. A reflection gate in exclusion_test.go fails the
// build path if any mutation or data-plane method is added here.
type apiClient interface {
	ListWebACLs(context.Context, *awswafv2.ListWebACLsInput, ...func(*awswafv2.Options)) (*awswafv2.ListWebACLsOutput, error)
	GetWebACL(context.Context, *awswafv2.GetWebACLInput, ...func(*awswafv2.Options)) (*awswafv2.GetWebACLOutput, error)
	ListResourcesForWebACL(context.Context, *awswafv2.ListResourcesForWebACLInput, ...func(*awswafv2.Options)) (*awswafv2.ListResourcesForWebACLOutput, error)
	ListRuleGroups(context.Context, *awswafv2.ListRuleGroupsInput, ...func(*awswafv2.Options)) (*awswafv2.ListRuleGroupsOutput, error)
	GetRuleGroup(context.Context, *awswafv2.GetRuleGroupInput, ...func(*awswafv2.Options)) (*awswafv2.GetRuleGroupOutput, error)
	ListIPSets(context.Context, *awswafv2.ListIPSetsInput, ...func(*awswafv2.Options)) (*awswafv2.ListIPSetsOutput, error)
	GetIPSet(context.Context, *awswafv2.GetIPSetInput, ...func(*awswafv2.Options)) (*awswafv2.GetIPSetOutput, error)
	ListRegexPatternSets(context.Context, *awswafv2.ListRegexPatternSetsInput, ...func(*awswafv2.Options)) (*awswafv2.ListRegexPatternSetsOutput, error)
	GetRegexPatternSet(context.Context, *awswafv2.GetRegexPatternSetInput, ...func(*awswafv2.Options)) (*awswafv2.GetRegexPatternSetOutput, error)
	ListTagsForResource(context.Context, *awswafv2.ListTagsForResourceInput, ...func(*awswafv2.Options)) (*awswafv2.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK for Go v2 WAFv2 reads into scanner-owned metadata. It
// never persists IP set address lists, regex pattern bodies, or rule Statement
// bodies, and it never calls a WAFv2 mutation API.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	scope       awswafv2types.Scope
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a WAFv2 SDK adapter for one claimed AWS boundary. A global
// boundary region selects the CLOUDFRONT scope and rebinds the SDK config to
// the us-east-1 control-plane endpoint; a concrete region selects REGIONAL.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	scope := scopeForBoundary(boundary)
	if scope == awswafv2types.ScopeCloudfront {
		config = config.Copy()
		config.Region = cloudFrontEndpointRegion
	}
	return &Client{
		client:      awswafv2.NewFromConfig(config),
		boundary:    boundary,
		scope:       scope,
		tracer:      tracer,
		instruments: instruments,
	}
}

// scopeForBoundary maps a claim boundary region to the WAFv2 scope. Only the
// canonical global region label (or an empty region) selects CLOUDFRONT; every
// concrete region selects REGIONAL. Matching the exact label keeps a regional
// boundary from being misrouted to the global control plane.
func scopeForBoundary(boundary awscloud.Boundary) awswafv2types.Scope {
	switch strings.TrimSpace(boundary.Region) {
	case globalRegionLabel, "":
		return awswafv2types.ScopeCloudfront
	default:
		return awswafv2types.ScopeRegional
	}
}

// ListWebACLs returns web ACL metadata with reference ARNs, managed rule set
// references, and protected-resource associations resolved from the rules. It
// never returns rule Statement bodies.
func (c *Client) ListWebACLs(ctx context.Context) ([]wafv2service.WebACL, error) {
	var webACLs []wafv2service.WebACL
	var marker *string
	for {
		var page *awswafv2.ListWebACLsOutput
		err := c.recordAPICall(ctx, "ListWebACLs", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListWebACLs(callCtx, &awswafv2.ListWebACLsInput{
				Scope:      c.scope,
				Limit:      aws.Int32(listLimit),
				NextMarker: marker,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return webACLs, nil
		}
		for _, summary := range page.WebACLs {
			webACL, err := c.webACLMetadata(ctx, summary)
			if err != nil {
				return nil, err
			}
			webACLs = append(webACLs, webACL)
		}
		if marker = nextMarker(page.NextMarker); marker == nil {
			return webACLs, nil
		}
	}
}

func (c *Client) webACLMetadata(ctx context.Context, summary awswafv2types.WebACLSummary) (wafv2service.WebACL, error) {
	var output *awswafv2.GetWebACLOutput
	err := c.recordAPICall(ctx, "GetWebACL", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetWebACL(callCtx, &awswafv2.GetWebACLInput{
			Id:    summary.Id,
			Name:  summary.Name,
			Scope: c.scope,
		})
		return err
	})
	if err != nil {
		return wafv2service.WebACL{}, err
	}
	webACL := mapWebACL(string(c.scope), summary, output)
	tags, err := c.listTags(ctx, aws.ToString(summary.ARN))
	if err != nil {
		return wafv2service.WebACL{}, err
	}
	webACL.Tags = tags
	protected, err := c.protectedResources(ctx, aws.ToString(summary.ARN))
	if err != nil {
		return wafv2service.WebACL{}, err
	}
	webACL.ProtectedResources = protected
	return webACL, nil
}

// protectedResources resolves regional associations for one web ACL. The
// CLOUDFRONT scope has no ListResourcesForWebACL surface, so it returns nil and
// CloudFront associations are recorded by the CloudFront scanner.
func (c *Client) protectedResources(ctx context.Context, webACLARN string) ([]wafv2service.ProtectedResource, error) {
	if c.scope != awswafv2types.ScopeRegional || strings.TrimSpace(webACLARN) == "" {
		return nil, nil
	}
	var resources []wafv2service.ProtectedResource
	for _, resourceType := range regionalProtectedResourceTypes {
		var output *awswafv2.ListResourcesForWebACLOutput
		err := c.recordAPICall(ctx, "ListResourcesForWebACL", func(callCtx context.Context) error {
			var err error
			output, err = c.client.ListResourcesForWebACL(callCtx, &awswafv2.ListResourcesForWebACLInput{
				WebACLArn:    aws.String(webACLARN),
				ResourceType: resourceType,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if output == nil {
			continue
		}
		for _, arn := range output.ResourceArns {
			if trimmed := strings.TrimSpace(arn); trimmed != "" {
				resources = append(resources, wafv2service.ProtectedResource{
					ARN:          trimmed,
					ResourceType: string(resourceType),
				})
			}
		}
	}
	return resources, nil
}

func (c *Client) listTags(ctx context.Context, resourceARN string) (map[string]string, error) {
	resourceARN = strings.TrimSpace(resourceARN)
	if resourceARN == "" {
		return nil, nil
	}
	var output *awswafv2.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awswafv2.ListTagsForResourceInput{
			ResourceARN: aws.String(resourceARN),
		})
		return err
	})
	if err != nil || output == nil || output.TagInfoForResource == nil {
		return nil, err
	}
	return mapTags(output.TagInfoForResource.TagList), nil
}

func nextMarker(marker *string) *string {
	if aws.ToString(marker) == "" {
		return nil
	}
	return marker
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

var _ wafv2service.Client = (*Client)(nil)
