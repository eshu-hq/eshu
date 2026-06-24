// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import "context"

// Client is the AWS Config metadata read surface consumed by Scanner. Runtime
// adapters translate AWS SDK responses into these scanner-owned metadata
// records. The interface intentionally exposes no recorded configuration-item
// read (GetResourceConfigHistory, BatchGetResourceConfig,
// GetDiscoveredResourceCounts), no per-resource compliance-detail read
// (GetComplianceDetailsByConfigRule), no custom-rule policy-body read, and no
// mutation call (Put/Delete/Start/Stop).
type Client interface {
	// ConfigurationRecorders returns the Config configuration recorders for the
	// claimed account and region with the recorded resource-type scope.
	ConfigurationRecorders(context.Context) ([]ConfigurationRecorder, error)
	// DeliveryChannels returns the Config delivery channels for the claimed
	// account and region.
	DeliveryChannels(context.Context) ([]DeliveryChannel, error)
	// ConfigRules returns Config rule metadata: name, identity, owner, the
	// managed-rule source identifier or custom-Lambda ARN, and the resource-type
	// scope. Implementations MUST NOT read compliance evaluation result bodies.
	ConfigRules(context.Context) ([]ConfigRule, error)
	// ConformancePacks returns conformance pack metadata with deployment status
	// and the member-rule name set used for the rule count and containment
	// relationships. Implementations MUST NOT read conformance pack template
	// bodies.
	ConformancePacks(context.Context) ([]ConformancePack, error)
	// ConfigurationAggregators returns Config configuration aggregator metadata
	// with the aggregated source account and region set.
	ConfigurationAggregators(context.Context) ([]ConfigurationAggregator, error)
	// RetentionConfigurations returns Config retention configuration metadata.
	RetentionConfigurations(context.Context) ([]RetentionConfiguration, error)
}

// ConfigurationRecorder is the scanner-owned view of an AWS Config configuration
// recorder. It records which resource types Config records; it never carries
// recorded configuration item bodies.
type ConfigurationRecorder struct {
	Name                       string
	AllSupported               bool
	IncludeGlobalResourceTypes bool
	RecordingStrategy          string
	ResourceTypes              []string
}

// DeliveryChannel is the scanner-owned view of an AWS Config delivery channel.
type DeliveryChannel struct {
	Name                     string
	S3BucketName             string
	S3KeyPrefix              string
	S3KMSKeyARN              string
	SNSTopicARN              string
	SnapshotDeliveryInterval string
}

// ConfigRule is the metadata-only view of an AWS Config rule. Owner classifies
// the rule as AWS managed, custom Lambda, or custom policy. SourceIdentifier
// carries the managed-rule identifier; LambdaFunctionARN carries the custom
// Lambda evaluator ARN. ScopeResourceTypes is the resource-type scope the rule
// evaluates. Compliance evaluation result bodies are never part of this type.
type ConfigRule struct {
	Name               string
	ARN                string
	ID                 string
	State              string
	Owner              string
	SourceIdentifier   string
	LambdaFunctionARN  string
	ScopeResourceTypes []string
}

// ConformancePack is the metadata-only view of an AWS Config conformance pack.
// Status is the deployment state. RuleNames is the set of member Config rule
// names used for the rule count and the containment relationships. Conformance
// pack template bodies are never part of this type.
type ConformancePack struct {
	Name      string
	ARN       string
	ID        string
	Status    string
	CreatedBy string
	RuleNames []string
}

// ConfigurationAggregator is the metadata-only view of an AWS Config
// configuration aggregator. SourceAccountIDs and SourceRegions describe an
// account-based aggregation; OrganizationRoleARN and OrganizationAllAWSRegions
// describe an organization-based aggregation. AllAWSRegions and
// OrganizationAllAWSRegions each map the SDK's AllAwsRegions flag ("aggregate
// from all AWS regions") for their respective aggregation sources.
type ConfigurationAggregator struct {
	Name                      string
	ARN                       string
	CreatedBy                 string
	SourceAccountIDs          []string
	SourceRegions             []string
	AllAWSRegions             bool
	OrganizationRoleARN       string
	OrganizationAllAWSRegions bool
}

// RetentionConfiguration is the metadata-only view of an AWS Config retention
// configuration: how long Config retains configuration item history.
type RetentionConfiguration struct {
	Name                  string
	RetentionPeriodInDays int32
}
