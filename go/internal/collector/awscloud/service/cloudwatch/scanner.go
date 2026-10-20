// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudwatch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// Scanner emits Amazon CloudWatch metadata facts for one claimed account and
// region. It is metadata-only: dashboard body JSON and Contributor Insights
// rule definitions are never persisted, and no mutation API is ever called.
type Scanner struct {
	Client       Client
	RedactionKey redact.Key
}

// Scan observes CloudWatch metric alarms, composite alarms, dashboards,
// Contributor Insights rules, and metric streams through the configured
// client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("cloudwatch scanner client is required")
	}
	if s.RedactionKey.IsZero() {
		return nil, fmt.Errorf("cloudwatch scanner redaction key is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceCloudWatch:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceCloudWatch
	default:
		return nil, fmt.Errorf("cloudwatch scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	metricAlarms, err := s.Client.ListMetricAlarms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudWatch metric alarms: %w", err)
	}
	for _, alarm := range metricAlarms {
		alarmEnvelopes, err := s.metricAlarmEnvelopes(boundary, alarm)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, alarmEnvelopes...)
	}

	compositeAlarms, err := s.Client.ListCompositeAlarms(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudWatch composite alarms: %w", err)
	}
	for _, alarm := range compositeAlarms {
		alarmEnvelopes, err := compositeAlarmEnvelopes(boundary, alarm)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, alarmEnvelopes...)
	}

	dashboards, err := s.Client.ListDashboards(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudWatch dashboards: %w", err)
	}
	for _, dashboard := range dashboards {
		envelope, err := awscloud.NewResourceEnvelope(dashboardObservation(boundary, dashboard))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	rules, err := s.Client.ListInsightRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudWatch insight rules: %w", err)
	}
	for _, rule := range rules {
		envelope, err := awscloud.NewResourceEnvelope(insightRuleObservation(boundary, rule))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	streams, err := s.Client.ListMetricStreams(ctx)
	if err != nil {
		return nil, fmt.Errorf("list CloudWatch metric streams: %w", err)
	}
	for _, stream := range streams {
		streamEnvelopes, err := metricStreamEnvelopes(boundary, stream)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, streamEnvelopes...)
	}

	return envelopes, nil
}

func (s Scanner) metricAlarmEnvelopes(
	boundary awscloud.Boundary,
	alarm MetricAlarm,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(metricAlarmObservation(boundary, alarm))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range alarmSNSRelationships(
		boundary,
		alarm.ARN,
		alarm.Name,
		alarm.AlarmActions,
		alarm.OKActions,
		alarm.InsufficientDataActions,
	) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	metric, ok := alarmMetricRelationship(boundary, alarm, s.RedactionKey)
	if ok {
		envelope, err := awscloud.NewRelationshipEnvelope(metric)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func metricAlarmObservation(boundary awscloud.Boundary, alarm MetricAlarm) awscloud.ResourceObservation {
	alarmARN := strings.TrimSpace(alarm.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          alarmARN,
		ResourceID:   firstNonEmpty(alarmARN, alarm.Name),
		ResourceType: awscloud.ResourceTypeCloudWatchAlarm,
		Name:         strings.TrimSpace(alarm.Name),
		State:        strings.TrimSpace(alarm.State),
		Tags:         cloneStringMap(alarm.Tags),
		Attributes: map[string]any{
			"description":                           strings.TrimSpace(alarm.Description),
			"state_reason":                          strings.TrimSpace(alarm.StateReason),
			"actions_enabled":                       alarm.ActionsEnabled,
			"alarm_actions":                         cloneStrings(alarm.AlarmActions),
			"ok_actions":                            cloneStrings(alarm.OKActions),
			"insufficient_data_actions":             cloneStrings(alarm.InsufficientDataActions),
			"namespace":                             strings.TrimSpace(alarm.Namespace),
			"metric_name":                           strings.TrimSpace(alarm.MetricName),
			"statistic":                             strings.TrimSpace(alarm.Statistic),
			"extended_statistic":                    strings.TrimSpace(alarm.ExtendedStatistic),
			"comparison_operator":                   strings.TrimSpace(alarm.ComparisonOperator),
			"threshold":                             float64OrNil(alarm.Threshold),
			"evaluation_periods":                    alarm.EvaluationPeriods,
			"datapoints_to_alarm":                   alarm.DatapointsToAlarm,
			"period":                                alarm.Period,
			"treat_missing_data":                    strings.TrimSpace(alarm.TreatMissingData),
			"evaluate_low_sample_count_percentile":  strings.TrimSpace(alarm.EvaluateLowSampleCountPercentile),
			"unit":                                  strings.TrimSpace(alarm.Unit),
			"state_updated_timestamp":               timeOrNil(alarm.StateUpdatedTimestamp),
			"alarm_configuration_updated_timestamp": timeOrNil(alarm.AlarmConfigurationUpdatedTimestamp),
		},
		CorrelationAnchors: []string{alarmARN, alarm.Name},
		SourceRecordID:     firstNonEmpty(alarmARN, alarm.Name),
	}
}

func compositeAlarmEnvelopes(
	boundary awscloud.Boundary,
	alarm CompositeAlarm,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(compositeAlarmObservation(boundary, alarm))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range alarmSNSRelationships(
		boundary,
		alarm.ARN,
		alarm.Name,
		alarm.AlarmActions,
		alarm.OKActions,
		alarm.InsufficientDataActions,
	) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, relationship := range compositeChildRelationships(boundary, alarm) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func compositeAlarmObservation(boundary awscloud.Boundary, alarm CompositeAlarm) awscloud.ResourceObservation {
	alarmARN := strings.TrimSpace(alarm.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          alarmARN,
		ResourceID:   firstNonEmpty(alarmARN, alarm.Name),
		ResourceType: awscloud.ResourceTypeCloudWatchCompositeAlarm,
		Name:         strings.TrimSpace(alarm.Name),
		State:        strings.TrimSpace(alarm.State),
		Tags:         cloneStringMap(alarm.Tags),
		Attributes: map[string]any{
			"description":                           strings.TrimSpace(alarm.Description),
			"state_reason":                          strings.TrimSpace(alarm.StateReason),
			"actions_enabled":                       alarm.ActionsEnabled,
			"alarm_rule":                            strings.TrimSpace(alarm.AlarmRule),
			"alarm_actions":                         cloneStrings(alarm.AlarmActions),
			"ok_actions":                            cloneStrings(alarm.OKActions),
			"insufficient_data_actions":             cloneStrings(alarm.InsufficientDataActions),
			"child_alarm_names":                     cloneStrings(alarm.ChildAlarmNames),
			"state_updated_timestamp":               timeOrNil(alarm.StateUpdatedTimestamp),
			"alarm_configuration_updated_timestamp": timeOrNil(alarm.AlarmConfigurationUpdatedTimestamp),
		},
		CorrelationAnchors: []string{alarmARN, alarm.Name},
		SourceRecordID:     firstNonEmpty(alarmARN, alarm.Name),
	}
}

func dashboardObservation(boundary awscloud.Boundary, dashboard Dashboard) awscloud.ResourceObservation {
	dashboardARN := strings.TrimSpace(dashboard.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          dashboardARN,
		ResourceID:   firstNonEmpty(dashboardARN, dashboard.Name),
		ResourceType: awscloud.ResourceTypeCloudWatchDashboard,
		Name:         strings.TrimSpace(dashboard.Name),
		Attributes: map[string]any{
			// Body / widgets / definition are intentionally absent: the SDK
			// adapter never calls GetDashboard so the body is never observed,
			// and even if it were we would not persist it. Widget bodies often
			// reveal internal infrastructure naming and KPI thresholds.
			"last_modified": timeOrNil(dashboard.LastModified),
			"size_in_bytes": dashboard.SizeInBytes,
		},
		CorrelationAnchors: []string{dashboardARN, dashboard.Name},
		SourceRecordID:     firstNonEmpty(dashboardARN, dashboard.Name),
	}
}

func insightRuleObservation(boundary awscloud.Boundary, rule InsightRule) awscloud.ResourceObservation {
	name := strings.TrimSpace(rule.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeCloudWatchInsightRule,
		Name:         name,
		State:        strings.TrimSpace(rule.State),
		Attributes: map[string]any{
			// Definition is intentionally absent: the SQL-like grammar may
			// encode customer query patterns.
			"schema": strings.TrimSpace(rule.Schema),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func metricStreamEnvelopes(
	boundary awscloud.Boundary,
	stream MetricStream,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(metricStreamObservation(boundary, stream))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	relationship, ok := metricStreamFirehoseRelationship(boundary, stream)
	if ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func metricStreamObservation(boundary awscloud.Boundary, stream MetricStream) awscloud.ResourceObservation {
	streamARN := strings.TrimSpace(stream.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          streamARN,
		ResourceID:   firstNonEmpty(streamARN, stream.Name),
		ResourceType: awscloud.ResourceTypeCloudWatchMetricStream,
		Name:         strings.TrimSpace(stream.Name),
		State:        strings.TrimSpace(stream.State),
		Tags:         cloneStringMap(stream.Tags),
		Attributes: map[string]any{
			"output_format":          strings.TrimSpace(stream.OutputFormat),
			"firehose_arn":           strings.TrimSpace(stream.FirehoseARN),
			"role_arn":               strings.TrimSpace(stream.RoleARN),
			"include_linked_account": stream.IncludeLinkedAccount,
			"creation_date":          timeOrNil(stream.CreationDate),
			"last_update_date":       timeOrNil(stream.LastUpdateDate),
		},
		CorrelationAnchors: []string{streamARN, stream.Name},
		SourceRecordID:     firstNonEmpty(streamARN, stream.Name),
	}
}

func float64OrNil(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
