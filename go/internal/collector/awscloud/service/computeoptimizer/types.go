// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package computeoptimizer

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS Compute Optimizer recommendation
// observations for one AWS claim. Implementations read control-plane
// recommendation metadata through the Compute Optimizer get APIs and never
// mutate Compute Optimizer state, never enroll or unenroll an account, and never
// persist the CloudWatch utilization metric data points behind a recommendation.
type Client interface {
	// Snapshot returns the Compute Optimizer recommendation summaries and the
	// per-resource recommendations visible to the configured AWS credentials.
	// When the account is not opted in to Compute Optimizer the implementation
	// returns an empty snapshot rather than an error.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Compute Optimizer recommendation metadata plus non-fatal
// scan warnings. Every slice is metadata-only.
type Snapshot struct {
	// Summaries is the per-resource-type recommendation summary set (one entry
	// per EC2 instance, Auto Scaling group, EBS volume, and Lambda function
	// summary) reported for the claimed account and region.
	Summaries []RecommendationSummary
	// InstanceRecommendations is the metadata-only set of EC2 instance
	// recommendations.
	InstanceRecommendations []InstanceRecommendation
	// AutoScalingGroupRecommendations is the metadata-only set of Auto Scaling
	// group recommendations.
	AutoScalingGroupRecommendations []AutoScalingGroupRecommendation
	// VolumeRecommendations is the metadata-only set of EBS volume
	// recommendations.
	VolumeRecommendations []VolumeRecommendation
	// LambdaFunctionRecommendations is the metadata-only set of Lambda function
	// recommendations.
	LambdaFunctionRecommendations []LambdaFunctionRecommendation
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// RecommendationSummary is the scanner-owned Compute Optimizer recommendation
// summary model for one resource type. It carries finding-class counts and the
// aggregated savings-opportunity percentage only; it never carries customer
// cost data points or per-resource utilization series.
type RecommendationSummary struct {
	// ResourceType is the Compute Optimizer recommendation resource type the
	// summary aggregates (for example Ec2Instance, AutoScalingGroup, EbsVolume,
	// LambdaFunction).
	ResourceType string
	// AccountID is the AWS account the summary aggregates.
	AccountID string
	// FindingCounts maps each finding class (for example Optimized,
	// Underprovisioned, Overprovisioned, NotOptimized) to the number of resources
	// reported with that finding.
	FindingCounts map[string]float64
	// SavingsOpportunityPercentage is the aggregated estimated monthly savings
	// percentage Compute Optimizer reports across the resource type.
	SavingsOpportunityPercentage float64
}

// InstanceRecommendation is the scanner-owned Compute Optimizer EC2 instance
// recommendation model. It carries the analyzed instance identity, finding,
// current instance type, and the top recommended instance type only. It never
// carries the CloudWatch utilization metric data points behind the finding.
type InstanceRecommendation struct {
	// InstanceARN is the Amazon Resource Name of the analyzed EC2 instance.
	InstanceARN string
	// InstanceName is the reported Name tag of the analyzed instance, when set.
	InstanceName string
	// AccountID is the AWS account that owns the instance.
	AccountID string
	// CurrentInstanceType is the instance type the instance currently runs.
	CurrentInstanceType string
	// Finding is the Compute Optimizer finding class for the instance (for
	// example Underprovisioned, Overprovisioned, Optimized).
	Finding string
	// RecommendedInstanceType is the top-ranked recommended instance type, when
	// Compute Optimizer reports a recommendation option.
	RecommendedInstanceType string
	// LookBackPeriodInDays is the analysis look-back window in days.
	LookBackPeriodInDays float64
	// SavingsOpportunityPercentage is the estimated monthly savings percentage of
	// the top recommended option, when reported.
	SavingsOpportunityPercentage float64
	// LastRefreshTimestamp is when Compute Optimizer last refreshed the
	// recommendation.
	LastRefreshTimestamp time.Time
	// Tags carries the analyzed instance resource tags Compute Optimizer reports.
	Tags map[string]string
}

// AutoScalingGroupRecommendation is the scanner-owned Compute Optimizer Auto
// Scaling group recommendation model. It carries the analyzed group identity,
// finding, current instance type, and the top recommended instance type only.
type AutoScalingGroupRecommendation struct {
	// AutoScalingGroupARN is the Amazon Resource Name of the analyzed group.
	AutoScalingGroupARN string
	// AutoScalingGroupName is the analyzed Auto Scaling group name.
	AutoScalingGroupName string
	// AccountID is the AWS account that owns the group.
	AccountID string
	// CurrentInstanceType is the instance type the group currently launches.
	CurrentInstanceType string
	// Finding is the Compute Optimizer finding class for the group.
	Finding string
	// RecommendedInstanceType is the top-ranked recommended instance type, when
	// Compute Optimizer reports a recommendation option.
	RecommendedInstanceType string
	// LookBackPeriodInDays is the analysis look-back window in days.
	LookBackPeriodInDays float64
	// SavingsOpportunityPercentage is the estimated monthly savings percentage of
	// the top recommended option, when reported.
	SavingsOpportunityPercentage float64
	// LastRefreshTimestamp is when Compute Optimizer last refreshed the
	// recommendation.
	LastRefreshTimestamp time.Time
}

// VolumeRecommendation is the scanner-owned Compute Optimizer EBS volume
// recommendation model. It carries the analyzed volume identity, finding, and
// current/recommended volume type only. The scanner records the volume identity
// as metadata; recommendation-to-volume relationship projection is a separate
// follow-up.
type VolumeRecommendation struct {
	// VolumeARN is the Amazon Resource Name of the analyzed EBS volume.
	VolumeARN string
	// AccountID is the AWS account that owns the volume.
	AccountID string
	// CurrentVolumeType is the volume type the volume currently uses.
	CurrentVolumeType string
	// Finding is the Compute Optimizer finding class for the volume (for example
	// Optimized, NotOptimized).
	Finding string
	// RecommendedVolumeType is the top-ranked recommended volume type, when
	// Compute Optimizer reports a recommendation option.
	RecommendedVolumeType string
	// LookBackPeriodInDays is the analysis look-back window in days.
	LookBackPeriodInDays float64
	// SavingsOpportunityPercentage is the estimated monthly savings percentage of
	// the top recommended option, when reported.
	SavingsOpportunityPercentage float64
	// LastRefreshTimestamp is when Compute Optimizer last refreshed the
	// recommendation.
	LastRefreshTimestamp time.Time
	// Tags carries the analyzed volume resource tags Compute Optimizer reports.
	Tags map[string]string
}

// LambdaFunctionRecommendation is the scanner-owned Compute Optimizer Lambda
// function recommendation model. It carries the analyzed function identity,
// finding, current memory size, and the top recommended memory size only.
type LambdaFunctionRecommendation struct {
	// FunctionARN is the Amazon Resource Name of the analyzed Lambda function.
	FunctionARN string
	// FunctionVersion is the analyzed function version, when reported.
	FunctionVersion string
	// AccountID is the AWS account that owns the function.
	AccountID string
	// CurrentMemorySize is the function's currently configured memory size in MB.
	CurrentMemorySize int32
	// Finding is the Compute Optimizer finding class for the function.
	Finding string
	// RecommendedMemorySize is the top-ranked recommended memory size in MB, when
	// Compute Optimizer reports a memory-size recommendation option.
	RecommendedMemorySize int32
	// LookBackPeriodInDays is the analysis look-back window in days.
	LookBackPeriodInDays float64
	// SavingsOpportunityPercentage is the estimated monthly savings percentage of
	// the top recommended option, when reported.
	SavingsOpportunityPercentage float64
	// LastRefreshTimestamp is when Compute Optimizer last refreshed the
	// recommendation.
	LastRefreshTimestamp time.Time
	// Tags carries the analyzed function resource tags Compute Optimizer reports.
	Tags map[string]string
}
