// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssfn "github.com/aws/aws-sdk-go-v2/service/sfn"
	awssfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListStateMachinesProjectsSafeMetadataAndDefinitionShape(t *testing.T) {
	stateMachineARN := "arn:aws:states:us-east-1:123456789012:stateMachine:order-fulfillment"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:charge-order"
	snsARN := "arn:aws:sns:us-east-1:123456789012:order-alerts"
	roleARN := "arn:aws:iam::123456789012:role/states-order-fulfillment"
	definition := `{
		"Comment": "do not persist this",
		"StartAt": "ChargeOrder",
		"States": {
			"ChargeOrder": {
				"Type": "Task",
				"Resource": "` + lambdaARN + `",
				"Parameters": {"customerEmail": "owner@example.com"},
				"ResultPath": "$.charge",
				"ResultSelector": {"chargeId.$": "$.id"},
				"InputPath": "$.payload",
				"OutputPath": "$.payload",
				"Next": "NotifyOrder"
			},
			"NotifyOrder": {
				"Type": "Task",
				"Resource": "` + snsARN + `",
				"Parameters": {"Message": "order ready"},
				"End": true
			}
		}
	}`

	client := &fakeSFNAPI{
		stateMachinePages: []*awssfn.ListStateMachinesOutput{{
			StateMachines: []awssfntypes.StateMachineListItem{{
				StateMachineArn: aws.String(stateMachineARN),
				Name:            aws.String("order-fulfillment"),
				Type:            awssfntypes.StateMachineTypeStandard,
				CreationDate:    aws.Time(time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC)),
			}},
		}},
		describeStateMachine: &awssfn.DescribeStateMachineOutput{
			StateMachineArn: aws.String(stateMachineARN),
			Name:            aws.String("order-fulfillment"),
			RoleArn:         aws.String(roleARN),
			Type:            awssfntypes.StateMachineTypeStandard,
			Status:          awssfntypes.StateMachineStatusActive,
			Definition:      aws.String(definition),
			CreationDate:    aws.Time(time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC)),
			LoggingConfiguration: &awssfntypes.LoggingConfiguration{
				Level: awssfntypes.LogLevelAll,
			},
			TracingConfiguration: &awssfntypes.TracingConfiguration{Enabled: true},
		},
		stateMachineTags: []awssfntypes.Tag{{
			Key:   aws.String("Environment"),
			Value: aws.String("prod"),
		}},
		activityPages: []*awssfn.ListActivitiesOutput{{
			Activities: []awssfntypes.ActivityListItem{{
				ActivityArn:  aws.String("arn:aws:states:us-east-1:123456789012:activity:human-review"),
				Name:         aws.String("human-review"),
				CreationDate: aws.Time(time.Date(2026, 5, 14, 16, 5, 0, 0, time.UTC)),
			}},
		}},
		activityTags: []awssfntypes.Tag{{
			Key:   aws.String("Owner"),
			Value: aws.String("ops"),
		}},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceStepFunctions},
	}

	machines, err := adapter.ListStateMachines(context.Background())
	if err != nil {
		t.Fatalf("ListStateMachines() error = %v, want nil", err)
	}
	if got, want := len(machines), 1; got != want {
		t.Fatalf("len(machines) = %d, want %d", got, want)
	}
	machine := machines[0]
	if machine.Name != "order-fulfillment" {
		t.Fatalf("machine.Name = %q, want order-fulfillment", machine.Name)
	}
	if machine.RoleARN != roleARN {
		t.Fatalf("machine.RoleARN = %q, want %q", machine.RoleARN, roleARN)
	}
	if !machine.TracingEnabled {
		t.Fatalf("machine.TracingEnabled = false, want true")
	}
	if machine.LoggingLevel != "ALL" {
		t.Fatalf("machine.LoggingLevel = %q, want ALL", machine.LoggingLevel)
	}
	if machine.StartAt != "ChargeOrder" {
		t.Fatalf("machine.StartAt = %q, want ChargeOrder", machine.StartAt)
	}
	if got, want := machine.Tags["Environment"], "prod"; got != want {
		t.Fatalf("machine.Tags[Environment] = %q, want %q", got, want)
	}
	if got, want := len(machine.States), 2; got != want {
		t.Fatalf("len(machine.States) = %d, want %d", got, want)
	}
	byName := make(map[string]struct {
		Type        string
		Next        string
		End         bool
		ResourceARN string
	}, len(machine.States))
	for _, state := range machine.States {
		byName[state.Name] = struct {
			Type        string
			Next        string
			End         bool
			ResourceARN string
		}{Type: state.Type, Next: state.Next, End: state.End, ResourceARN: state.ResourceARN}
	}
	if got := byName["ChargeOrder"]; got.Type != "Task" || got.Next != "NotifyOrder" || got.ResourceARN != lambdaARN {
		t.Fatalf("ChargeOrder state = %#v, want Task Next=NotifyOrder Resource=%q", got, lambdaARN)
	}
	if got := byName["NotifyOrder"]; !got.End || got.ResourceARN != snsARN {
		t.Fatalf("NotifyOrder state = %#v, want End=true Resource=%q", got, snsARN)
	}
	// The adapter must surface the ARN references from the definition so the
	// scanner can emit relationship facts without re-parsing the raw document.
	references := map[string]bool{lambdaARN: false, snsARN: false}
	for _, ref := range machine.ReferencedARNs {
		references[ref] = true
	}
	for arn, ok := range references {
		if !ok {
			t.Fatalf("missing referenced ARN %q in %#v", arn, machine.ReferencedARNs)
		}
	}

	activities, err := adapter.ListActivities(context.Background())
	if err != nil {
		t.Fatalf("ListActivities() error = %v, want nil", err)
	}
	if got, want := len(activities), 1; got != want {
		t.Fatalf("len(activities) = %d, want %d", got, want)
	}
	activity := activities[0]
	if activity.Name != "human-review" {
		t.Fatalf("activity.Name = %q, want human-review", activity.Name)
	}
	if got, want := activity.Tags["Owner"], "ops"; got != want {
		t.Fatalf("activity.Tags[Owner] = %q, want %q", got, want)
	}
}

func TestClientListStateMachinesSkipsActivitiesAndDoesNotMutate(t *testing.T) {
	client := &fakeSFNAPI{
		stateMachinePages: []*awssfn.ListStateMachinesOutput{{
			StateMachines: []awssfntypes.StateMachineListItem{{
				StateMachineArn: aws.String("arn:aws:states:us-east-1:123456789012:stateMachine:empty"),
				Name:            aws.String("empty"),
				Type:            awssfntypes.StateMachineTypeExpress,
			}},
		}},
		describeStateMachine: &awssfn.DescribeStateMachineOutput{
			StateMachineArn: aws.String("arn:aws:states:us-east-1:123456789012:stateMachine:empty"),
			Name:            aws.String("empty"),
			Type:            awssfntypes.StateMachineTypeExpress,
			Definition:      aws.String(`{"StartAt":"End","States":{"End":{"Type":"Succeed"}}}`),
		},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceStepFunctions},
	}
	if _, err := adapter.ListStateMachines(context.Background()); err != nil {
		t.Fatalf("ListStateMachines() error = %v, want nil", err)
	}
	if client.startExecutionCalls != 0 || client.createCalls != 0 || client.updateCalls != 0 || client.deleteCalls != 0 {
		t.Fatalf("mutation API called: start=%d create=%d update=%d delete=%d",
			client.startExecutionCalls, client.createCalls, client.updateCalls, client.deleteCalls)
	}
}

type fakeSFNAPI struct {
	stateMachinePages    []*awssfn.ListStateMachinesOutput
	stateMachineCalls    int
	describeStateMachine *awssfn.DescribeStateMachineOutput
	stateMachineTags     []awssfntypes.Tag
	activityPages        []*awssfn.ListActivitiesOutput
	activityCalls        int
	activityTags         []awssfntypes.Tag

	// Counters that must remain zero — the adapter is forbidden from calling
	// any mutation or execution-payload API.
	startExecutionCalls int
	createCalls         int
	updateCalls         int
	deleteCalls         int
}

func (f *fakeSFNAPI) ListStateMachines(
	_ context.Context,
	_ *awssfn.ListStateMachinesInput,
	_ ...func(*awssfn.Options),
) (*awssfn.ListStateMachinesOutput, error) {
	if f.stateMachineCalls >= len(f.stateMachinePages) {
		return &awssfn.ListStateMachinesOutput{}, nil
	}
	page := f.stateMachinePages[f.stateMachineCalls]
	f.stateMachineCalls++
	return page, nil
}

func (f *fakeSFNAPI) DescribeStateMachine(
	_ context.Context,
	_ *awssfn.DescribeStateMachineInput,
	_ ...func(*awssfn.Options),
) (*awssfn.DescribeStateMachineOutput, error) {
	if f.describeStateMachine == nil {
		return &awssfn.DescribeStateMachineOutput{}, nil
	}
	return f.describeStateMachine, nil
}

func (f *fakeSFNAPI) ListActivities(
	_ context.Context,
	_ *awssfn.ListActivitiesInput,
	_ ...func(*awssfn.Options),
) (*awssfn.ListActivitiesOutput, error) {
	if f.activityCalls >= len(f.activityPages) {
		return &awssfn.ListActivitiesOutput{}, nil
	}
	page := f.activityPages[f.activityCalls]
	f.activityCalls++
	return page, nil
}

func (f *fakeSFNAPI) ListTagsForResource(
	_ context.Context,
	input *awssfn.ListTagsForResourceInput,
	_ ...func(*awssfn.Options),
) (*awssfn.ListTagsForResourceOutput, error) {
	arn := aws.ToString(input.ResourceArn)
	if arn == "" {
		return &awssfn.ListTagsForResourceOutput{}, nil
	}
	if strings.Contains(arn, ":activity:") {
		return &awssfn.ListTagsForResourceOutput{Tags: f.activityTags}, nil
	}
	return &awssfn.ListTagsForResourceOutput{Tags: f.stateMachineTags}, nil
}

var _ apiClient = (*fakeSFNAPI)(nil)
