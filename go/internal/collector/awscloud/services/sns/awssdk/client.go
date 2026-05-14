package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	awssnstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	snsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sns"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	awssns.ListTopicsAPIClient
	GetTopicAttributes(context.Context, *awssns.GetTopicAttributesInput, ...func(*awssns.Options)) (*awssns.GetTopicAttributesOutput, error)
	ListTagsForResource(context.Context, *awssns.ListTagsForResourceInput, ...func(*awssns.Options)) (*awssns.ListTagsForResourceOutput, error)
	ListSubscriptionsByTopic(context.Context, *awssns.ListSubscriptionsByTopicInput, ...func(*awssns.Options)) (*awssns.ListSubscriptionsByTopicOutput, error)
}

// Client adapts AWS SDK SNS pagination into scanner-owned topic metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an SNS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssns.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListTopics returns SNS topic metadata visible to the configured AWS
// credentials. It reads attributes, tags, and subscription metadata; it never
// publishes messages or mutates topics/subscriptions.
func (c *Client) ListTopics(ctx context.Context) ([]snsservice.Topic, error) {
	paginator := awssns.NewListTopicsPaginator(c.client, &awssns.ListTopicsInput{})
	var topics []snsservice.Topic
	for paginator.HasMorePages() {
		var page *awssns.ListTopicsOutput
		err := c.recordAPICall(ctx, "ListTopics", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, topic := range page.Topics {
			topicARN := aws.ToString(topic.TopicArn)
			if strings.TrimSpace(topicARN) == "" {
				continue
			}
			mapped, err := c.topicMetadata(ctx, topicARN)
			if err != nil {
				return nil, err
			}
			topics = append(topics, mapped)
		}
	}
	return topics, nil
}

func (c *Client) topicMetadata(ctx context.Context, topicARN string) (snsservice.Topic, error) {
	attributes, err := c.getTopicAttributes(ctx, topicARN)
	if err != nil {
		return snsservice.Topic{}, err
	}
	tags, err := c.listTags(ctx, topicARN)
	if err != nil {
		return snsservice.Topic{}, err
	}
	subscriptions, err := c.listSubscriptions(ctx, topicARN)
	if err != nil {
		return snsservice.Topic{}, err
	}
	return mapTopic(topicARN, attributes, tags, subscriptions), nil
}

func (c *Client) getTopicAttributes(ctx context.Context, topicARN string) (map[string]string, error) {
	var output *awssns.GetTopicAttributesOutput
	err := c.recordAPICall(ctx, "GetTopicAttributes", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetTopicAttributes(callCtx, &awssns.GetTopicAttributesInput{
			TopicArn: aws.String(topicARN),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return output.Attributes, nil
}

func (c *Client) listTags(ctx context.Context, topicARN string) (map[string]string, error) {
	var output *awssns.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListTagsForResource(callCtx, &awssns.ListTagsForResourceInput{
			ResourceArn: aws.String(topicARN),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return tagsMap(output.Tags), nil
}

func (c *Client) listSubscriptions(ctx context.Context, topicARN string) ([]snsservice.Subscription, error) {
	paginator := awssns.NewListSubscriptionsByTopicPaginator(c.client, &awssns.ListSubscriptionsByTopicInput{
		TopicArn: aws.String(topicARN),
	})
	var subscriptions []snsservice.Subscription
	for paginator.HasMorePages() {
		var page *awssns.ListSubscriptionsByTopicOutput
		err := c.recordAPICall(ctx, "ListSubscriptionsByTopic", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, subscription := range page.Subscriptions {
			subscriptions = append(subscriptions, mapSubscription(subscription))
		}
	}
	return subscriptions, nil
}

func mapTopic(
	topicARN string,
	attributes map[string]string,
	tags map[string]string,
	subscriptions []snsservice.Subscription,
) snsservice.Topic {
	attrs := mapTopicAttributes(attributes)
	arn := firstNonEmpty(attributes["TopicArn"], topicARN)
	return snsservice.Topic{
		ARN:           arn,
		Name:          topicNameFromARN(arn),
		Tags:          cloneStringMap(tags),
		Attributes:    attrs,
		Subscriptions: subscriptions,
	}
}

func mapTopicAttributes(attributes map[string]string) snsservice.TopicAttributes {
	return snsservice.TopicAttributes{
		DisplayName:               attributes["DisplayName"],
		Owner:                     attributes["Owner"],
		SubscriptionsConfirmed:    attributes["SubscriptionsConfirmed"],
		SubscriptionsDeleted:      attributes["SubscriptionsDeleted"],
		SubscriptionsPending:      attributes["SubscriptionsPending"],
		SignatureVersion:          attributes["SignatureVersion"],
		TracingConfig:             attributes["TracingConfig"],
		KMSMasterKeyID:            attributes["KmsMasterKeyId"],
		FIFOTopic:                 parseBool(attributes["FifoTopic"]),
		ContentBasedDeduplication: parseBool(attributes["ContentBasedDeduplication"]),
		ArchivePolicy:             attributes["ArchivePolicy"],
		BeginningArchiveTime:      attributes["BeginningArchiveTime"],
	}
}

func mapSubscription(subscription awssnstypes.Subscription) snsservice.Subscription {
	endpoint := aws.ToString(subscription.Endpoint)
	return snsservice.Subscription{
		SubscriptionARN: aws.ToString(subscription.SubscriptionArn),
		Protocol:        aws.ToString(subscription.Protocol),
		Owner:           aws.ToString(subscription.Owner),
		EndpointARN:     arnOnly(endpoint),
	}
}

func tagsMap(tags []awssnstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func arnOnly(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "arn:") {
		return trimmed
	}
	return ""
}

func topicNameFromARN(topicARN string) string {
	trimmed := strings.TrimSpace(topicARN)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) < 6 {
		return trimmed
	}
	return parts[len(parts)-1]
}

func parseBool(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
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

var _ snsservice.Client = (*Client)(nil)

var _ apiClient = (*awssns.Client)(nil)
