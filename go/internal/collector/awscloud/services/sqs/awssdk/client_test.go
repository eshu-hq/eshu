package awssdk

import (
	"context"
	"fmt"
	"testing"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	awssqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListQueuesReadsOnlyMetadataAttributesAndTags(t *testing.T) {
	queueURL := "https://sqs.us-east-1.amazonaws.com/123456789012/orders"
	client := &fakeSQSAPI{
		listQueuesPages: []*awssqs.ListQueuesOutput{{
			QueueUrls: []string{queueURL},
		}},
		attributes: map[string]string{
			string(awssqstypes.QueueAttributeNameQueueArn):           "arn:aws:sqs:us-east-1:123456789012:orders",
			string(awssqstypes.QueueAttributeNameDelaySeconds):       "5",
			string(awssqstypes.QueueAttributeNameFifoQueue):          "false",
			string(awssqstypes.QueueAttributeNameMaximumMessageSize): "262144",
			string(awssqstypes.QueueAttributeNameRedrivePolicy):      `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:123456789012:orders-dlq","maxReceiveCount":10}`,
			string(awssqstypes.QueueAttributeNameRedriveAllowPolicy): `{"redrivePermission":"byQueue","sourceQueueArns":["arn:aws:sqs:us-east-1:123456789012:orders-source"]}`,
			string(awssqstypes.QueueAttributeNamePolicy):             `{"Statement":[{"Effect":"Allow"}]}`,
		},
		tags: map[string]string{"Environment": "prod"},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceSQS},
	}

	queues, err := adapter.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("ListQueues() error = %v, want nil", err)
	}
	if got, want := len(queues), 1; got != want {
		t.Fatalf("len(queues) = %d, want %d", got, want)
	}
	queue := queues[0]
	if queue.Name != "orders" {
		t.Fatalf("Name = %q, want orders", queue.Name)
	}
	if queue.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", queue.Tags)
	}
	if queue.Attributes.DeadLetterTargetARN != "arn:aws:sqs:us-east-1:123456789012:orders-dlq" {
		t.Fatalf("DeadLetterTargetARN = %q", queue.Attributes.DeadLetterTargetARN)
	}
	if queue.Attributes.MaxReceiveCount != "10" {
		t.Fatalf("MaxReceiveCount = %q, want 10", queue.Attributes.MaxReceiveCount)
	}
	if queue.Attributes.RedrivePermission != "byQueue" {
		t.Fatalf("RedrivePermission = %q, want byQueue", queue.Attributes.RedrivePermission)
	}
	if got, want := queue.Attributes.RedriveSourceQueueARNs, []string{"arn:aws:sqs:us-east-1:123456789012:orders-source"}; !equalStrings(got, want) {
		t.Fatalf("RedriveSourceQueueARNs = %#v, want %#v", got, want)
	}
	if !containsAttributeName(client.attributeNames, awssqstypes.QueueAttributeNameRedriveAllowPolicy) {
		t.Fatalf("GetQueueAttributes did not request RedriveAllowPolicy")
	}
	for _, name := range client.attributeNames {
		if name == awssqstypes.QueueAttributeNamePolicy {
			t.Fatalf("GetQueueAttributes requested Policy; metadata-only scanner must not request queue policy JSON")
		}
		if isTestFIFOOnlyAttribute(name) {
			t.Fatalf("standard queue attribute request included FIFO-only attribute %q", name)
		}
	}
}

func TestClientListQueuesReadsFIFOAttributesOnlyForFIFOQueues(t *testing.T) {
	queueURL := "https://sqs.us-east-1.amazonaws.com/123456789012/orders.fifo"
	client := &fakeSQSAPI{
		listQueuesPages: []*awssqs.ListQueuesOutput{{
			QueueUrls: []string{queueURL},
		}},
		attributesByCall: []map[string]string{
			{
				string(awssqstypes.QueueAttributeNameQueueArn):  "arn:aws:sqs:us-east-1:123456789012:orders.fifo",
				string(awssqstypes.QueueAttributeNameFifoQueue): "true",
			},
			{
				string(awssqstypes.QueueAttributeNameContentBasedDeduplication): "true",
				string(awssqstypes.QueueAttributeNameDeduplicationScope):        "messageGroup",
				string(awssqstypes.QueueAttributeNameFifoThroughputLimit):       "perMessageGroupId",
			},
		},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceSQS},
	}

	queues, err := adapter.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("ListQueues() error = %v, want nil", err)
	}
	if got, want := client.attributeCallCount, 2; got != want {
		t.Fatalf("GetQueueAttributes calls = %d, want %d", got, want)
	}
	if got := queues[0].Attributes.DeduplicationScope; got != "messageGroup" {
		t.Fatalf("DeduplicationScope = %q, want messageGroup", got)
	}
}

func TestClientListQueuesDoesNotRequestFIFOOnlyAttributesForStandardQueues(t *testing.T) {
	queueURL := "https://sqs.us-east-1.amazonaws.com/123456789012/orders"
	client := &fakeSQSAPI{
		listQueuesPages: []*awssqs.ListQueuesOutput{{
			QueueUrls: []string{queueURL},
		}},
		attributes: map[string]string{
			string(awssqstypes.QueueAttributeNameQueueArn):  "arn:aws:sqs:us-east-1:123456789012:orders",
			string(awssqstypes.QueueAttributeNameFifoQueue): "false",
		},
		rejectFIFOOnlyAttributes: true,
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceSQS},
	}

	if _, err := adapter.ListQueues(context.Background()); err != nil {
		t.Fatalf("ListQueues() error = %v, want nil", err)
	}
	if got, want := client.attributeCallCount, 1; got != want {
		t.Fatalf("GetQueueAttributes calls = %d, want %d", got, want)
	}
}

type fakeSQSAPI struct {
	listQueuesPages          []*awssqs.ListQueuesOutput
	listQueuesCalls          int
	attributeCallCount       int
	attributeNames           []awssqstypes.QueueAttributeName
	attributes               map[string]string
	attributesByCall         []map[string]string
	tags                     map[string]string
	rejectFIFOOnlyAttributes bool
}

func (f *fakeSQSAPI) ListQueues(
	_ context.Context,
	_ *awssqs.ListQueuesInput,
	_ ...func(*awssqs.Options),
) (*awssqs.ListQueuesOutput, error) {
	if f.listQueuesCalls >= len(f.listQueuesPages) {
		return &awssqs.ListQueuesOutput{}, nil
	}
	page := f.listQueuesPages[f.listQueuesCalls]
	f.listQueuesCalls++
	return page, nil
}

func (f *fakeSQSAPI) GetQueueAttributes(
	_ context.Context,
	input *awssqs.GetQueueAttributesInput,
	_ ...func(*awssqs.Options),
) (*awssqs.GetQueueAttributesOutput, error) {
	f.attributeNames = append(f.attributeNames, input.AttributeNames...)
	f.attributeCallCount++
	if f.rejectFIFOOnlyAttributes {
		for _, name := range input.AttributeNames {
			if isTestFIFOOnlyAttribute(name) {
				return nil, &smithy.GenericAPIError{
					Code:    "InvalidAttributeName",
					Message: fmt.Sprintf("attribute %s is only valid for FIFO queues", name),
				}
			}
		}
	}
	if len(f.attributesByCall) > 0 {
		attributes := f.attributesByCall[0]
		f.attributesByCall = f.attributesByCall[1:]
		return &awssqs.GetQueueAttributesOutput{Attributes: attributes}, nil
	}
	return &awssqs.GetQueueAttributesOutput{Attributes: f.attributes}, nil
}

func (f *fakeSQSAPI) ListQueueTags(
	context.Context,
	*awssqs.ListQueueTagsInput,
	...func(*awssqs.Options),
) (*awssqs.ListQueueTagsOutput, error) {
	return &awssqs.ListQueueTagsOutput{Tags: f.tags}, nil
}

func containsAttributeName(names []awssqstypes.QueueAttributeName, want awssqstypes.QueueAttributeName) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func isTestFIFOOnlyAttribute(name awssqstypes.QueueAttributeName) bool {
	switch name {
	case awssqstypes.QueueAttributeNameFifoQueue,
		awssqstypes.QueueAttributeNameContentBasedDeduplication,
		awssqstypes.QueueAttributeNameDeduplicationScope,
		awssqstypes.QueueAttributeNameFifoThroughputLimit:
		return true
	default:
		return false
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

var _ apiClient = (*fakeSQSAPI)(nil)
