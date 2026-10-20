// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK v2 X-Ray client into the scanner-owned
// Client interface defined by the parent xray package.
//
// The adapter is configuration-only by construction. Its apiClient interface
// lists exactly the three X-Ray configuration reads — GetGroups,
// GetSamplingRules, GetEncryptionConfig — and MUST NOT include any
// observability-payload read (GetTraceSummaries, BatchGetTraces, GetTraceGraph,
// GetServiceGraph, GetTimeSeriesServiceStatistics, GetInsight,
// GetInsightSummaries, GetInsightEvents, GetInsightImpactGraph,
// GetSamplingTargets, GetSamplingStatisticSummaries) or any mutation
// (PutTraceSegments, PutTelemetryRecords, CreateGroup, UpdateGroup,
// DeleteGroup, CreateSamplingRule, UpdateSamplingRule, DeleteSamplingRule,
// PutEncryptionConfig). Because the adapter holds an apiClient interface value
// rather than the concrete *xray.Client, the compiler ensures these methods are
// unreachable. The companion test asserts the interface shape by reflection.
package awssdk
