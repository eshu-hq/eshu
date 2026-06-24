// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceApplicationAutoScaling identifies the regional AWS Application Auto
	// Scaling metadata-only scan slice. The scanner reads scalable target,
	// scaling policy, and scheduled action control-plane metadata through the
	// Application Auto Scaling Describe APIs (DescribeScalableTargets,
	// DescribeScalingPolicies, DescribeScheduledActions) across every supported
	// service namespace. It never registers, deregisters, or mutates a scalable
	// target, never puts or deletes a scaling policy or scheduled action, and
	// never invokes a scaling action.
	ServiceApplicationAutoScaling = "applicationautoscaling"
)

const (
	// ResourceTypeApplicationAutoScalingScalableTarget identifies an Application
	// Auto Scaling scalable target metadata resource. The scanner emits the
	// service namespace, scalable dimension, the scaled resource id, the IAM
	// role ARN Application Auto Scaling assumes, and the min/max capacity bounds.
	ResourceTypeApplicationAutoScalingScalableTarget = "aws_application_autoscaling_scalable_target"
	// ResourceTypeApplicationAutoScalingScalingPolicy identifies an Application
	// Auto Scaling scaling policy metadata resource. The scanner emits identity,
	// policy type, the owning service namespace/dimension/resource id, and the
	// names of the CloudWatch alarms the policy is bound to. Step-scaling and
	// target-tracking configuration bodies are intentionally excluded.
	ResourceTypeApplicationAutoScalingScalingPolicy = "aws_application_autoscaling_scaling_policy"
	// ResourceTypeApplicationAutoScalingScheduledAction identifies an Application
	// Auto Scaling scheduled action metadata resource. The scanner emits
	// identity, the owning service namespace/dimension/resource id, the schedule
	// expression, the time zone, and the start/end window. The min/max target
	// capacity carried by the action is recorded; no other body is read.
	ResourceTypeApplicationAutoScalingScheduledAction = "aws_application_autoscaling_scheduled_action"
)

const (
	// RelationshipApplicationAutoScalingPolicyForScalableTarget records that a
	// scaling policy governs a scalable target. The target is keyed by the same
	// composite scalable-target resource_id the scalable-target node publishes,
	// so the edge joins that node exactly.
	RelationshipApplicationAutoScalingPolicyForScalableTarget = "application_autoscaling_policy_for_scalable_target"
	// RelationshipApplicationAutoScalingScheduledActionForScalableTarget records
	// that a scheduled action targets a scalable target, keyed by the composite
	// scalable-target resource_id the scalable-target node publishes.
	RelationshipApplicationAutoScalingScheduledActionForScalableTarget = "application_autoscaling_scheduled_action_for_scalable_target"
	// RelationshipApplicationAutoScalingTargetScalesDynamoDBTable records that a
	// scalable target governs a DynamoDB table's provisioned capacity. The target
	// is keyed by the partition-aware DynamoDB table ARN the DynamoDB scanner
	// publishes as its table resource_id.
	RelationshipApplicationAutoScalingTargetScalesDynamoDBTable = "application_autoscaling_target_scales_dynamodb_table"
	// RelationshipApplicationAutoScalingTargetScalesECSService records that a
	// scalable target governs an ECS service's desired task count. The target is
	// keyed by the partition-aware ECS service ARN the ECS scanner publishes as
	// its service resource_id.
	RelationshipApplicationAutoScalingTargetScalesECSService = "application_autoscaling_target_scales_ecs_service"
	// RelationshipApplicationAutoScalingTargetScalesRDSCluster records that a
	// scalable target governs an Aurora DB cluster's read-replica count. The
	// target is keyed by the partition-aware RDS cluster ARN the RDS scanner
	// publishes as its cluster resource_id.
	RelationshipApplicationAutoScalingTargetScalesRDSCluster = "application_autoscaling_target_scales_rds_cluster"
	// RelationshipApplicationAutoScalingTargetScalesLambdaFunction records that a
	// scalable target governs a Lambda function's provisioned concurrency. The
	// target is keyed by the partition-aware base Lambda function ARN the Lambda
	// scanner publishes as its function resource_id; the version/alias qualifier
	// carried by the scaling resource id is dropped to match the function node.
	RelationshipApplicationAutoScalingTargetScalesLambdaFunction = "application_autoscaling_target_scales_lambda_function"
	// RelationshipApplicationAutoScalingPolicyTriggersCloudWatchAlarm records that
	// a scaling policy is bound to a CloudWatch alarm. The target is keyed by the
	// alarm ARN Application Auto Scaling reports, which matches the CloudWatch
	// scanner's published alarm resource_id.
	RelationshipApplicationAutoScalingPolicyTriggersCloudWatchAlarm = "application_autoscaling_policy_triggers_cloudwatch_alarm"
)
