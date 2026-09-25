// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudwatch

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// alarmSNSRelationships emits one relationship per distinct SNS topic ARN
// referenced by an alarm's alarm/ok/insufficient-data actions. Non-SNS
// actions (Lambda, SSM, autoscaling, etc.) are ignored here — only the SNS
// notification fan-out is tracked.
func alarmSNSRelationships(
	boundary aws.Boundary,
	alarmARN string,
	alarmName string,
	alarmActions []string,
	okActions []string,
	insufficientDataActions []string,
) []aws.RelationshipObservation {
	source := firstNonEmpty(strings.TrimSpace(alarmARN), strings.TrimSpace(alarmName))
	if source == "" {
		return nil
	}
	seen := map[string]string{}
	add := func(arn, category string) {
		trimmed := strings.TrimSpace(arn)
		if !strings.Contains(trimmed, ":sns:") {
			return
		}
		if existing, ok := seen[trimmed]; ok {
			if !strings.Contains(existing, category) {
				seen[trimmed] = existing + "," + category
			}
			return
		}
		seen[trimmed] = category
	}
	for _, arn := range alarmActions {
		add(arn, "alarm")
	}
	for _, arn := range okActions {
		add(arn, "ok")
	}
	for _, arn := range insufficientDataActions {
		add(arn, "insufficient_data")
	}
	relationships := make([]aws.RelationshipObservation, 0, len(seen))
	for topicARN, categories := range seen {
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipCloudWatchAlarmNotifiesSNSTopic,
			SourceResourceID: source,
			SourceARN:        strings.TrimSpace(alarmARN),
			TargetResourceID: topicARN,
			TargetARN:        topicARN,
			TargetType:       aws.ResourceTypeSNSTopic,
			Attributes: map[string]any{
				"alarm_name":     strings.TrimSpace(alarmName),
				"action_classes": categories,
			},
			SourceRecordID: source + "->" + topicARN,
		})
	}
	return relationships
}

// compositeChildRelationships emits one relationship per child alarm name
// referenced by a composite alarm's AlarmRule. Child alarm targets carry the
// alarm name only because the composite alarm rule names children by name,
// not ARN; the reducer materializes the ARN later through correlation.
func compositeChildRelationships(
	boundary aws.Boundary,
	alarm CompositeAlarm,
) []aws.RelationshipObservation {
	source := firstNonEmpty(strings.TrimSpace(alarm.ARN), strings.TrimSpace(alarm.Name))
	if source == "" {
		return nil
	}
	seen := map[string]struct{}{}
	relationships := make([]aws.RelationshipObservation, 0, len(alarm.ChildAlarmNames))
	for _, child := range alarm.ChildAlarmNames {
		trimmed := strings.TrimSpace(child)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		relationships = append(relationships, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipCloudWatchCompositeAlarmHasChildAlarm,
			SourceResourceID: source,
			SourceARN:        strings.TrimSpace(alarm.ARN),
			TargetResourceID: trimmed,
			TargetType:       aws.ResourceTypeCloudWatchAlarm,
			Attributes: map[string]any{
				"alarm_rule": strings.TrimSpace(alarm.AlarmRule),
				"child_name": trimmed,
			},
			SourceRecordID: source + "->" + trimmed,
		})
	}
	return relationships
}

// metricStreamFirehoseRelationship emits a metric-stream-to-Firehose edge when
// the stream reports a Firehose ARN. Other destinations are skipped: metric
// streams are documented to deliver only to Firehose today, but if AWS adds a
// new destination type the scanner stays silent rather than misroute.
func metricStreamFirehoseRelationship(
	boundary aws.Boundary,
	stream MetricStream,
) (aws.RelationshipObservation, bool) {
	source := firstNonEmpty(strings.TrimSpace(stream.ARN), strings.TrimSpace(stream.Name))
	firehoseARN := strings.TrimSpace(stream.FirehoseARN)
	if source == "" || !strings.Contains(firehoseARN, ":firehose:") {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCloudWatchMetricStreamDeliversToFirehose,
		SourceResourceID: source,
		SourceARN:        strings.TrimSpace(stream.ARN),
		TargetResourceID: firehoseARN,
		TargetARN:        firehoseARN,
		TargetType:       aws.ResourceTypeKinesisFirehoseDeliveryStream,
		Attributes: map[string]any{
			"output_format": strings.TrimSpace(stream.OutputFormat),
			"stream_name":   strings.TrimSpace(stream.Name),
		},
		SourceRecordID: source + "->" + firehoseARN,
	}, true
}

// alarmMetricRelationship emits a single edge from the alarm to the metric
// identity (namespace + metric name + dimension summary). Dimensions are
// reported with their original names; values whose dimension name looks like
// a customer tag are routed through the shared redact library so customer
// identifiers do not land in the fact stream.
func alarmMetricRelationship(
	boundary aws.Boundary,
	alarm MetricAlarm,
	key redact.Key,
) (aws.RelationshipObservation, bool) {
	source := firstNonEmpty(strings.TrimSpace(alarm.ARN), strings.TrimSpace(alarm.Name))
	namespace := strings.TrimSpace(alarm.Namespace)
	metricName := strings.TrimSpace(alarm.MetricName)
	if source == "" || (namespace == "" && metricName == "") {
		return aws.RelationshipObservation{}, false
	}
	metricID := joinNonEmpty("/", namespace, metricName)
	if metricID == "" {
		metricID = firstNonEmpty(namespace, metricName)
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipCloudWatchAlarmObservesMetric,
		SourceResourceID: source,
		SourceARN:        strings.TrimSpace(alarm.ARN),
		TargetResourceID: metricID,
		TargetType:       "aws_cloudwatch_metric",
		Attributes: map[string]any{
			"namespace":   namespace,
			"metric_name": metricName,
			"statistic":   strings.TrimSpace(alarm.Statistic),
			"period":      alarm.Period,
			"unit":        strings.TrimSpace(alarm.Unit),
			"dimensions":  dimensionSummary(alarm.Dimensions, key),
		},
		SourceRecordID: source + "->" + metricID,
	}, true
}
