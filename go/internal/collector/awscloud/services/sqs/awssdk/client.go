package awssdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	awssqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	sqsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/sqs"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	awssqs.ListQueuesAPIClient
	GetQueueAttributes(context.Context, *awssqs.GetQueueAttributesInput, ...func(*awssqs.Options)) (*awssqs.GetQueueAttributesOutput, error)
	ListQueueTags(context.Context, *awssqs.ListQueueTagsInput, ...func(*awssqs.Options)) (*awssqs.ListQueueTagsOutput, error)
}

// Client adapts AWS SDK SQS pagination into scanner-owned queue metadata.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds an SQS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssqs.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListQueues returns SQS queue metadata visible to the configured AWS
// credentials. It requests an explicit safe attribute set and never calls
// ReceiveMessage or requests the queue Policy attribute.
func (c *Client) ListQueues(ctx context.Context) ([]sqsservice.Queue, error) {
	paginator := awssqs.NewListQueuesPaginator(c.client, &awssqs.ListQueuesInput{})
	var queues []sqsservice.Queue
	for paginator.HasMorePages() {
		var page *awssqs.ListQueuesOutput
		err := c.recordAPICall(ctx, "ListQueues", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, queueURL := range page.QueueUrls {
			queue, err := c.queueMetadata(ctx, queueURL)
			if err != nil {
				return nil, err
			}
			queues = append(queues, queue)
		}
	}
	return queues, nil
}

func (c *Client) queueMetadata(ctx context.Context, queueURL string) (sqsservice.Queue, error) {
	attributes, err := c.getQueueAttributes(ctx, queueURL)
	if err != nil {
		return sqsservice.Queue{}, err
	}
	tags, err := c.listQueueTags(ctx, queueURL)
	if err != nil {
		return sqsservice.Queue{}, err
	}
	return mapQueue(queueURL, attributes, tags), nil
}

func (c *Client) getQueueAttributes(ctx context.Context, queueURL string) (map[string]string, error) {
	attributeNames := standardQueueAttributeNames()
	if isFIFOQueueURL(queueURL) {
		attributeNames = append(attributeNames, fifoQueueAttributeNames()...)
	}
	return c.requestQueueAttributes(ctx, queueURL, attributeNames)
}

func (c *Client) requestQueueAttributes(
	ctx context.Context,
	queueURL string,
	attributeNames []awssqstypes.QueueAttributeName,
) (map[string]string, error) {
	var output *awssqs.GetQueueAttributesOutput
	err := c.recordAPICall(ctx, "GetQueueAttributes", func(callCtx context.Context) error {
		var err error
		output, err = c.client.GetQueueAttributes(callCtx, &awssqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(queueURL),
			AttributeNames: attributeNames,
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

func (c *Client) listQueueTags(ctx context.Context, queueURL string) (map[string]string, error) {
	var output *awssqs.ListQueueTagsOutput
	err := c.recordAPICall(ctx, "ListQueueTags", func(callCtx context.Context) error {
		var err error
		output, err = c.client.ListQueueTags(callCtx, &awssqs.ListQueueTagsInput{
			QueueUrl: aws.String(queueURL),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	if output == nil {
		return nil, nil
	}
	return cloneStringMap(output.Tags), nil
}

func standardQueueAttributeNames() []awssqstypes.QueueAttributeName {
	return []awssqstypes.QueueAttributeName{
		awssqstypes.QueueAttributeNameQueueArn,
		awssqstypes.QueueAttributeNameCreatedTimestamp,
		awssqstypes.QueueAttributeNameLastModifiedTimestamp,
		awssqstypes.QueueAttributeNameDelaySeconds,
		awssqstypes.QueueAttributeNameMaximumMessageSize,
		awssqstypes.QueueAttributeNameMessageRetentionPeriod,
		awssqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds,
		awssqstypes.QueueAttributeNameVisibilityTimeout,
		awssqstypes.QueueAttributeNameKmsMasterKeyId,
		awssqstypes.QueueAttributeNameKmsDataKeyReusePeriodSeconds,
		awssqstypes.QueueAttributeNameSqsManagedSseEnabled,
		awssqstypes.QueueAttributeNameRedrivePolicy,
		awssqstypes.QueueAttributeNameRedriveAllowPolicy,
	}
}

func fifoQueueAttributeNames() []awssqstypes.QueueAttributeName {
	return []awssqstypes.QueueAttributeName{
		awssqstypes.QueueAttributeNameFifoQueue,
		awssqstypes.QueueAttributeNameContentBasedDeduplication,
		awssqstypes.QueueAttributeNameDeduplicationScope,
		awssqstypes.QueueAttributeNameFifoThroughputLimit,
	}
}

func isFIFOQueueURL(queueURL string) bool {
	return strings.HasSuffix(queueNameFromURL(queueURL), ".fifo")
}

func mapQueue(queueURL string, attributes map[string]string, tags map[string]string) sqsservice.Queue {
	queueAttributes := mapQueueAttributes(attributes)
	return sqsservice.Queue{
		ARN:        queueAttributesARN(attributes),
		URL:        strings.TrimSpace(queueURL),
		Name:       queueNameFromURL(queueURL),
		Tags:       cloneStringMap(tags),
		Attributes: queueAttributes,
	}
}

func mapQueueAttributes(attributes map[string]string) sqsservice.QueueAttributes {
	redrive := parseRedrivePolicy(attributes[string(awssqstypes.QueueAttributeNameRedrivePolicy)])
	allow := parseRedriveAllowPolicy(attributes[string(awssqstypes.QueueAttributeNameRedriveAllowPolicy)])
	return sqsservice.QueueAttributes{
		DelaySeconds:                  attributes[string(awssqstypes.QueueAttributeNameDelaySeconds)],
		FIFOQueue:                     parseBool(attributes[string(awssqstypes.QueueAttributeNameFifoQueue)]),
		ContentBasedDeduplication:     parseBool(attributes[string(awssqstypes.QueueAttributeNameContentBasedDeduplication)]),
		DeduplicationScope:            attributes[string(awssqstypes.QueueAttributeNameDeduplicationScope)],
		FIFOThroughputLimit:           attributes[string(awssqstypes.QueueAttributeNameFifoThroughputLimit)],
		MaximumMessageSize:            attributes[string(awssqstypes.QueueAttributeNameMaximumMessageSize)],
		MessageRetentionPeriod:        attributes[string(awssqstypes.QueueAttributeNameMessageRetentionPeriod)],
		ReceiveMessageWaitTimeSeconds: attributes[string(awssqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds)],
		VisibilityTimeout:             attributes[string(awssqstypes.QueueAttributeNameVisibilityTimeout)],
		KMSMasterKeyID:                attributes[string(awssqstypes.QueueAttributeNameKmsMasterKeyId)],
		KMSDataKeyReusePeriodSeconds:  attributes[string(awssqstypes.QueueAttributeNameKmsDataKeyReusePeriodSeconds)],
		SQSManagedSSEEnabled:          parseBool(attributes[string(awssqstypes.QueueAttributeNameSqsManagedSseEnabled)]),
		DeadLetterTargetARN:           redrive.DeadLetterTargetARN,
		MaxReceiveCount:               redrive.MaxReceiveCount,
		RedrivePermission:             allow.RedrivePermission,
		RedriveSourceQueueARNs:        allow.SourceQueueARNs,
		CreatedTimestamp:              attributes[string(awssqstypes.QueueAttributeNameCreatedTimestamp)],
		LastModifiedTimestamp:         attributes[string(awssqstypes.QueueAttributeNameLastModifiedTimestamp)],
	}
}

func queueAttributesARN(attributes map[string]string) string {
	return strings.TrimSpace(attributes[string(awssqstypes.QueueAttributeNameQueueArn)])
}

type redrivePolicy struct {
	DeadLetterTargetARN string `json:"deadLetterTargetArn"`
	MaxReceiveCount     string `json:"maxReceiveCount"`
}

func parseRedrivePolicy(raw string) redrivePolicy {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return redrivePolicy{}
	}
	var decoded struct {
		DeadLetterTargetARN string `json:"deadLetterTargetArn"`
		MaxReceiveCount     any    `json:"maxReceiveCount"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return redrivePolicy{}
	}
	return redrivePolicy{
		DeadLetterTargetARN: strings.TrimSpace(decoded.DeadLetterTargetARN),
		MaxReceiveCount:     strings.TrimSpace(toString(decoded.MaxReceiveCount)),
	}
}

type redriveAllowPolicy struct {
	RedrivePermission string
	SourceQueueARNs   []string
}

func parseRedriveAllowPolicy(raw string) redriveAllowPolicy {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return redriveAllowPolicy{}
	}
	var decoded struct {
		RedrivePermission string   `json:"redrivePermission"`
		SourceQueueARNs   []string `json:"sourceQueueArns"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return redriveAllowPolicy{}
	}
	return redriveAllowPolicy{
		RedrivePermission: strings.TrimSpace(decoded.RedrivePermission),
		SourceQueueARNs:   cloneStrings(decoded.SourceQueueARNs),
	}
}

func queueNameFromURL(queueURL string) string {
	trimmed := strings.TrimSpace(queueURL)
	if trimmed == "" {
		return ""
	}
	return path.Base(trimmed)
}

func parseBool(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func toString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
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

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
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

var _ sqsservice.Client = (*Client)(nil)

var _ apiClient = (*awssqs.Client)(nil)
