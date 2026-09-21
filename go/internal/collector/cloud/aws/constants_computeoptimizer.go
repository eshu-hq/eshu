// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceComputeOptimizer identifies the regional AWS Compute Optimizer
	// metadata-only scan slice. The scanner reads recommendation control-plane
	// metadata through the Compute Optimizer get APIs (GetRecommendationSummaries,
	// GetEC2InstanceRecommendations, GetAutoScalingGroupRecommendations,
	// GetEBSVolumeRecommendations, GetLambdaFunctionRecommendations) and never
	// mutates Compute Optimizer state, never enrolls or unenrolls an account, and
	// never reads CloudWatch metric data points behind a recommendation.
	ServiceComputeOptimizer = "computeoptimizer"
)

const (
	// ResourceTypeComputeOptimizerRecommendationSummary identifies a Compute
	// Optimizer recommendation summary metadata resource. The scanner emits one
	// summary per recommendation resource type (EC2 instance, Auto Scaling group,
	// EBS volume, Lambda function) for the claimed account and region, carrying the
	// finding-class counts and aggregated savings-opportunity percentage only.
	ResourceTypeComputeOptimizerRecommendationSummary = "aws_compute_optimizer_recommendation_summary"
	// ResourceTypeComputeOptimizerRecommendation identifies a single Compute
	// Optimizer per-resource recommendation metadata resource. The scanner emits
	// one recommendation per analyzed EC2 instance, Auto Scaling group, EBS volume,
	// and Lambda function, carrying the finding, current configuration shape, and
	// top recommended-option shape only. It never persists the underlying
	// CloudWatch utilization metric data points.
	ResourceTypeComputeOptimizerRecommendation = "aws_compute_optimizer_recommendation"
)

const (
	// RelationshipComputeOptimizerRecommendationTargetsInstance records that a
	// Compute Optimizer recommendation analyzes an EC2 instance. The target is
	// keyed by the bare EC2 instance id (i-...), matching how EC2 instance
	// relationship targets are published, so the edge joins the instance identity
	// instead of dangling.
	RelationshipComputeOptimizerRecommendationTargetsInstance = "compute_optimizer_recommendation_targets_instance"
	// RelationshipComputeOptimizerRecommendationTargetsAutoScalingGroup records
	// that a Compute Optimizer recommendation analyzes an Auto Scaling group. The
	// target is keyed by the Auto Scaling group name, matching how the autoscaling
	// scanner publishes its group resource_id.
	RelationshipComputeOptimizerRecommendationTargetsAutoScalingGroup = "compute_optimizer_recommendation_targets_auto_scaling_group"
	// RelationshipComputeOptimizerRecommendationTargetsFunction records that a
	// Compute Optimizer recommendation analyzes a Lambda function. The target is
	// keyed by the function ARN, matching how the lambda scanner publishes its
	// function resource_id.
	RelationshipComputeOptimizerRecommendationTargetsFunction = "compute_optimizer_recommendation_targets_function"
)
