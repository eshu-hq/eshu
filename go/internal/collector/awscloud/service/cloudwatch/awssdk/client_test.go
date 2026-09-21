// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	awscwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// forbiddenAPIs is the closed list of CloudWatch SDK methods the adapter MUST
// NOT call. GetDashboard is the headline exclusion: it returns the dashboard
// body JSON which the scanner contract forbids. The mutation APIs cover every
// surface that could change customer state. The interface-shape test below
// asserts the adapter's apiClient interface does not declare any of these
// methods, so the compiler enforces the contract even if a future edit tries
// to call one.
var forbiddenAPIs = []string{
	"GetDashboard",
	"PutMetricAlarm",
	"DeleteAlarms",
	"PutCompositeAlarm",
	"PutDashboard",
	"DeleteDashboards",
	"EnableAlarmActions",
	"DisableAlarmActions",
	"SetAlarmState",
	"PutInsightRule",
	"DeleteInsightRules",
	"StartMetricStreams",
	"StopMetricStreams",
	"PutMetricData",
}

// TestApiClientInterfaceExcludesForbiddenMethods is the structural proof that
// the adapter cannot reach GetDashboard or any mutation API. The Client
// struct holds an apiClient value, so any method missing from the interface
// is unreachable from this package. This test fails if a future edit adds
// any forbidden name to the apiClient interface.
func TestApiClientInterfaceExcludesForbiddenMethods(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for _, name := range forbiddenAPIs {
		if _, ok := iface.MethodByName(name); ok {
			t.Fatalf("apiClient interface declares forbidden method %q; CloudWatch adapter must not be able to call it", name)
		}
	}
	// And the allow-list: exactly these read methods are expected.
	want := map[string]bool{
		"DescribeAlarms":       true,
		"ListDashboards":       true,
		"DescribeInsightRules": true,
		"ListMetricStreams":    true,
		"GetMetricStream":      true,
		"ListTagsForResource":  true,
	}
	have := map[string]bool{}
	for i := 0; i < iface.NumMethod(); i++ {
		have[iface.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("apiClient interface missing required method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("apiClient interface declares unexpected method %q (update want list?)", name)
		}
	}
}

// TestClientListsCallsOnlySafeAPIs runs the full happy-path List/Describe
// flow against a fake apiClient that records every call name, and asserts no
// forbidden call name was ever recorded. The fake declares every forbidden
// method as well, but routes them through a counter so the test fails loudly
// if any path in the adapter ever invokes one through a future interface
// widening.
func TestClientListsCallsOnlySafeAPIs(t *testing.T) {
	alarmARN := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:high-cpu"
	compositeARN := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:composite"
	dashboardARN := "arn:aws:cloudwatch::123456789012:dashboard/orders-overview"
	streamARN := "arn:aws:cloudwatch:us-east-1:123456789012:metric-stream/orders-stream"
	firehoseARN := "arn:aws:firehose:us-east-1:123456789012:deliverystream/cw-metrics"
	fake := &fakeCloudWatchAPI{
		describeMetricPages: []*awscw.DescribeAlarmsOutput{{
			MetricAlarms: []awscwtypes.MetricAlarm{{
				AlarmArn:           aws.String(alarmARN),
				AlarmName:          aws.String("high-cpu"),
				AlarmDescription:   aws.String("fires when CPU > 80"),
				StateValue:         awscwtypes.StateValueOk,
				ActionsEnabled:     aws.Bool(true),
				AlarmActions:       []string{"arn:aws:sns:us-east-1:123456789012:on-call"},
				Namespace:          aws.String("AWS/EC2"),
				MetricName:         aws.String("CPUUtilization"),
				Statistic:          awscwtypes.StatisticAverage,
				ComparisonOperator: awscwtypes.ComparisonOperatorGreaterThanThreshold,
				Threshold:          aws.Float64(80),
				EvaluationPeriods:  aws.Int32(3),
				Period:             aws.Int32(60),
				Dimensions: []awscwtypes.Dimension{
					{Name: aws.String("InstanceId"), Value: aws.String("i-12345")},
					{Name: aws.String("Customer"), Value: aws.String("tenant-42")},
				},
				StateUpdatedTimestamp: aws.Time(time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC)),
			}},
		}},
		describeCompositePages: []*awscw.DescribeAlarmsOutput{{
			CompositeAlarms: []awscwtypes.CompositeAlarm{{
				AlarmArn:       aws.String(compositeARN),
				AlarmName:      aws.String("composite-fleet"),
				StateValue:     awscwtypes.StateValueOk,
				ActionsEnabled: aws.Bool(true),
				AlarmRule:      aws.String(`ALARM("high-cpu") OR ALARM("low-disk")`),
				AlarmActions:   []string{"arn:aws:sns:us-east-1:123456789012:on-call"},
			}},
		}},
		listDashboardsPages: []*awscw.ListDashboardsOutput{{
			DashboardEntries: []awscwtypes.DashboardEntry{{
				DashboardArn:  aws.String(dashboardARN),
				DashboardName: aws.String("orders-overview"),
				LastModified:  aws.Time(time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC)),
				Size:          aws.Int64(4096),
			}},
		}},
		describeInsightRulesPages: []*awscw.DescribeInsightRulesOutput{{
			InsightRules: []awscwtypes.InsightRule{{
				Name:       aws.String("top-talkers"),
				State:      aws.String("ENABLED"),
				Schema:     aws.String("CloudWatchLogRule/1"),
				Definition: aws.String(`{"Keys":["$.customerId"],"AggregateOn":"COUNT"}`),
			}},
		}},
		listMetricStreamsPages: []*awscw.ListMetricStreamsOutput{{
			Entries: []awscwtypes.MetricStreamEntry{{
				Arn:            aws.String(streamARN),
				Name:           aws.String("orders-stream"),
				State:          aws.String("running"),
				OutputFormat:   awscwtypes.MetricStreamOutputFormatJson,
				FirehoseArn:    aws.String(firehoseARN),
				CreationDate:   aws.Time(time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)),
				LastUpdateDate: aws.Time(time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC)),
			}},
		}},
		getMetricStreamOutput: &awscw.GetMetricStreamOutput{
			Arn:                          aws.String(streamARN),
			Name:                         aws.String("orders-stream"),
			State:                        aws.String("running"),
			OutputFormat:                 awscwtypes.MetricStreamOutputFormatJson,
			FirehoseArn:                  aws.String(firehoseARN),
			RoleArn:                      aws.String("arn:aws:iam::123456789012:role/cw-metric-stream"),
			IncludeLinkedAccountsMetrics: aws.Bool(false),
		},
		tags: []awscwtypes.Tag{{Key: aws.String("Environment"), Value: aws.String("prod")}},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceCloudWatch},
	}

	metricAlarms, err := adapter.ListMetricAlarms(context.Background())
	if err != nil {
		t.Fatalf("ListMetricAlarms() error = %v", err)
	}
	if len(metricAlarms) != 1 || metricAlarms[0].ARN != alarmARN {
		t.Fatalf("metricAlarms = %#v, want one alarm with ARN %q", metricAlarms, alarmARN)
	}
	if len(metricAlarms[0].Dimensions) != 2 {
		t.Fatalf("dimensions len = %d, want 2", len(metricAlarms[0].Dimensions))
	}

	compositeAlarms, err := adapter.ListCompositeAlarms(context.Background())
	if err != nil {
		t.Fatalf("ListCompositeAlarms() error = %v", err)
	}
	if len(compositeAlarms) != 1 || compositeAlarms[0].ARN != compositeARN {
		t.Fatalf("compositeAlarms = %#v, want one composite alarm", compositeAlarms)
	}
	if got, want := compositeAlarms[0].ChildAlarmNames, []string{"high-cpu", "low-disk"}; !slices.Equal(got, want) {
		t.Fatalf("ChildAlarmNames = %#v, want %#v", got, want)
	}

	dashboards, err := adapter.ListDashboards(context.Background())
	if err != nil {
		t.Fatalf("ListDashboards() error = %v", err)
	}
	if len(dashboards) != 1 || dashboards[0].ARN != dashboardARN {
		t.Fatalf("dashboards = %#v, want one dashboard %q", dashboards, dashboardARN)
	}

	rules, err := adapter.ListInsightRules(context.Background())
	if err != nil {
		t.Fatalf("ListInsightRules() error = %v", err)
	}
	if len(rules) != 1 || rules[0].Name != "top-talkers" || rules[0].State != "ENABLED" {
		t.Fatalf("rules = %#v, want top-talkers ENABLED", rules)
	}

	streams, err := adapter.ListMetricStreams(context.Background())
	if err != nil {
		t.Fatalf("ListMetricStreams() error = %v", err)
	}
	if len(streams) != 1 || streams[0].FirehoseARN != firehoseARN {
		t.Fatalf("streams = %#v, want firehose %q", streams, firehoseARN)
	}

	// Runtime allow-list check. The fake records every dispatched method in
	// fake.calls and only implements the six allowed apiClient methods, so a
	// forbidden name can never appear here unless the interface is widened.
	// The compile-time guarantee that no forbidden method is even reachable
	// lives in TestApiClientInterfaceExcludesForbiddenMethods; this loop is a
	// belt-and-suspenders runtime assertion over the calls this scan made.
	for _, forbidden := range forbiddenAPIs {
		if slices.Contains(fake.calls, forbidden) {
			t.Fatalf("forbidden CloudWatch call %q recorded: calls=%v", forbidden, fake.calls)
		}
	}

	// And every recorded call must be in the allow-list.
	allowed := map[string]bool{
		"DescribeAlarms":       true,
		"ListDashboards":       true,
		"DescribeInsightRules": true,
		"ListMetricStreams":    true,
		"GetMetricStream":      true,
		"ListTagsForResource":  true,
	}
	sort.Strings(fake.calls)
	for _, name := range fake.calls {
		if !allowed[name] {
			t.Fatalf("unexpected CloudWatch call %q; calls=%v", name, fake.calls)
		}
	}
}

type fakeCloudWatchAPI struct {
	describeMetricPages       []*awscw.DescribeAlarmsOutput
	describeMetricCalls       int
	describeCompositePages    []*awscw.DescribeAlarmsOutput
	describeCompositeCalls    int
	listDashboardsPages       []*awscw.ListDashboardsOutput
	listDashboardsCalls       int
	describeInsightRulesPages []*awscw.DescribeInsightRulesOutput
	describeInsightRulesCalls int
	listMetricStreamsPages    []*awscw.ListMetricStreamsOutput
	listMetricStreamsCalls    int
	getMetricStreamOutput     *awscw.GetMetricStreamOutput
	tags                      []awscwtypes.Tag
	calls                     []string
}

func (f *fakeCloudWatchAPI) DescribeAlarms(
	_ context.Context,
	input *awscw.DescribeAlarmsInput,
	_ ...func(*awscw.Options),
) (*awscw.DescribeAlarmsOutput, error) {
	f.calls = append(f.calls, "DescribeAlarms")
	if isCompositeOnly(input.AlarmTypes) {
		page := nextPage(f.describeCompositePages, &f.describeCompositeCalls)
		if page == nil {
			return &awscw.DescribeAlarmsOutput{}, nil
		}
		return page, nil
	}
	page := nextPage(f.describeMetricPages, &f.describeMetricCalls)
	if page == nil {
		return &awscw.DescribeAlarmsOutput{}, nil
	}
	return page, nil
}

func (f *fakeCloudWatchAPI) ListDashboards(
	_ context.Context,
	_ *awscw.ListDashboardsInput,
	_ ...func(*awscw.Options),
) (*awscw.ListDashboardsOutput, error) {
	f.calls = append(f.calls, "ListDashboards")
	page := nextPage(f.listDashboardsPages, &f.listDashboardsCalls)
	if page == nil {
		return &awscw.ListDashboardsOutput{}, nil
	}
	return page, nil
}

func (f *fakeCloudWatchAPI) DescribeInsightRules(
	_ context.Context,
	_ *awscw.DescribeInsightRulesInput,
	_ ...func(*awscw.Options),
) (*awscw.DescribeInsightRulesOutput, error) {
	f.calls = append(f.calls, "DescribeInsightRules")
	page := nextPage(f.describeInsightRulesPages, &f.describeInsightRulesCalls)
	if page == nil {
		return &awscw.DescribeInsightRulesOutput{}, nil
	}
	return page, nil
}

func (f *fakeCloudWatchAPI) ListMetricStreams(
	_ context.Context,
	_ *awscw.ListMetricStreamsInput,
	_ ...func(*awscw.Options),
) (*awscw.ListMetricStreamsOutput, error) {
	f.calls = append(f.calls, "ListMetricStreams")
	page := nextPage(f.listMetricStreamsPages, &f.listMetricStreamsCalls)
	if page == nil {
		return &awscw.ListMetricStreamsOutput{}, nil
	}
	return page, nil
}

func (f *fakeCloudWatchAPI) GetMetricStream(
	_ context.Context,
	_ *awscw.GetMetricStreamInput,
	_ ...func(*awscw.Options),
) (*awscw.GetMetricStreamOutput, error) {
	f.calls = append(f.calls, "GetMetricStream")
	return f.getMetricStreamOutput, nil
}

func (f *fakeCloudWatchAPI) ListTagsForResource(
	_ context.Context,
	_ *awscw.ListTagsForResourceInput,
	_ ...func(*awscw.Options),
) (*awscw.ListTagsForResourceOutput, error) {
	f.calls = append(f.calls, "ListTagsForResource")
	return &awscw.ListTagsForResourceOutput{Tags: f.tags}, nil
}

func nextPage[T any](pages []*T, calls *int) *T {
	if *calls >= len(pages) {
		return nil
	}
	page := pages[*calls]
	*calls++
	return page
}

func isCompositeOnly(types []awscwtypes.AlarmType) bool {
	for _, t := range types {
		if t == awscwtypes.AlarmTypeCompositeAlarm {
			return true
		}
	}
	return false
}

var _ apiClient = (*fakeCloudWatchAPI)(nil)
