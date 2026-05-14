package eventbridge

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsEventBridgeMetadataOnlyFactsAndRelationships(t *testing.T) {
	busARN := "arn:aws:events:us-east-1:123456789012:event-bus/orders"
	ruleARN := "arn:aws:events:us-east-1:123456789012:rule/orders/route-orders"
	targetARN := "arn:aws:lambda:us-east-1:123456789012:function:order-router"
	deadLetterARN := "arn:aws:sqs:us-east-1:123456789012:eventbridge-dlq"
	client := fakeClient{buses: []EventBus{{
		ARN:              busARN,
		Name:             "orders",
		Description:      "orders event bus",
		CreationTime:     time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC),
		LastModifiedTime: time.Date(2026, 5, 14, 16, 10, 0, 0, time.UTC),
		Tags:             map[string]string{"Environment": "prod"},
		Rules: []Rule{{
			ARN:                ruleARN,
			Name:               "route-orders",
			EventBusName:       "orders",
			Description:        "route order events",
			EventPattern:       `{"source":["orders"]}`,
			ManagedBy:          "events.amazonaws.com",
			RoleARN:            "arn:aws:iam::123456789012:role/eventbridge-route-orders",
			ScheduleExpression: "rate(5 minutes)",
			State:              "ENABLED",
			CreatedBy:          "123456789012",
			Tags:               map[string]string{"Owner": "platform"},
			Targets: []Target{{
				ID:                       "lambda-target",
				ARN:                      targetARN,
				RoleARN:                  "arn:aws:iam::123456789012:role/eventbridge-target",
				DeadLetterARN:            deadLetterARN,
				MaximumEventAgeInSeconds: 3600,
				MaximumRetryAttempts:     4,
			}},
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	bus := resourceByType(t, envelopes, awscloud.ResourceTypeEventBridgeEventBus)
	busAttributes := attributesOf(t, bus)
	if got, want := busAttributes["description"], "orders event bus"; got != want {
		t.Fatalf("bus description = %#v, want %q", got, want)
	}
	if _, exists := busAttributes["policy"]; exists {
		t.Fatalf("policy attribute persisted; EventBridge scanner must not store event bus policy JSON")
	}

	rule := resourceByType(t, envelopes, awscloud.ResourceTypeEventBridgeRule)
	ruleAttributes := attributesOf(t, rule)
	if got, want := ruleAttributes["event_pattern"], `{"source":["orders"]}`; got != want {
		t.Fatalf("event_pattern = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"input", "input_path", "input_transformer", "http_parameters"} {
		if _, exists := ruleAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; EventBridge scanner must not store target payload fields", forbidden)
		}
	}

	ruleBus := relationshipByType(t, envelopes, awscloud.RelationshipEventBridgeRuleOnEventBus)
	if got, want := ruleBus.Payload["target_arn"], busARN; got != want {
		t.Fatalf("rule bus target_arn = %#v, want %q", got, want)
	}

	target := relationshipByType(t, envelopes, awscloud.RelationshipEventBridgeRuleTargetsResource)
	if got, want := target.Payload["target_arn"], targetARN; got != want {
		t.Fatalf("target_arn = %#v, want %q", got, want)
	}
	targetAttributes := attributesOf(t, target)
	if got, want := targetAttributes["dead_letter_arn"], deadLetterARN; got != want {
		t.Fatalf("dead_letter_arn = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"input", "input_path", "input_transformer", "http_parameters"} {
		if _, exists := targetAttributes[forbidden]; exists {
			t.Fatalf("%s relationship attribute persisted; EventBridge scanner must not store payload fields", forbidden)
		}
	}
}

func TestScannerSkipsNonARNTargets(t *testing.T) {
	client := fakeClient{buses: []EventBus{{
		ARN:  "arn:aws:events:us-east-1:123456789012:event-bus/orders",
		Name: "orders",
		Rules: []Rule{{
			ARN:          "arn:aws:events:us-east-1:123456789012:rule/orders/route-orders",
			Name:         "route-orders",
			EventBusName: "orders",
			Targets: []Target{{
				ID:  "webhook",
				ARN: "https://example.com/hook",
			}},
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipEventBridgeRuleTargetsResource); got != 0 {
		t.Fatalf("target relationship count = %d, want 0 for non-ARN target", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceEventBridge,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:eventbridge:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	buses []EventBus
}

func (c fakeClient) ListEventBuses(context.Context) ([]EventBus, error) {
	return c.buses, nil
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
