// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudwatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestScannerEmitsAllFiveResourceTypesWithRelationships(t *testing.T) {
	alarmARN := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:high-cpu"
	childAlarmARN := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:child-low-disk"
	compositeAlarmARN := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:composite-fleet"
	dashboardARN := "arn:aws:cloudwatch::123456789012:dashboard/orders-overview"
	streamARN := "arn:aws:cloudwatch:us-east-1:123456789012:metric-stream/orders-stream"
	snsTopicARN := "arn:aws:sns:us-east-1:123456789012:on-call"
	firehoseARN := "arn:aws:firehose:us-east-1:123456789012:deliverystream/cw-metrics"
	threshold := 80.0
	client := fakeClient{
		metricAlarms: []MetricAlarm{{
			ARN:                                alarmARN,
			Name:                               "high-cpu",
			Description:                        "fires when CPU > 80",
			State:                              "OK",
			StateReason:                        "within threshold",
			ActionsEnabled:                     true,
			AlarmActions:                       []string{snsTopicARN},
			OKActions:                          []string{snsTopicARN},
			InsufficientDataActions:            []string{snsTopicARN},
			Namespace:                          "AWS/EC2",
			MetricName:                         "CPUUtilization",
			Statistic:                          "Average",
			ComparisonOperator:                 "GreaterThanThreshold",
			Threshold:                          &threshold,
			EvaluationPeriods:                  3,
			DatapointsToAlarm:                  3,
			Period:                             60,
			TreatMissingData:                   "missing",
			Dimensions:                         []MetricDimension{{Name: "InstanceId", Value: "i-12345"}, {Name: "Customer", Value: "tenant-42"}},
			StateUpdatedTimestamp:              time.Date(2026, 5, 14, 16, 0, 0, 0, time.UTC),
			AlarmConfigurationUpdatedTimestamp: time.Date(2026, 5, 14, 15, 0, 0, 0, time.UTC),
			Tags:                               map[string]string{"Environment": "prod"},
		}, {
			ARN:                "arn:aws:cloudwatch:us-east-1:123456789012:alarm:child-low-disk",
			Name:               "child-low-disk",
			Namespace:          "AWS/EC2",
			MetricName:         "DiskSpaceUtilization",
			ComparisonOperator: "LessThanThreshold",
			Dimensions:         []MetricDimension{{Name: "InstanceId", Value: "i-12345"}},
		}},
		compositeAlarms: []CompositeAlarm{{
			ARN:             compositeAlarmARN,
			Name:            "composite-fleet",
			Description:     "fleet posture composite",
			State:           "OK",
			ActionsEnabled:  true,
			AlarmRule:       `ALARM("high-cpu") OR ALARM("child-low-disk")`,
			AlarmActions:    []string{snsTopicARN},
			ChildAlarmNames: []string{"high-cpu", "child-low-disk"},
			Tags:            map[string]string{"Owner": "platform"},
		}},
		dashboards: []Dashboard{{
			ARN:          dashboardARN,
			Name:         "orders-overview",
			LastModified: time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
			SizeInBytes:  4096,
		}},
		insightRules: []InsightRule{{
			Name:   "top-talkers",
			State:  "ENABLED",
			Schema: "CloudWatchLogRule/1",
		}},
		metricStreams: []MetricStream{{
			ARN:                  streamARN,
			Name:                 "orders-stream",
			State:                "running",
			OutputFormat:         "json",
			FirehoseARN:          firehoseARN,
			RoleARN:              "arn:aws:iam::123456789012:role/cw-metric-stream",
			CreationDate:         time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			LastUpdateDate:       time.Date(2026, 5, 14, 13, 0, 0, 0, time.UTC),
			IncludeLinkedAccount: false,
			Tags:                 map[string]string{"Stream": "primary"},
		}},
		childAlarmARN: childAlarmARN,
	}
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// All five resource types must appear.
	for _, kind := range []string{
		awscloud.ResourceTypeCloudWatchAlarm,
		awscloud.ResourceTypeCloudWatchCompositeAlarm,
		awscloud.ResourceTypeCloudWatchDashboard,
		awscloud.ResourceTypeCloudWatchInsightRule,
		awscloud.ResourceTypeCloudWatchMetricStream,
	} {
		if envelope, ok := firstResource(envelopes, kind); !ok {
			t.Fatalf("missing resource_type %q in envelopes", kind)
		} else if envelope.Payload["resource_type"] != kind {
			t.Fatalf("resource_type = %#v, want %q", envelope.Payload["resource_type"], kind)
		}
	}

	// Dashboard must not carry a body field.
	dashboard, _ := firstResource(envelopes, awscloud.ResourceTypeCloudWatchDashboard)
	dashAttrs := attributesOf(t, dashboard)
	for _, forbidden := range []string{"body", "dashboard_body", "widgets", "definition"} {
		if _, exists := dashAttrs[forbidden]; exists {
			t.Fatalf("%s attribute persisted on dashboard; scanner must not store dashboard body JSON", forbidden)
		}
	}
	if got := dashAttrs["last_modified"]; got == nil {
		t.Fatalf("dashboard last_modified missing; want UTC timestamp")
	}

	// Insight rule must not carry a definition field.
	rule, _ := firstResource(envelopes, awscloud.ResourceTypeCloudWatchInsightRule)
	ruleAttrs := attributesOf(t, rule)
	for _, forbidden := range []string{"definition", "rule_definition", "body"} {
		if _, exists := ruleAttrs[forbidden]; exists {
			t.Fatalf("%s attribute persisted on insight rule; scanner must not store rule definition", forbidden)
		}
	}
	if got, want := rule.Payload["state"], "ENABLED"; got != want {
		t.Fatalf("insight rule state = %#v, want %q", got, want)
	}

	// Metric alarm: thresholds, action ARNs preserved.
	metricAlarm, _ := firstResource(envelopes, awscloud.ResourceTypeCloudWatchAlarm)
	alarmAttrs := attributesOf(t, metricAlarm)
	if got, want := alarmAttrs["namespace"], "AWS/EC2"; got != want {
		t.Fatalf("alarm namespace = %#v, want %q", got, want)
	}
	if got, want := alarmAttrs["threshold"], 80.0; got != want {
		t.Fatalf("alarm threshold = %#v, want %v", got, want)
	}

	// Composite alarm relationship for each child.
	compositeChildRels := relationshipsOfType(envelopes, awscloud.RelationshipCloudWatchCompositeAlarmHasChildAlarm)
	if got, want := len(compositeChildRels), 2; got != want {
		t.Fatalf("composite child alarm relationships = %d, want %d", got, want)
	}

	// SNS notify relationship for the alarm — three actions on the same topic dedupe to one edge.
	snsRels := relationshipsOfType(envelopes, awscloud.RelationshipCloudWatchAlarmNotifiesSNSTopic)
	if got, want := len(snsRels), 2; got != want {
		// 1 for the metric alarm (deduped) + 1 for the composite alarm = 2
		t.Fatalf("alarm notifies SNS topic relationships = %d, want %d", got, want)
	}
	for _, edge := range snsRels {
		if got, want := edge.Payload["target_arn"], snsTopicARN; got != want {
			t.Fatalf("SNS edge target_arn = %#v, want %q", got, want)
		}
	}

	// Metric stream relationship to Firehose.
	firehoseRels := relationshipsOfType(envelopes, awscloud.RelationshipCloudWatchMetricStreamDeliversToFirehose)
	if got, want := len(firehoseRels), 1; got != want {
		t.Fatalf("metric stream firehose relationships = %d, want %d", got, want)
	}
	if got, want := firehoseRels[0].Payload["target_arn"], firehoseARN; got != want {
		t.Fatalf("firehose target_arn = %#v, want %q", got, want)
	}
	// The target_type must match the resource_type the kinesis scanner publishes
	// for Firehose delivery streams, or the edge dangles and never joins the
	// Firehose node. Regression for the #804 graph-join defect class.
	if got, want := firehoseRels[0].Payload["target_type"], awscloud.ResourceTypeKinesisFirehoseDeliveryStream; got != want {
		t.Fatalf("firehose target_type = %#v, want %q (the type kinesis publishes for Firehose)", got, want)
	}

	// Alarm observes metric relationship: dimensions present, customer-tag-named one redacted.
	metricRels := relationshipsOfType(envelopes, awscloud.RelationshipCloudWatchAlarmObservesMetric)
	if got, want := len(metricRels), 2; got != want { // one per metric alarm
		t.Fatalf("alarm observes metric relationships = %d, want %d", got, want)
	}
	firstAlarmMetric := relationshipForSource(metricRels, alarmARN)
	if firstAlarmMetric == nil {
		t.Fatalf("missing metric observation for alarm %q", alarmARN)
	}
	dims, ok := firstAlarmMetric.Payload["attributes"].(map[string]any)["dimensions"].([]any)
	if !ok {
		t.Fatalf("dimensions = %#v, want []any", firstAlarmMetric.Payload["attributes"])
	}
	if len(dims) != 2 {
		t.Fatalf("dimensions len = %d, want 2", len(dims))
	}
	// Find the Customer dimension: its value must be a redaction marker map.
	var customerEntry map[string]any
	for _, raw := range dims {
		entry, _ := raw.(map[string]any)
		if entry["name"] == "Customer" {
			customerEntry = entry
		}
	}
	if customerEntry == nil {
		t.Fatalf("missing Customer dimension in %#v", dims)
	}
	value, _ := customerEntry["value"].(map[string]any)
	marker, _ := value["marker"].(string)
	if !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("customer dimension value not redacted: %#v", customerEntry)
	}
	if strings.Contains(marker, "tenant-42") {
		t.Fatalf("redaction marker leaks raw value: %q", marker)
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing redaction key")
	}
	if !strings.Contains(err.Error(), "redaction key") {
		t.Fatalf("Scan() error = %q, want mention of redaction key", err)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	key, err := redact.NewKey([]byte("aws-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceCloudWatchLogs
	_, err = (Scanner{Client: fakeClient{}, RedactionKey: key}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	key, _ := redact.NewKey([]byte("k"))
	_, err := (Scanner{RedactionKey: key}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing client")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCloudWatch,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:cloudwatch:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	metricAlarms    []MetricAlarm
	compositeAlarms []CompositeAlarm
	dashboards      []Dashboard
	insightRules    []InsightRule
	metricStreams   []MetricStream
	childAlarmARN   string
}

func (c fakeClient) ListMetricAlarms(context.Context) ([]MetricAlarm, error) {
	return c.metricAlarms, nil
}

func (c fakeClient) ListCompositeAlarms(context.Context) ([]CompositeAlarm, error) {
	return c.compositeAlarms, nil
}

func (c fakeClient) ListDashboards(context.Context) ([]Dashboard, error) {
	return c.dashboards, nil
}

func (c fakeClient) ListInsightRules(context.Context) ([]InsightRule, error) {
	return c.insightRules, nil
}

func (c fakeClient) ListMetricStreams(context.Context) ([]MetricStream, error) {
	return c.metricStreams, nil
}

func firstResource(envelopes []facts.Envelope, resourceType string) (facts.Envelope, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope, true
		}
	}
	return facts.Envelope{}, false
}

func relationshipsOfType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			out = append(out, envelope)
		}
	}
	return out
}

func relationshipForSource(envelopes []facts.Envelope, sourceARN string) *facts.Envelope {
	for i := range envelopes {
		if got, _ := envelopes[i].Payload["source_arn"].(string); got == sourceARN {
			return &envelopes[i]
		}
	}
	return nil
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
