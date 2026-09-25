// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudwatch

import (
	"context"
	"time"
)

// Client is the CloudWatch read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned metadata records. The
// interface intentionally exposes only List/Describe-shaped methods. It MUST
// NOT include GetDashboard, any Put*, Delete*, Enable*, Disable*, Start*,
// Stop*, or SetAlarmState method.
type Client interface {
	// ListMetricAlarms returns metric alarm metadata only.
	ListMetricAlarms(ctx context.Context) ([]MetricAlarm, error)
	// ListCompositeAlarms returns composite alarm metadata only.
	ListCompositeAlarms(ctx context.Context) ([]CompositeAlarm, error)
	// ListDashboards returns dashboard identity (name + last modified) only.
	// Adapters MUST NOT call GetDashboard or persist dashboard body JSON.
	ListDashboards(ctx context.Context) ([]Dashboard, error)
	// ListInsightRules returns Contributor Insights rule identity (name +
	// state) only. Adapters MUST NOT persist the rule definition.
	ListInsightRules(ctx context.Context) ([]InsightRule, error)
	// ListMetricStreams returns metric stream metadata (name, output format,
	// destination ARN) only.
	ListMetricStreams(ctx context.Context) ([]MetricStream, error)
}

// MetricAlarm is the scanner-owned representation of one CloudWatch metric
// alarm. It carries identity, configured thresholds, action ARNs, and the
// observed metric identity. Metric data points themselves are not part of the
// model.
type MetricAlarm struct {
	ARN                                string
	Name                               string
	Description                        string
	State                              string
	StateReason                        string
	ActionsEnabled                     bool
	AlarmActions                       []string
	OKActions                          []string
	InsufficientDataActions            []string
	Namespace                          string
	MetricName                         string
	Statistic                          string
	ExtendedStatistic                  string
	ComparisonOperator                 string
	Threshold                          *float64
	EvaluationPeriods                  int32
	DatapointsToAlarm                  int32
	Period                             int32
	TreatMissingData                   string
	EvaluateLowSampleCountPercentile   string
	Unit                               string
	Dimensions                         []MetricDimension
	StateUpdatedTimestamp              time.Time
	AlarmConfigurationUpdatedTimestamp time.Time
	Tags                               map[string]string
}

// MetricDimension is one CloudWatch metric dimension reported on an alarm.
// Customer-tag-named values may carry sensitive payload, so the scanner
// routes them through the shared redact library before persistence.
type MetricDimension struct {
	Name  string
	Value string
}

// CompositeAlarm is the scanner-owned representation of one CloudWatch
// composite alarm. It carries identity, the AlarmRule expression, action ARNs,
// and the list of child alarm names extracted from the rule.
type CompositeAlarm struct {
	ARN                                string
	Name                               string
	Description                        string
	State                              string
	StateReason                        string
	ActionsEnabled                     bool
	AlarmRule                          string
	AlarmActions                       []string
	OKActions                          []string
	InsufficientDataActions            []string
	ChildAlarmNames                    []string
	StateUpdatedTimestamp              time.Time
	AlarmConfigurationUpdatedTimestamp time.Time
	Tags                               map[string]string
}

// Dashboard is the scanner-owned representation of one CloudWatch dashboard.
// Only the name, ARN, and last-modified timestamp are persisted. The
// dashboard body JSON is intentionally excluded because widget bodies often
// reveal internal infrastructure naming and KPI thresholds.
type Dashboard struct {
	ARN          string
	Name         string
	LastModified time.Time
	SizeInBytes  int64
}

// InsightRule is the scanner-owned representation of one CloudWatch
// Contributor Insights rule. Only the name, state, and schema label are
// persisted. The rule definition body is intentionally excluded because the
// SQL-like grammar may encode customer query patterns.
type InsightRule struct {
	Name   string
	State  string
	Schema string
}

// MetricStream is the scanner-owned representation of one CloudWatch metric
// stream. It carries the name, output format, and Firehose destination ARN.
type MetricStream struct {
	ARN                  string
	Name                 string
	State                string
	OutputFormat         string
	FirehoseARN          string
	RoleARN              string
	CreationDate         time.Time
	LastUpdateDate       time.Time
	IncludeLinkedAccount bool
	Tags                 map[string]string
}
