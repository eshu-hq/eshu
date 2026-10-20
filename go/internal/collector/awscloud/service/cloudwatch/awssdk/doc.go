// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK v2 CloudWatch client into the
// scanner-owned Client interface defined by the parent cloudwatch package.
//
// The adapter intentionally exposes only List/Describe-shaped methods. The
// apiClient interface here MUST NOT include GetDashboard or any mutation API
// (PutMetricAlarm, DeleteAlarms, PutCompositeAlarm, PutDashboard,
// DeleteDashboards, EnableAlarmActions, DisableAlarmActions, SetAlarmState,
// PutInsightRule, DeleteInsightRules, StartMetricStreams, StopMetricStreams,
// PutMetricData). Because the adapter holds an apiClient interface value
// rather than the concrete *cloudwatch.Client, the compiler ensures these
// methods are unreachable. The companion test asserts the interface shape
// and that no forbidden call was made during a normal scan.
package awssdk
