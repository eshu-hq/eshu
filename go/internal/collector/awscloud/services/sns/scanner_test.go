package sns

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsTopicFactsMetadataOnlyAndARNEndpointRelationships(t *testing.T) {
	topicARN := "arn:aws:sns:us-east-1:123456789012:orders"
	queueARN := "arn:aws:sqs:us-east-1:123456789012:orders-events"
	client := fakeClient{topics: []Topic{{
		ARN:  topicARN,
		Name: "orders",
		Tags: map[string]string{"Environment": "prod"},
		Attributes: TopicAttributes{
			DisplayName:               "Orders",
			Owner:                     "123456789012",
			SubscriptionsConfirmed:    "2",
			SubscriptionsDeleted:      "0",
			SubscriptionsPending:      "1",
			SignatureVersion:          "2",
			TracingConfig:             "Active",
			KMSMasterKeyID:            "alias/aws/sns",
			FIFOTopic:                 false,
			ContentBasedDeduplication: false,
		},
		Subscriptions: []Subscription{{
			SubscriptionARN: "arn:aws:sns:us-east-1:123456789012:orders:11111111-2222-3333-4444-555555555555",
			Protocol:        "sqs",
			Owner:           "123456789012",
			EndpointARN:     queueARN,
		}, {
			SubscriptionARN: "arn:aws:sns:us-east-1:123456789012:orders:66666666-7777-8888-9999-000000000000",
			Protocol:        "email",
			Owner:           "123456789012",
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	topic := resourceByType(t, envelopes, awscloud.ResourceTypeSNSTopic)
	attributes := attributesOf(t, topic)
	if got, want := attributes["display_name"], "Orders"; got != want {
		t.Fatalf("display_name = %#v, want %q", got, want)
	}
	if got, want := attributes["subscriptions_confirmed"], "2"; got != want {
		t.Fatalf("subscriptions_confirmed = %#v, want %q", got, want)
	}
	if _, exists := attributes["policy"]; exists {
		t.Fatalf("policy attribute persisted; SNS scanner must not store topic policy JSON")
	}
	if _, exists := attributes["data_protection_policy"]; exists {
		t.Fatalf("data_protection_policy attribute persisted; SNS scanner must not store data protection policy JSON")
	}
	if _, exists := attributes["message_body"]; exists {
		t.Fatalf("message body attribute persisted; SNS scanner must not read or store messages")
	}
	if got, want := topic.Payload["arn"], topicARN; got != want {
		t.Fatalf("resource ARN = %#v, want %q", got, want)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipSNSTopicDeliversToResource)
	if got, want := relationship.Payload["target_arn"], queueARN; got != want {
		t.Fatalf("target_arn = %#v, want %q", got, want)
	}
	relAttributes := attributesOf(t, relationship)
	if got, want := relAttributes["protocol"], "sqs"; got != want {
		t.Fatalf("relationship protocol = %#v, want %q", got, want)
	}
	if got, want := countRelationships(envelopes, awscloud.RelationshipSNSTopicDeliversToResource), 1; got != want {
		t.Fatalf("SNS relationship count = %d, want %d; non-ARN endpoints must not persist", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSQS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSNS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:sns:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	topics []Topic
}

func (c fakeClient) ListTopics(context.Context) ([]Topic, error) {
	return c.topics, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
