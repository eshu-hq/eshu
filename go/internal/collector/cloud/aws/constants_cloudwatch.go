// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceCloudWatch identifies the regional Amazon CloudWatch metadata-only
	// scan slice covering metric alarms, composite alarms, dashboard
	// identities, Contributor Insights rule identities, and metric streams.
	// Dashboard body JSON and Contributor Insights rule definitions stay
	// outside the scan slice; the CloudWatch Logs log group surface lives in
	// ServiceCloudWatchLogs.
	ServiceCloudWatch = "cloudwatch"
)

const (
	// ResourceTypeCloudWatchAlarm identifies a CloudWatch metric alarm metadata
	// resource.
	ResourceTypeCloudWatchAlarm = "aws_cloudwatch_alarm"
	// ResourceTypeCloudWatchCompositeAlarm identifies a CloudWatch composite
	// alarm metadata resource.
	ResourceTypeCloudWatchCompositeAlarm = "aws_cloudwatch_composite_alarm"
	// ResourceTypeCloudWatchDashboard identifies a CloudWatch dashboard
	// metadata resource. The scanner persists the dashboard name and
	// last-modified timestamp only; the dashboard body JSON is never
	// persisted because widget bodies often reveal internal infrastructure
	// naming and KPI thresholds.
	ResourceTypeCloudWatchDashboard = "aws_cloudwatch_dashboard"
	// ResourceTypeCloudWatchInsightRule identifies a CloudWatch Contributor
	// Insights rule metadata resource. The scanner persists the rule name
	// and state only; the rule definition is never persisted because the
	// SQL-like grammar may encode customer query patterns.
	ResourceTypeCloudWatchInsightRule = "aws_cloudwatch_insight_rule"
	// ResourceTypeCloudWatchMetricStream identifies a CloudWatch metric stream
	// metadata resource.
	ResourceTypeCloudWatchMetricStream = "aws_cloudwatch_metric_stream"
)

const (
	// RelationshipCloudWatchAlarmNotifiesSNSTopic records an SNS topic listed
	// in a CloudWatch alarm's alarm, ok, or insufficient-data actions.
	RelationshipCloudWatchAlarmNotifiesSNSTopic = "cloudwatch_alarm_notifies_sns_topic"
	// RelationshipCloudWatchCompositeAlarmHasChildAlarm records a child alarm
	// referenced by a CloudWatch composite alarm's AlarmRule expression.
	RelationshipCloudWatchCompositeAlarmHasChildAlarm = "cloudwatch_composite_alarm_has_child_alarm"
	// RelationshipCloudWatchMetricStreamDeliversToFirehose records a metric
	// stream's reported Kinesis Data Firehose delivery destination.
	RelationshipCloudWatchMetricStreamDeliversToFirehose = "cloudwatch_metric_stream_delivers_to_firehose"
	// RelationshipCloudWatchAlarmObservesMetric records a metric identity
	// (namespace, metric name, and dimension summary) observed by a CloudWatch
	// metric alarm. Customer-tag-named dimension values route through the
	// shared redact library before persistence.
	RelationshipCloudWatchAlarmObservesMetric = "cloudwatch_alarm_observes_metric"
)
