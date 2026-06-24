// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package stepfunctions

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsStateMachineAndActivityMetadataAndRelationships(t *testing.T) {
	stateMachineARN := "arn:aws:states:us-east-1:123456789012:stateMachine:order-fulfillment"
	roleARN := "arn:aws:iam::123456789012:role/states-order-fulfillment"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:charge-order"
	snsARN := "arn:aws:sns:us-east-1:123456789012:order-alerts"
	activityARN := "arn:aws:states:us-east-1:123456789012:activity:human-review"

	client := fakeClient{
		stateMachines: []StateMachine{{
			ARN:            stateMachineARN,
			Name:           "order-fulfillment",
			Type:           "STANDARD",
			Status:         "ACTIVE",
			RoleARN:        roleARN,
			CreationDate:   time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC),
			LoggingLevel:   "ALL",
			TracingEnabled: true,
			StartAt:        "ChargeOrder",
			States: []StateNode{
				{
					Name:        "ChargeOrder",
					Type:        "Task",
					Next:        "NotifyOrder",
					ResourceARN: lambdaARN,
				},
				{
					Name:        "NotifyOrder",
					Type:        "Task",
					End:         true,
					ResourceARN: snsARN,
				},
			},
			ReferencedARNs: []string{lambdaARN, snsARN},
			Tags:           map[string]string{"Environment": "prod"},
		}},
		activities: []Activity{{
			ARN:          activityARN,
			Name:         "human-review",
			CreationDate: time.Date(2026, 5, 14, 16, 5, 0, 0, time.UTC),
			Tags:         map[string]string{"Owner": "ops"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	stateMachine := resourceByType(t, envelopes, awscloud.ResourceTypeStepFunctionsStateMachine)
	if got, want := stateMachine.Payload["arn"], stateMachineARN; got != want {
		t.Fatalf("state machine arn = %#v, want %q", got, want)
	}
	if got, want := stateMachine.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("state machine top-level state = %#v, want %q", got, want)
	}
	smAttributes := attributesOf(t, stateMachine)
	if got, want := smAttributes["type"], "STANDARD"; got != want {
		t.Fatalf("state machine type = %#v, want %q", got, want)
	}
	if got, want := smAttributes["role_arn"], roleARN; got != want {
		t.Fatalf("state machine role_arn = %#v, want %q", got, want)
	}
	if got, want := smAttributes["start_at"], "ChargeOrder"; got != want {
		t.Fatalf("state machine start_at = %#v, want %q", got, want)
	}
	if got, want := smAttributes["tracing_enabled"], true; got != want {
		t.Fatalf("tracing_enabled = %#v, want %v", got, want)
	}
	if got, want := smAttributes["logging_level"], "ALL"; got != want {
		t.Fatalf("logging_level = %#v, want %q", got, want)
	}
	states, ok := smAttributes["states"].([]map[string]any)
	if !ok {
		t.Fatalf("states attribute = %#v, want []map[string]any", smAttributes["states"])
	}
	if got, want := len(states), 2; got != want {
		t.Fatalf("len(states) = %d, want %d", got, want)
	}
	if got, want := states[0]["name"], "ChargeOrder"; got != want {
		t.Fatalf("states[0].name = %#v, want %q", got, want)
	}
	if got, want := states[0]["next"], "NotifyOrder"; got != want {
		t.Fatalf("states[0].next = %#v, want %q", got, want)
	}
	if got, want := states[0]["resource_arn"], lambdaARN; got != want {
		t.Fatalf("states[0].resource_arn = %#v, want %q", got, want)
	}
	if got, want := states[1]["end"], true; got != want {
		t.Fatalf("states[1].end = %#v, want %v", got, want)
	}
	// Sensitive definition contents must never reach the persisted attribute.
	for _, forbidden := range []string{
		"definition",
		"parameters",
		"result_path",
		"result_selector",
		"input_path",
		"output_path",
		"result",
	} {
		if _, exists := smAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; state machine scanner must not store definition literals", forbidden)
		}
	}
	for _, state := range states {
		for _, forbidden := range []string{
			"parameters",
			"result_path",
			"result_selector",
			"input_path",
			"output_path",
			"result",
		} {
			if _, exists := state[forbidden]; exists {
				t.Fatalf("state node %q persisted %s; scanner must not store definition literals", state["name"], forbidden)
			}
		}
	}

	role := relationshipByType(t, envelopes, awscloud.RelationshipStepFunctionsStateMachineUsesIAMRole)
	if got, want := role.Payload["target_arn"], roleARN; got != want {
		t.Fatalf("role target_arn = %#v, want %q", got, want)
	}
	if got, want := role.Payload["target_type"], awscloud.ResourceTypeIAMRole; got != want {
		t.Fatalf("role target_type = %#v, want %q", got, want)
	}

	references := referencedTargets(envelopes)
	for _, want := range []string{lambdaARN, snsARN} {
		if _, ok := references[want]; !ok {
			t.Fatalf("missing referenced-resource relationship for %q in %#v", want, references)
		}
	}
	if got, want := references[lambdaARN], awscloud.ResourceTypeLambdaFunction; got != want {
		t.Fatalf("lambda reference target_type = %q, want %q", got, want)
	}
	if got, want := references[snsARN], awscloud.ResourceTypeSNSTopic; got != want {
		t.Fatalf("sns reference target_type = %q, want %q", got, want)
	}

	activity := resourceByType(t, envelopes, awscloud.ResourceTypeStepFunctionsActivity)
	if got, want := activity.Payload["arn"], activityARN; got != want {
		t.Fatalf("activity arn = %#v, want %q", got, want)
	}
	if got, want := activity.Payload["tags"].(map[string]string)["Owner"], "ops"; got != want {
		t.Fatalf("activity Owner tag = %#v, want %q", got, want)
	}
	activityAttributes := attributesOf(t, activity)
	for _, forbidden := range []string{"input", "output", "task_token", "task_input", "task_output"} {
		if _, exists := activityAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; activity scanner must not store task payload fields", forbidden)
		}
	}
}

func TestScannerSkipsReferencedResourcesThatAreNotARNs(t *testing.T) {
	client := fakeClient{
		stateMachines: []StateMachine{{
			ARN:     "arn:aws:states:us-east-1:123456789012:stateMachine:hello",
			Name:    "hello",
			Type:    "EXPRESS",
			Status:  "ACTIVE",
			StartAt: "SayHello",
			States: []StateNode{{
				Name:        "SayHello",
				Type:        "Task",
				End:         true,
				ResourceARN: "states:::lambda:invoke.waitForTaskToken",
			}},
			ReferencedARNs: []string{"states:::lambda:invoke.waitForTaskToken"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipStepFunctionsStateMachineReferencesResource); got != 0 {
		t.Fatalf("references relationship count = %d, want 0 for non-ARN reference", got)
	}

	// The state node attribute must also drop the non-ARN literal so the
	// scanner does not persist service-integration identifiers as Resource ARNs.
	stateMachine := resourceByType(t, envelopes, awscloud.ResourceTypeStepFunctionsStateMachine)
	states, ok := attributesOf(t, stateMachine)["states"].([]map[string]any)
	if !ok {
		t.Fatalf("states attribute missing or wrong shape: %#v", attributesOf(t, stateMachine)["states"])
	}
	if got, want := len(states), 1; got != want {
		t.Fatalf("len(states) = %d, want %d", got, want)
	}
	if _, exists := states[0]["resource_arn"]; exists {
		t.Fatalf("resource_arn persisted for non-ARN literal %#v; scanner must gate on isARN", states[0])
	}
}

func TestScannerSkipsRoleRelationshipWhenRoleARNMissing(t *testing.T) {
	client := fakeClient{
		stateMachines: []StateMachine{{
			ARN:     "arn:aws:states:us-east-1:123456789012:stateMachine:no-role",
			Name:    "no-role",
			Type:    "STANDARD",
			Status:  "ACTIVE",
			StartAt: "End",
			States:  []StateNode{{Name: "End", Type: "Succeed", End: true}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipStepFunctionsStateMachineUsesIAMRole); got != 0 {
		t.Fatalf("role relationship count = %d, want 0 when role missing", got)
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceStepFunctions,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:stepfunctions:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        17,
		ObservedAt:          time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	stateMachines []StateMachine
	activities    []Activity
}

func (c fakeClient) ListStateMachines(context.Context) ([]StateMachine, error) {
	return c.stateMachines, nil
}

func (c fakeClient) ListActivities(context.Context) ([]Activity, error) {
	return c.activities, nil
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

func referencedTargets(envelopes []facts.Envelope) map[string]string {
	out := make(map[string]string)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != awscloud.RelationshipStepFunctionsStateMachineReferencesResource {
			continue
		}
		targetARN, _ := envelope.Payload["target_arn"].(string)
		targetType, _ := envelope.Payload["target_type"].(string)
		out[targetARN] = targetType
	}
	return out
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
