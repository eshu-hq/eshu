// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsevents "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	awseventstypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

func TestClientListEventBusesReadsSafeMetadataRulesTargetsAndTags(t *testing.T) {
	busARN := "arn:aws:events:us-east-1:123456789012:event-bus/orders"
	ruleARN := "arn:aws:events:us-east-1:123456789012:rule/orders/route-orders"
	targetARN := "arn:aws:lambda:us-east-1:123456789012:function:order-router"
	client := &fakeEventBridgeAPI{
		eventBusPages: []*awsevents.ListEventBusesOutput{{
			EventBuses: []awseventstypes.EventBus{{
				Arn:              awsv2.String(busARN),
				Name:             awsv2.String("orders"),
				Description:      awsv2.String("orders bus"),
				CreationTime:     awsv2.Time(time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC)),
				LastModifiedTime: awsv2.Time(time.Date(2026, 5, 14, 16, 10, 0, 0, time.UTC)),
				Policy:           awsv2.String(`{"Statement":[{"Effect":"Allow"}]}`),
			}},
		}},
		rulePages: []*awsevents.ListRulesOutput{{
			Rules: []awseventstypes.Rule{{
				Arn:                awsv2.String(ruleARN),
				Name:               awsv2.String("route-orders"),
				EventBusName:       awsv2.String("orders"),
				Description:        awsv2.String("route order events"),
				EventPattern:       awsv2.String(`{"source":["orders"]}`),
				ManagedBy:          awsv2.String("events.amazonaws.com"),
				RoleArn:            awsv2.String("arn:aws:iam::123456789012:role/eventbridge-route-orders"),
				ScheduleExpression: awsv2.String("rate(5 minutes)"),
				State:              awseventstypes.RuleStateEnabled,
			}},
		}},
		describeRuleOutput: &awsevents.DescribeRuleOutput{
			Arn:       awsv2.String(ruleARN),
			CreatedBy: awsv2.String("123456789012"),
		},
		targetPages: []*awsevents.ListTargetsByRuleOutput{{
			Targets: []awseventstypes.Target{{
				Arn: awsv2.String(targetARN),
				Id:  awsv2.String("lambda-target"),
				Input: awsv2.String(`{
				  "customerEmail":"owner@example.com"
				}`),
				InputPath:        awsv2.String("$.detail"),
				InputTransformer: &awseventstypes.InputTransformer{InputTemplate: awsv2.String("<secret>")},
				HttpParameters:   &awseventstypes.HttpParameters{HeaderParameters: map[string]string{"Authorization": "Bearer secret"}},
				DeadLetterConfig: &awseventstypes.DeadLetterConfig{
					Arn: awsv2.String("arn:aws:sqs:us-east-1:123456789012:eventbridge-dlq"),
				},
				RetryPolicy: &awseventstypes.RetryPolicy{
					MaximumEventAgeInSeconds: awsv2.Int32(3600),
					MaximumRetryAttempts:     awsv2.Int32(4),
				},
				RoleArn: awsv2.String("arn:aws:iam::123456789012:role/eventbridge-target"),
			}},
		}},
		tags: []awseventstypes.Tag{{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
	}
	adapter := &Client{
		client:   client,
		boundary: aws.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: aws.ServiceEventBridge},
	}

	buses, err := adapter.ListEventBuses(context.Background())
	if err != nil {
		t.Fatalf("ListEventBuses() error = %v, want nil", err)
	}
	if got, want := len(buses), 1; got != want {
		t.Fatalf("len(buses) = %d, want %d", got, want)
	}
	bus := buses[0]
	if bus.Name != "orders" {
		t.Fatalf("bus.Name = %q, want orders", bus.Name)
	}
	if bus.Tags["Environment"] != "prod" {
		t.Fatalf("bus.Tags = %#v, want Environment=prod", bus.Tags)
	}
	if got, want := len(bus.Rules), 1; got != want {
		t.Fatalf("len(bus.Rules) = %d, want %d", got, want)
	}
	rule := bus.Rules[0]
	if rule.CreatedBy != "123456789012" {
		t.Fatalf("rule.CreatedBy = %q, want 123456789012", rule.CreatedBy)
	}
	if rule.Tags["Environment"] != "prod" {
		t.Fatalf("rule.Tags = %#v, want Environment=prod", rule.Tags)
	}
	if got, want := len(rule.Targets), 1; got != want {
		t.Fatalf("len(rule.Targets) = %d, want %d", got, want)
	}
	target := rule.Targets[0]
	if target.ARN != targetARN {
		t.Fatalf("target.ARN = %q, want %q", target.ARN, targetARN)
	}
	if got, want := target.MaximumEventAgeInSeconds, int32(3600); got != want {
		t.Fatalf("MaximumEventAgeInSeconds = %d, want %d", got, want)
	}
}

type fakeEventBridgeAPI struct {
	eventBusPages      []*awsevents.ListEventBusesOutput
	eventBusCalls      int
	rulePages          []*awsevents.ListRulesOutput
	ruleCalls          int
	describeRuleOutput *awsevents.DescribeRuleOutput
	targetPages        []*awsevents.ListTargetsByRuleOutput
	targetCalls        int
	tags               []awseventstypes.Tag
}

func (f *fakeEventBridgeAPI) ListEventBuses(
	_ context.Context,
	_ *awsevents.ListEventBusesInput,
	_ ...func(*awsevents.Options),
) (*awsevents.ListEventBusesOutput, error) {
	if f.eventBusCalls >= len(f.eventBusPages) {
		return &awsevents.ListEventBusesOutput{}, nil
	}
	page := f.eventBusPages[f.eventBusCalls]
	f.eventBusCalls++
	return page, nil
}

func (f *fakeEventBridgeAPI) ListRules(
	_ context.Context,
	input *awsevents.ListRulesInput,
	_ ...func(*awsevents.Options),
) (*awsevents.ListRulesOutput, error) {
	if awsv2.ToString(input.EventBusName) == "" {
		return nil, nil
	}
	if f.ruleCalls >= len(f.rulePages) {
		return &awsevents.ListRulesOutput{}, nil
	}
	page := f.rulePages[f.ruleCalls]
	f.ruleCalls++
	return page, nil
}

func (f *fakeEventBridgeAPI) DescribeRule(
	_ context.Context,
	input *awsevents.DescribeRuleInput,
	_ ...func(*awsevents.Options),
) (*awsevents.DescribeRuleOutput, error) {
	if awsv2.ToString(input.Name) == "" {
		return nil, nil
	}
	return f.describeRuleOutput, nil
}

func (f *fakeEventBridgeAPI) ListTargetsByRule(
	_ context.Context,
	input *awsevents.ListTargetsByRuleInput,
	_ ...func(*awsevents.Options),
) (*awsevents.ListTargetsByRuleOutput, error) {
	if awsv2.ToString(input.Rule) == "" {
		return nil, nil
	}
	if f.targetCalls >= len(f.targetPages) {
		return &awsevents.ListTargetsByRuleOutput{}, nil
	}
	page := f.targetPages[f.targetCalls]
	f.targetCalls++
	return page, nil
}

func (f *fakeEventBridgeAPI) ListTagsForResource(
	_ context.Context,
	input *awsevents.ListTagsForResourceInput,
	_ ...func(*awsevents.Options),
) (*awsevents.ListTagsForResourceOutput, error) {
	if awsv2.ToString(input.ResourceARN) == "" {
		return nil, nil
	}
	return &awsevents.ListTagsForResourceOutput{Tags: f.tags}, nil
}

var _ apiClient = (*fakeEventBridgeAPI)(nil)
