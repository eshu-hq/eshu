package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	awssnstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListTopicsReadsSafeMetadataTagsAndARNSubscriptions(t *testing.T) {
	topicARN := "arn:aws:sns:us-east-1:123456789012:orders"
	queueARN := "arn:aws:sqs:us-east-1:123456789012:orders-events"
	client := &fakeSNSAPI{
		listTopicsPages: []*awssns.ListTopicsOutput{{
			Topics: []awssnstypes.Topic{{TopicArn: aws.String(topicARN)}},
		}},
		attributes: map[string]string{
			"TopicArn":                  topicARN,
			"DisplayName":               "Orders",
			"Owner":                     "123456789012",
			"SubscriptionsConfirmed":    "2",
			"SubscriptionsDeleted":      "0",
			"SubscriptionsPending":      "1",
			"SignatureVersion":          "2",
			"TracingConfig":             "Active",
			"KmsMasterKeyId":            "alias/aws/sns",
			"FifoTopic":                 "false",
			"ContentBasedDeduplication": "false",
			"Policy":                    `{"Statement":[{"Effect":"Allow"}]}`,
			"DeliveryPolicy":            `{"healthyRetryPolicy":{"numRetries":3}}`,
			"EffectiveDeliveryPolicy":   `{"healthyRetryPolicy":{"numRetries":3}}`,
			"DataProtectionPolicy":      `{"Name":"protect"}`,
		},
		tags: []awssnstypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
		subscriptionPages: []*awssns.ListSubscriptionsByTopicOutput{{
			Subscriptions: []awssnstypes.Subscription{{
				TopicArn:        aws.String(topicARN),
				Protocol:        aws.String("sqs"),
				SubscriptionArn: aws.String(topicARN + ":11111111-2222-3333-4444-555555555555"),
				Owner:           aws.String("123456789012"),
				Endpoint:        aws.String(queueARN),
			}, {
				TopicArn:        aws.String(topicARN),
				Protocol:        aws.String("email"),
				SubscriptionArn: aws.String(topicARN + ":66666666-7777-8888-9999-000000000000"),
				Owner:           aws.String("123456789012"),
				Endpoint:        aws.String("owner@example.com"),
			}},
		}},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceSNS},
	}

	topics, err := adapter.ListTopics(context.Background())
	if err != nil {
		t.Fatalf("ListTopics() error = %v, want nil", err)
	}
	if got, want := len(topics), 1; got != want {
		t.Fatalf("len(topics) = %d, want %d", got, want)
	}
	topic := topics[0]
	if topic.Name != "orders" {
		t.Fatalf("Name = %q, want orders", topic.Name)
	}
	if topic.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", topic.Tags)
	}
	if topic.Attributes.DisplayName != "Orders" {
		t.Fatalf("DisplayName = %q, want Orders", topic.Attributes.DisplayName)
	}
	if topic.Attributes.Owner != "123456789012" {
		t.Fatalf("Owner = %q, want 123456789012", topic.Attributes.Owner)
	}
	if got, want := len(topic.Subscriptions), 2; got != want {
		t.Fatalf("len(Subscriptions) = %d, want %d", got, want)
	}
	if topic.Subscriptions[0].EndpointARN != queueARN {
		t.Fatalf("ARN subscription endpoint = %q, want %q", topic.Subscriptions[0].EndpointARN, queueARN)
	}
	if topic.Subscriptions[1].EndpointARN != "" {
		t.Fatalf("non-ARN subscription endpoint leaked as %q", topic.Subscriptions[1].EndpointARN)
	}
}

type fakeSNSAPI struct {
	listTopicsPages   []*awssns.ListTopicsOutput
	listTopicsCalls   int
	attributes        map[string]string
	tags              []awssnstypes.Tag
	subscriptionPages []*awssns.ListSubscriptionsByTopicOutput
	subscriptionCalls int
}

func (f *fakeSNSAPI) ListTopics(
	_ context.Context,
	_ *awssns.ListTopicsInput,
	_ ...func(*awssns.Options),
) (*awssns.ListTopicsOutput, error) {
	if f.listTopicsCalls >= len(f.listTopicsPages) {
		return &awssns.ListTopicsOutput{}, nil
	}
	page := f.listTopicsPages[f.listTopicsCalls]
	f.listTopicsCalls++
	return page, nil
}

func (f *fakeSNSAPI) GetTopicAttributes(
	_ context.Context,
	input *awssns.GetTopicAttributesInput,
	_ ...func(*awssns.Options),
) (*awssns.GetTopicAttributesOutput, error) {
	if aws.ToString(input.TopicArn) == "" {
		return nil, nil
	}
	return &awssns.GetTopicAttributesOutput{Attributes: f.attributes}, nil
}

func (f *fakeSNSAPI) ListTagsForResource(
	_ context.Context,
	input *awssns.ListTagsForResourceInput,
	_ ...func(*awssns.Options),
) (*awssns.ListTagsForResourceOutput, error) {
	if aws.ToString(input.ResourceArn) == "" {
		return nil, nil
	}
	return &awssns.ListTagsForResourceOutput{Tags: f.tags}, nil
}

func (f *fakeSNSAPI) ListSubscriptionsByTopic(
	_ context.Context,
	input *awssns.ListSubscriptionsByTopicInput,
	_ ...func(*awssns.Options),
) (*awssns.ListSubscriptionsByTopicOutput, error) {
	if aws.ToString(input.TopicArn) == "" {
		return nil, nil
	}
	if f.subscriptionCalls >= len(f.subscriptionPages) {
		return &awssns.ListSubscriptionsByTopicOutput{}, nil
	}
	page := f.subscriptionPages[f.subscriptionCalls]
	f.subscriptionCalls++
	return page, nil
}

var _ apiClient = (*fakeSNSAPI)(nil)
