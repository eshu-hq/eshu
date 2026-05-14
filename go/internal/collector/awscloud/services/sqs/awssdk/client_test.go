package awssdk

import (
	"context"
	"testing"

	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	awssqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

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
	for _, name := range client.attributeNames {
		if name == awssqstypes.QueueAttributeNamePolicy {
			t.Fatalf("GetQueueAttributes requested Policy; metadata-only scanner must not request queue policy JSON")
		}
	}
}

type fakeSQSAPI struct {
	listQueuesPages []*awssqs.ListQueuesOutput
	listQueuesCalls int
	attributeNames  []awssqstypes.QueueAttributeName
	attributes      map[string]string
	tags            map[string]string
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
	return &awssqs.GetQueueAttributesOutput{Attributes: f.attributes}, nil
}

func (f *fakeSQSAPI) ListQueueTags(
	context.Context,
	*awssqs.ListQueueTagsInput,
	...func(*awssqs.Options),
) (*awssqs.ListQueueTagsOutput, error) {
	return &awssqs.ListQueueTagsOutput{Tags: f.tags}, nil
}

var _ apiClient = (*fakeSQSAPI)(nil)
