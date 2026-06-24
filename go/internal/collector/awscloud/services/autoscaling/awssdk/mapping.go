// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsautoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"

	autoscalingservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/autoscaling"
)

// mapGroup maps an Auto Scaling group into the scanner-owned type. The reported
// Instances list and WarmPoolConfiguration are intentionally not read so
// per-instance churn never becomes graph truth.
func mapGroup(detail awsautoscalingtypes.AutoScalingGroup) autoscalingservice.Group {
	group := autoscalingservice.Group{
		ARN:                     aws.ToString(detail.AutoScalingGroupARN),
		Name:                    aws.ToString(detail.AutoScalingGroupName),
		MinSize:                 aws.ToInt32(detail.MinSize),
		MaxSize:                 aws.ToInt32(detail.MaxSize),
		DesiredCapacity:         aws.ToInt32(detail.DesiredCapacity),
		AvailabilityZones:       cloneStrings(detail.AvailabilityZones),
		HealthCheckType:         aws.ToString(detail.HealthCheckType),
		HealthCheckGracePeriod:  aws.ToInt32(detail.HealthCheckGracePeriod),
		Status:                  aws.ToString(detail.Status),
		CapacityRebalance:       aws.ToBool(detail.CapacityRebalance),
		DefaultCooldown:         aws.ToInt32(detail.DefaultCooldown),
		NewInstancesProtected:   aws.ToBool(detail.NewInstancesProtectedFromScaleIn),
		MaxInstanceLifetime:     aws.ToInt32(detail.MaxInstanceLifetime),
		LaunchConfigurationName: aws.ToString(detail.LaunchConfigurationName),
		SubnetIDs:               splitSubnetIdentifier(aws.ToString(detail.VPCZoneIdentifier)),
		TargetGroupARNs:         cloneStrings(detail.TargetGroupARNs),
		LoadBalancerNames:       cloneStrings(detail.LoadBalancerNames),
		ServiceLinkedRoleARN:    aws.ToString(detail.ServiceLinkedRoleARN),
		TerminationPolicies:     cloneStrings(detail.TerminationPolicies),
		CreatedTime:             aws.ToTime(detail.CreatedTime),
		Tags:                    mapTagDescriptions(detail.Tags),
	}
	if detail.LaunchTemplate != nil {
		group.LaunchTemplateID = aws.ToString(detail.LaunchTemplate.LaunchTemplateId)
		group.LaunchTemplateName = aws.ToString(detail.LaunchTemplate.LaunchTemplateName)
		group.LaunchTemplateVersion = aws.ToString(detail.LaunchTemplate.Version)
	}
	return group
}

// mapLaunchConfiguration maps only the launch configuration identity. The
// reported UserData, BlockDeviceMappings, SecurityGroups, KeyName, and
// IamInstanceProfile are intentionally discarded so they never cross the
// adapter boundary; UserData in particular can hold bootstrap secrets.
func mapLaunchConfiguration(detail awsautoscalingtypes.LaunchConfiguration) autoscalingservice.LaunchConfiguration {
	return autoscalingservice.LaunchConfiguration{
		ARN:  aws.ToString(detail.LaunchConfigurationARN),
		Name: aws.ToString(detail.LaunchConfigurationName),
	}
}

// mapScalingPolicy maps the scaling policy identity, type, and owning group
// reference. The step adjustments, target-tracking configuration, and
// CloudWatch alarm bindings AWS reports are intentionally discarded.
func mapScalingPolicy(detail awsautoscalingtypes.ScalingPolicy) autoscalingservice.ScalingPolicy {
	return autoscalingservice.ScalingPolicy{
		ARN:                  aws.ToString(detail.PolicyARN),
		Name:                 aws.ToString(detail.PolicyName),
		AutoScalingGroupName: aws.ToString(detail.AutoScalingGroupName),
		PolicyType:           aws.ToString(detail.PolicyType),
		AdjustmentType:       aws.ToString(detail.AdjustmentType),
		Enabled:              aws.ToBool(detail.Enabled),
	}
}

// mapLifecycleHook maps the lifecycle hook transition, target, role, and
// timeout metadata. The reported NotificationMetadata is intentionally
// discarded because it can carry caller-supplied free-form data.
func mapLifecycleHook(detail awsautoscalingtypes.LifecycleHook) autoscalingservice.LifecycleHook {
	return autoscalingservice.LifecycleHook{
		Name:                  aws.ToString(detail.LifecycleHookName),
		AutoScalingGroupName:  aws.ToString(detail.AutoScalingGroupName),
		LifecycleTransition:   aws.ToString(detail.LifecycleTransition),
		DefaultResult:         aws.ToString(detail.DefaultResult),
		HeartbeatTimeout:      aws.ToInt32(detail.HeartbeatTimeout),
		GlobalTimeout:         aws.ToInt32(detail.GlobalTimeout),
		NotificationTargetARN: aws.ToString(detail.NotificationTargetARN),
		RoleARN:               aws.ToString(detail.RoleARN),
	}
}

func mapScheduledAction(detail awsautoscalingtypes.ScheduledUpdateGroupAction) autoscalingservice.ScheduledAction {
	return autoscalingservice.ScheduledAction{
		ARN:                  aws.ToString(detail.ScheduledActionARN),
		Name:                 aws.ToString(detail.ScheduledActionName),
		AutoScalingGroupName: aws.ToString(detail.AutoScalingGroupName),
		Recurrence:           aws.ToString(detail.Recurrence),
		TimeZone:             aws.ToString(detail.TimeZone),
		MinSize:              detail.MinSize,
		MaxSize:              detail.MaxSize,
		DesiredCapacity:      detail.DesiredCapacity,
		StartTime:            aws.ToTime(detail.StartTime),
		EndTime:              aws.ToTime(detail.EndTime),
	}
}

// splitSubnetIdentifier parses the comma-separated VPCZoneIdentifier into bare
// subnet IDs, matching the EC2-owned subnet resource_id form.
func splitSubnetIdentifier(identifier string) []string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil
	}
	parts := strings.Split(identifier, ",")
	output := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func mapTagDescriptions(tags []awsautoscalingtypes.TagDescription) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
