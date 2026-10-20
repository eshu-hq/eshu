// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package autoscaling

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// groupRelationships records the launch-template, launch-configuration, subnet,
// target-group, and service-linked IAM role joins of an Auto Scaling group.
// Every edge sets a non-empty target_type matching the target scanner's
// resource_id form.
func groupRelationships(boundary awscloud.Boundary, group Group) []awscloud.RelationshipObservation {
	groupName := strings.TrimSpace(group.Name)
	if groupName == "" {
		return nil
	}
	groupARN := strings.TrimSpace(group.ARN)
	var observations []awscloud.RelationshipObservation

	// Launch template: key on the launch template ID (lt-...) when reported,
	// otherwise the launch template name, matching the EC2 launch-template
	// resource_id form owned by the Batch scanner.
	if launchTemplate := firstNonEmpty(group.LaunchTemplateID, group.LaunchTemplateName); launchTemplate != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAutoScalingGroupUsesLaunchTemplate,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: launchTemplate,
			TargetType:       awscloud.ResourceTypeEC2LaunchTemplate,
			SourceRecordID:   groupName + "#launch-template#" + launchTemplate,
		})
	}

	// Launch configuration: key on the launch configuration name, matching the
	// launch-configuration resource_id this scanner emits.
	if launchConfiguration := strings.TrimSpace(group.LaunchConfigurationName); launchConfiguration != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAutoScalingGroupUsesLaunchConfiguration,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: launchConfiguration,
			TargetType:       awscloud.ResourceTypeAutoScalingLaunchConfiguration,
			SourceRecordID:   groupName + "#launch-configuration#" + launchConfiguration,
		})
	}

	// Subnets: key on the bare subnet ID, matching the EC2-owned subnet
	// resource_id form.
	for _, subnetID := range dedupeStrings(group.SubnetIDs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAutoScalingGroupUsesSubnet,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: subnetID,
			TargetType:       awscloud.ResourceTypeEC2Subnet,
			SourceRecordID:   groupName + "#subnet#" + subnetID,
		})
	}

	// Target groups: key on the target group ARN, matching the ELBv2-owned
	// target-group resource_id form.
	for _, targetGroupARN := range dedupeStrings(group.TargetGroupARNs) {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAutoScalingGroupAttachedToTargetGroup,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: targetGroupARN,
			TargetARN:        targetGroupARN,
			TargetType:       awscloud.ResourceTypeELBv2TargetGroup,
			SourceRecordID:   groupName + "#target-group#" + targetGroupARN,
		})
	}

	// Service-linked IAM role: key on the role ARN.
	if roleARN := strings.TrimSpace(group.ServiceLinkedRoleARN); roleARN != "" {
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipAutoScalingGroupUsesIAMRole,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID:   groupName + "#service-linked-role#" + roleARN,
		})
	}

	return observations
}

// scalingPolicyRelationship records the Auto Scaling group a scaling policy
// applies to. The edge keys on the group name, matching the Auto Scaling group
// resource_id form.
func scalingPolicyRelationship(
	boundary awscloud.Boundary,
	policy ScalingPolicy,
) (awscloud.RelationshipObservation, bool) {
	policyID := firstNonEmpty(policy.ARN, policy.Name)
	groupName := strings.TrimSpace(policy.AutoScalingGroupName)
	if policyID == "" || groupName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAutoScalingPolicyTargetsGroup,
		SourceResourceID: policyID,
		SourceARN:        strings.TrimSpace(policy.ARN),
		TargetResourceID: groupName,
		TargetType:       awscloud.ResourceTypeAutoScalingGroup,
		SourceRecordID:   policyID + "#group#" + groupName,
	}, true
}

// lifecycleHookRelationship records the Auto Scaling group a lifecycle hook is
// defined on. The edge keys on the group name.
func lifecycleHookRelationship(
	boundary awscloud.Boundary,
	hook LifecycleHook,
) (awscloud.RelationshipObservation, bool) {
	name := strings.TrimSpace(hook.Name)
	groupName := strings.TrimSpace(hook.AutoScalingGroupName)
	if name == "" || groupName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	hookID := groupName + "/" + name
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAutoScalingLifecycleHookTargetsGroup,
		SourceResourceID: hookID,
		TargetResourceID: groupName,
		TargetType:       awscloud.ResourceTypeAutoScalingGroup,
		SourceRecordID:   hookID + "#group#" + groupName,
	}, true
}

// scheduledActionRelationship records the Auto Scaling group a scheduled action
// is defined on. The edge keys on the group name.
func scheduledActionRelationship(
	boundary awscloud.Boundary,
	action ScheduledAction,
) (awscloud.RelationshipObservation, bool) {
	name := strings.TrimSpace(action.Name)
	groupName := strings.TrimSpace(action.AutoScalingGroupName)
	if name == "" || groupName == "" {
		return awscloud.RelationshipObservation{}, false
	}
	actionID := firstNonEmpty(action.ARN, groupName+"/"+name)
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipAutoScalingScheduledActionTargetsGroup,
		SourceResourceID: actionID,
		SourceARN:        strings.TrimSpace(action.ARN),
		TargetResourceID: groupName,
		TargetType:       awscloud.ResourceTypeAutoScalingGroup,
		SourceRecordID:   actionID + "#group#" + groupName,
	}, true
}

func dedupeStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(input))
	output := make([]string, 0, len(input))
	for _, value := range input {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		output = append(output, trimmed)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
