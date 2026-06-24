// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceAutoScaling identifies the regional Amazon EC2 Auto Scaling
	// metadata scan slice.
	ServiceAutoScaling = "autoscaling"
)

const (
	// ResourceTypeAutoScalingLaunchConfiguration identifies an EC2 Auto Scaling
	// launch configuration. Only the name and ARN are emitted; the launch
	// configuration UserData (which can hold bootstrap secrets) is never read or
	// persisted.
	ResourceTypeAutoScalingLaunchConfiguration = "aws_autoscaling_launch_configuration"
	// ResourceTypeAutoScalingPolicy identifies an EC2 Auto Scaling scaling
	// policy.
	ResourceTypeAutoScalingPolicy = "aws_autoscaling_policy"
	// ResourceTypeAutoScalingLifecycleHook identifies an EC2 Auto Scaling
	// lifecycle hook.
	ResourceTypeAutoScalingLifecycleHook = "aws_autoscaling_lifecycle_hook"
	// ResourceTypeAutoScalingScheduledAction identifies an EC2 Auto Scaling
	// scheduled action.
	ResourceTypeAutoScalingScheduledAction = "aws_autoscaling_scheduled_action"
)

const (
	// RelationshipAutoScalingGroupUsesLaunchTemplate records the EC2 launch
	// template an Auto Scaling group launches instances from. The edge keys on
	// the launch template ID (lt-...) when reported, otherwise the launch
	// template name, matching the EC2 launch-template resource_id form owned by
	// the Batch scanner.
	RelationshipAutoScalingGroupUsesLaunchTemplate = "autoscaling_group_uses_launch_template"
	// RelationshipAutoScalingGroupUsesLaunchConfiguration records the launch
	// configuration an Auto Scaling group launches instances from. The edge keys
	// on the launch configuration name.
	RelationshipAutoScalingGroupUsesLaunchConfiguration = "autoscaling_group_uses_launch_configuration"
	// RelationshipAutoScalingGroupUsesSubnet records a subnet placement for an
	// Auto Scaling group. The edge keys on the bare subnet ID, matching the
	// EC2-owned subnet resource_id form.
	RelationshipAutoScalingGroupUsesSubnet = "autoscaling_group_uses_subnet"
	// RelationshipAutoScalingGroupAttachedToTargetGroup records an ELBv2 target
	// group an Auto Scaling group registers instances with. The edge keys on the
	// target group ARN, matching the ELBv2-owned target-group resource_id form.
	RelationshipAutoScalingGroupAttachedToTargetGroup = "autoscaling_group_attached_to_target_group"
	// RelationshipAutoScalingGroupUsesIAMRole records the service-linked IAM
	// role an Auto Scaling group assumes to call other AWS services. The edge
	// keys on the role ARN.
	RelationshipAutoScalingGroupUsesIAMRole = "autoscaling_group_uses_iam_role"
	// RelationshipAutoScalingPolicyTargetsGroup records the Auto Scaling group a
	// scaling policy applies to.
	RelationshipAutoScalingPolicyTargetsGroup = "autoscaling_policy_targets_group"
	// RelationshipAutoScalingLifecycleHookTargetsGroup records the Auto Scaling
	// group a lifecycle hook is defined on.
	RelationshipAutoScalingLifecycleHookTargetsGroup = "autoscaling_lifecycle_hook_targets_group"
	// RelationshipAutoScalingScheduledActionTargetsGroup records the Auto
	// Scaling group a scheduled action is defined on.
	RelationshipAutoScalingScheduledActionTargetsGroup = "autoscaling_scheduled_action_targets_group"
)
