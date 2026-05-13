package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	awselbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	elbv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elbv2"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const describeTagsLimit = 20

type apiClient interface {
	awselbv2.DescribeListenersAPIClient
	awselbv2.DescribeLoadBalancersAPIClient
	awselbv2.DescribeRulesAPIClient
	awselbv2.DescribeTargetGroupsAPIClient
	DescribeTags(context.Context, *awselbv2.DescribeTagsInput, ...func(*awselbv2.Options)) (*awselbv2.DescribeTagsOutput, error)
}

// Client adapts AWS SDK ELBv2 pagination into scanner-owned ELBv2 records.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an ELBv2 SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awselbv2.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListLoadBalancers returns all ELBv2 load balancers visible to the configured
// AWS credentials.
func (c *Client) ListLoadBalancers(ctx context.Context) ([]elbv2service.LoadBalancer, error) {
	paginator := awselbv2.NewDescribeLoadBalancersPaginator(c.client, &awselbv2.DescribeLoadBalancersInput{})
	var raw []awselbv2types.LoadBalancer
	var arns []string
	for paginator.HasMorePages() {
		var page *awselbv2.DescribeLoadBalancersOutput
		err := c.recordAPICall(ctx, "DescribeLoadBalancers", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, loadBalancer := range page.LoadBalancers {
			raw = append(raw, loadBalancer)
			arns = append(arns, aws.ToString(loadBalancer.LoadBalancerArn))
		}
	}
	tagSets, err := c.describeTags(ctx, arns)
	if err != nil {
		return nil, err
	}
	loadBalancers := make([]elbv2service.LoadBalancer, 0, len(raw))
	for _, loadBalancer := range raw {
		loadBalancers = append(loadBalancers, mapLoadBalancer(loadBalancer, tagSets[aws.ToString(loadBalancer.LoadBalancerArn)]))
	}
	return loadBalancers, nil
}

// ListListeners returns all listeners on one ELBv2 load balancer.
func (c *Client) ListListeners(
	ctx context.Context,
	loadBalancer elbv2service.LoadBalancer,
) ([]elbv2service.Listener, error) {
	paginator := awselbv2.NewDescribeListenersPaginator(c.client, &awselbv2.DescribeListenersInput{
		LoadBalancerArn: aws.String(loadBalancer.ARN),
	})
	var raw []awselbv2types.Listener
	var arns []string
	for paginator.HasMorePages() {
		var page *awselbv2.DescribeListenersOutput
		err := c.recordAPICall(ctx, "DescribeListeners", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, listener := range page.Listeners {
			raw = append(raw, listener)
			arns = append(arns, aws.ToString(listener.ListenerArn))
		}
	}
	tagSets, err := c.describeTags(ctx, arns)
	if err != nil {
		return nil, err
	}
	listeners := make([]elbv2service.Listener, 0, len(raw))
	for _, listener := range raw {
		listeners = append(listeners, mapListener(listener, tagSets[aws.ToString(listener.ListenerArn)]))
	}
	return listeners, nil
}

// ListRules returns all rules on one ELBv2 listener.
func (c *Client) ListRules(ctx context.Context, listener elbv2service.Listener) ([]elbv2service.Rule, error) {
	paginator := awselbv2.NewDescribeRulesPaginator(c.client, &awselbv2.DescribeRulesInput{
		ListenerArn: aws.String(listener.ARN),
	})
	var raw []awselbv2types.Rule
	var arns []string
	for paginator.HasMorePages() {
		var page *awselbv2.DescribeRulesOutput
		err := c.recordAPICall(ctx, "DescribeRules", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, rule := range page.Rules {
			raw = append(raw, rule)
			arns = append(arns, aws.ToString(rule.RuleArn))
		}
	}
	tagSets, err := c.describeTags(ctx, arns)
	if err != nil {
		return nil, err
	}
	rules := make([]elbv2service.Rule, 0, len(raw))
	for _, rule := range raw {
		rules = append(rules, mapRule(listener.ARN, rule, tagSets[aws.ToString(rule.RuleArn)]))
	}
	return rules, nil
}

// ListTargetGroups returns all ELBv2 target groups visible to the configured
// AWS credentials. It does not call DescribeTargetHealth.
func (c *Client) ListTargetGroups(ctx context.Context) ([]elbv2service.TargetGroup, error) {
	paginator := awselbv2.NewDescribeTargetGroupsPaginator(c.client, &awselbv2.DescribeTargetGroupsInput{})
	var raw []awselbv2types.TargetGroup
	var arns []string
	for paginator.HasMorePages() {
		var page *awselbv2.DescribeTargetGroupsOutput
		err := c.recordAPICall(ctx, "DescribeTargetGroups", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, targetGroup := range page.TargetGroups {
			raw = append(raw, targetGroup)
			arns = append(arns, aws.ToString(targetGroup.TargetGroupArn))
		}
	}
	tagSets, err := c.describeTags(ctx, arns)
	if err != nil {
		return nil, err
	}
	targetGroups := make([]elbv2service.TargetGroup, 0, len(raw))
	for _, targetGroup := range raw {
		targetGroups = append(targetGroups, mapTargetGroup(targetGroup, tagSets[aws.ToString(targetGroup.TargetGroupArn)]))
	}
	return targetGroups, nil
}

func (c *Client) describeTags(ctx context.Context, arns []string) (map[string]map[string]string, error) {
	output := make(map[string]map[string]string)
	for _, chunk := range chunkStrings(nonEmptyStrings(arns), describeTagsLimit) {
		var page *awselbv2.DescribeTagsOutput
		err := c.recordAPICall(ctx, "DescribeTags", func(callCtx context.Context) error {
			var err error
			page, err = c.client.DescribeTags(callCtx, &awselbv2.DescribeTagsInput{
				ResourceArns: chunk,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, description := range page.TagDescriptions {
			output[aws.ToString(description.ResourceArn)] = mapTags(description.Tags)
		}
	}
	return output, nil
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

func nonEmptyStrings(values []string) []string {
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	return output
}

var _ elbv2service.Client = (*Client)(nil)

var _ apiClient = (*awselbv2.Client)(nil)
