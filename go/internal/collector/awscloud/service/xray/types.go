// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package xray

import "context"

// Client is the AWS X-Ray read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned configuration records.
//
// The interface is configuration-only by construction. It exposes exactly the
// three X-Ray configuration reads — GetGroups, GetSamplingRules, and
// GetEncryptionConfig — and nothing else. It MUST NOT include any
// observability-payload read (GetTraceSummaries, BatchGetTraces,
// GetTraceGraph, GetServiceGraph, GetTimeSeriesServiceStatistics,
// GetInsight, GetInsightSummaries, GetInsightEvents, GetInsightImpactGraph)
// and MUST NOT include any mutation (PutTraceSegments, PutTelemetryRecords,
// CreateGroup, UpdateGroup, DeleteGroup, CreateSamplingRule,
// UpdateSamplingRule, DeleteSamplingRule, PutEncryptionConfig). A reflection
// test asserts those methods are unreachable through this interface.
type Client interface {
	// GetGroups returns X-Ray group configuration (name, ARN, filter
	// expression, insights flags) only. No trace selected by a group's filter
	// expression is read.
	GetGroups(ctx context.Context) ([]Group, error)
	// GetSamplingRules returns X-Ray sampling rule configuration (name, ARN,
	// priority, reservoir, fixed rate, and service match criteria) only. No
	// sampled trace, segment, or summary is read.
	GetSamplingRules(ctx context.Context) ([]SamplingRule, error)
	// GetEncryptionConfig returns the account-region X-Ray encryption
	// configuration (type, status, KMS key reference) only. AWS exposes a
	// single encryption configuration per account and region.
	GetEncryptionConfig(ctx context.Context) (*EncryptionConfig, error)
}

// Group is the scanner-owned representation of one X-Ray group. It carries the
// group identity and its trace filter expression. The filter expression is a
// configuration string describing which traces the group includes; the traces
// themselves are never read.
type Group struct {
	ARN                  string
	Name                 string
	FilterExpression     string
	InsightsEnabled      *bool
	NotificationsEnabled *bool
}

// SamplingRule is the scanner-owned representation of one X-Ray sampling rule.
// It carries the rule identity, sampling configuration (priority, reservoir
// size, fixed rate), and the service match criteria. The criteria describe
// which requests X-Ray samples; no sampled request payload is read.
type SamplingRule struct {
	ARN           string
	Name          string
	Priority      *int32
	ReservoirSize int32
	FixedRate     float64
	ServiceName   string
	ServiceType   string
	Host          string
	HTTPMethod    string
	URLPath       string
	ResourceARN   string
	Version       *int32
}

// EncryptionConfig is the scanner-owned representation of the account-region
// X-Ray encryption configuration. Type is NONE for X-Ray default encryption or
// KMS when a customer-managed key is configured; KeyID is the reported KMS key
// reference (key id, key ARN, or alias) when Type is KMS.
type EncryptionConfig struct {
	Type   string
	Status string
	KeyID  string
}
