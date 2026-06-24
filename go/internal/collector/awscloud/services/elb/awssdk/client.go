// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselb "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	awselbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	elbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// describeTagsLimit is the maximum number of load balancer names accepted by one
// DescribeTags call. The Classic ELB API rejects more than 20 names per call.
const describeTagsLimit = 20

// apiClient is the metadata-only Classic ELB read surface the adapter depends
// on. It deliberately names only DescribeLoadBalancers (paginated) and
// DescribeTags. No create, delete, register/deregister, attach/detach, modify,
// or other mutation operation is reachable through this interface; a reflective
// guard test enforces that.
type apiClient interface {
	awselb.DescribeLoadBalancersAPIClient
	DescribeTags(context.Context, *awselb.DescribeTagsInput, ...func(*awselb.Options)) (*awselb.DescribeTagsOutput, error)
}

// Client adapts AWS SDK Classic ELB pagination into scanner-owned ELB records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Classic ELB SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awselb.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListLoadBalancers returns every Classic load balancer visible to the
// configured AWS credentials with reported listeners, registered instances,
// subnets, security groups, and tags attached. It never calls
// DescribeInstanceHealth; live instance health is excluded from this stable
// topology slice.
func (c *Client) ListLoadBalancers(ctx context.Context) ([]elbservice.LoadBalancer, error) {
	paginator := awselb.NewDescribeLoadBalancersPaginator(c.client, &awselb.DescribeLoadBalancersInput{})
	var raw []awselbtypes.LoadBalancerDescription
	var names []string
	for paginator.HasMorePages() {
		var page *awselb.DescribeLoadBalancersOutput
		err := c.recordAPICall(ctx, "DescribeLoadBalancers", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, description := range page.LoadBalancerDescriptions {
			raw = append(raw, description)
			names = append(names, aws.ToString(description.LoadBalancerName))
		}
	}
	tagSets, err := c.describeTags(ctx, names)
	if err != nil {
		return nil, err
	}
	loadBalancers := make([]elbservice.LoadBalancer, 0, len(raw))
	for _, description := range raw {
		loadBalancers = append(loadBalancers, mapLoadBalancer(description, tagSets[aws.ToString(description.LoadBalancerName)]))
	}
	return loadBalancers, nil
}

// describeTags batches DescribeTags calls at describeTagsLimit load balancer
// names per call and returns tag sets keyed by load balancer name.
func (c *Client) describeTags(ctx context.Context, names []string) (map[string]map[string]string, error) {
	output := make(map[string]map[string]string)
	for _, chunk := range chunkStrings(nonEmptyStrings(names), describeTagsLimit) {
		var page *awselb.DescribeTagsOutput
		err := c.recordAPICall(ctx, "DescribeTags", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeTags(callCtx, &awselb.DescribeTagsInput{
				LoadBalancerNames: chunk,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, description := range page.TagDescriptions {
			output[aws.ToString(description.LoadBalancerName)] = mapTags(description.Tags)
		}
	}
	return output, nil
}

// recordAPICall wraps one SDK call with a pagination span and AWS API-call,
// throttle, and result telemetry.
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

// isThrottleError reports whether err is an AWS throttling error so the adapter
// can label the call for the throttle counter.
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

// chunkStrings splits values into slices of at most size elements.
func chunkStrings(values []string, size int) [][]string {
	if len(values) == 0 || size <= 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

// nonEmptyStrings returns the trimmed, non-empty values from values.
func nonEmptyStrings(values []string) []string {
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}

var _ elbservice.Client = (*Client)(nil)

var _ apiClient = (*awselb.Client)(nil)
