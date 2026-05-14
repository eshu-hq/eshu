package sqs

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsQueueFactsMetadataOnlyAndDLQRelationship(t *testing.T) {
	queueARN := "arn:aws:sqs:us-east-1:123456789012:orders"
	dlqARN := "arn:aws:sqs:us-east-1:123456789012:orders-dlq"
	client := fakeClient{queues: []Queue{{
		ARN:  queueARN,
		URL:  "https://sqs.us-east-1.amazonaws.com/123456789012/orders",
		Name: "orders",
		Tags: map[string]string{"Environment": "prod"},
		Attributes: QueueAttributes{
			DelaySeconds:                  "5",
			FIFOQueue:                     false,
			MaximumMessageSize:            "262144",
			MessageRetentionPeriod:        "345600",
			ReceiveMessageWaitTimeSeconds: "10",
			VisibilityTimeout:             "45",
			KMSMasterKeyID:                "alias/aws/sqs",
			SQSManagedSSEEnabled:          true,
			DeadLetterTargetARN:           dlqARN,
			MaxReceiveCount:               "3",
			RedrivePermission:             "byQueue",
			RedriveSourceQueueARNs:        []string{"arn:aws:sqs:us-east-1:123456789012:orders-source"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	queue := resourceByType(t, envelopes, awscloud.ResourceTypeSQSQueue)
	attributes := attributesOf(t, queue)
	if got, want := attributes["queue_url"], "https://sqs.us-east-1.amazonaws.com/123456789012/orders"; got != want {
		t.Fatalf("queue_url = %#v, want %q", got, want)
	}
	if got, want := attributes["fifo_queue"], false; got != want {
		t.Fatalf("fifo_queue = %#v, want %v", got, want)
	}
	if got, want := attributes["dead_letter_target_arn"], dlqARN; got != want {
		t.Fatalf("dead_letter_target_arn = %#v, want %q", got, want)
	}
	if got, want := attributes["redrive_permission"], "byQueue"; got != want {
		t.Fatalf("redrive_permission = %#v, want %q", got, want)
	}
	if got, want := attributes["redrive_source_queue_arns"], []string{"arn:aws:sqs:us-east-1:123456789012:orders-source"}; !equalStringSlices(got, want) {
		t.Fatalf("redrive_source_queue_arns = %#v, want %#v", got, want)
	}
	if _, exists := attributes["policy"]; exists {
		t.Fatalf("policy attribute persisted; SQS scanner must not store queue policy JSON")
	}
	if _, exists := attributes["message_body"]; exists {
		t.Fatalf("message body attribute persisted; SQS scanner must not read or store messages")
	}
	if got, want := queue.Payload["arn"], queueARN; got != want {
		t.Fatalf("resource ARN = %#v, want %q", got, want)
	}
	assertRelationship(t, envelopes, awscloud.RelationshipSQSQueueUsesDeadLetterQueue)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSQS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:sqs:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	queues []Queue
}

func (c fakeClient) ListQueues(context.Context) ([]Queue, error) {
	return c.queues, nil
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func equalStringSlices(got any, want []string) bool {
	values, ok := got.([]string)
	if !ok || len(values) != len(want) {
		return false
	}
	for i := range values {
		if values[i] != want[i] {
			return false
		}
	}
	return true
}
