// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cloudwatch emits Amazon CloudWatch metadata-only facts for one
// claimed AWS boundary. It covers metric alarms, composite alarms, dashboards
// (identity only), Contributor Insights rules (identity only), and metric
// streams.
//
// The scanner is strictly metadata-only. It never calls mutation APIs such as
// PutMetricAlarm, DeleteAlarms, PutCompositeAlarm, PutDashboard,
// DeleteDashboards, EnableAlarmActions, DisableAlarmActions, SetAlarmState,
// PutInsightRule, DeleteInsightRules, Start/Stop MetricStreams, or
// PutMetricData. It never persists dashboard body JSON or Contributor Insights
// rule definitions. Alarm metric dimensions whose key names look like customer
// tags are routed through the shared redact library before persistence.
//
// CloudWatch Logs log groups are emitted by the separate `cloudwatchlogs`
// service slice and are not part of this package's contract.
package cloudwatch
