package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsevents "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	awseventstypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientListEventBusesReadsSafeMetadataRulesTargetsAndTags(t *testing.T) {
	busARN := "arn:aws:events:us-east-1:123456789012:event-bus/orders"
	ruleARN := "arn:aws:events:us-east-1:123456789012:rule/orders/route-orders"
	targetARN := "arn:aws:lambda:us-east-1:123456789012:function:order-router"
	client := &fakeEventBridgeAPI{
		eventBusPages: []*awsevents.ListEventBusesOutput{{
			EventBuses: []awseventstypes.EventBus{{
				Arn:              aws.String(busARN),
				Name:             aws.String("orders"),
				Description:      aws.String("orders bus"),
				CreationTime:     aws.Time(time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC)),
				LastModifiedTime: aws.Time(time.Date(2026, 5, 14, 16, 10, 0, 0, time.UTC)),
				Policy:           aws.String(`{"Statement":[{"Effect":"Allow"}]}`),
			}},
		}},
		rulePages: []*awsevents.ListRulesOutput{{
			Rules: []awseventstypes.Rule{{
				Arn:                aws.String(ruleARN),
				Name:               aws.String("route-orders"),
				EventBusName:       aws.String("orders"),
				Description:        aws.String("route order events"),
				EventPattern:       aws.String(`{"source":["orders"]}`),
				ManagedBy:          aws.String("events.amazonaws.com"),
				RoleArn:            aws.String("arn:aws:iam::123456789012:role/eventbridge-route-orders"),
				ScheduleExpression: aws.String("rate(5 minutes)"),
				State:              awseventstypes.RuleStateEnabled,
			}},
		}},
		describeRuleOutput: &awsevents.DescribeRuleOutput{
			Arn:       aws.String(ruleARN),
			CreatedBy: aws.String("123456789012"),
		},
		targetPages: []*awsevents.ListTargetsByRuleOutput{{
			Targets: []awseventstypes.Target{{
				Arn: aws.String(targetARN),
				Id:  aws.String("lambda-target"),
				Input: aws.String(`{
				  "customerEmail":"owner@example.com"
				}`),
				InputPath:        aws.String("$.detail"),
				InputTransformer: &awseventstypes.InputTransformer{InputTemplate: aws.String("<secret>")},
				HttpParameters:   &awseventstypes.HttpParameters{HeaderParameters: map[string]string{"Authorization": "Bearer secret"}},
				DeadLetterConfig: &awseventstypes.DeadLetterConfig{
					Arn: aws.String("arn:aws:sqs:us-east-1:123456789012:eventbridge-dlq"),
				},
				RetryPolicy: &awseventstypes.RetryPolicy{
					MaximumEventAgeInSeconds: aws.Int32(3600),
					MaximumRetryAttempts:     aws.Int32(4),
				},
				RoleArn: aws.String("arn:aws:iam::123456789012:role/eventbridge-target"),
			}},
		}},
		tags: []awseventstypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
	}
	adapter := &Client{
		client:   client,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceEventBridge},
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
	if aws.ToString(input.EventBusName) == "" {
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
	if aws.ToString(input.Name) == "" {
		return nil, nil
	}
	return f.describeRuleOutput, nil
}

func (f *fakeEventBridgeAPI) ListTargetsByRule(
	_ context.Context,
	input *awsevents.ListTargetsByRuleInput,
	_ ...func(*awsevents.Options),
) (*awsevents.ListTargetsByRuleOutput, error) {
	if aws.ToString(input.Rule) == "" {
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
	if aws.ToString(input.ResourceARN) == "" {
		return nil, nil
	}
	return &awsevents.ListTagsForResourceOutput{Tags: f.tags}, nil
}

var _ apiClient = (*fakeEventBridgeAPI)(nil)
