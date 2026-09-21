// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package autoscaling

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
)

// groupRelationships records the launch-template, launch-configuration, subnet,
// target-group, and service-linked IAM role joins of an Auto Scaling group.
// Every edge sets a non-empty target_type matching the target scanner's
// resource_id form.
func groupRelationships(boundary aws.Boundary, group Group) []aws.RelationshipObservation {
	groupName := strings.TrimSpace(group.Name)
	if groupName == "" {
		return nil
	}
	groupARN := strings.TrimSpace(group.ARN)
	var observations []aws.RelationshipObservation

	// Launch template: key on the launch template ID (lt-...) when reported,
	// otherwise the launch template name, matching the EC2 launch-template
	// resource_id form owned by the Batch scanner.
	if launchTemplate := firstNonEmpty(group.LaunchTemplateID, group.LaunchTemplateName); launchTemplate != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAutoScalingGroupUsesLaunchTemplate,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: launchTemplate,
			TargetType:       aws.ResourceTypeEC2LaunchTemplate,
			SourceRecordID:   groupName + "#launch-template#" + launchTemplate,
		})
	}

	// Launch configuration: key on the launch configuration name, matching the
	// launch-configuration resource_id this scanner emits.
	if launchConfiguration := strings.TrimSpace(group.LaunchConfigurationName); launchConfiguration != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAutoScalingGroupUsesLaunchConfiguration,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: launchConfiguration,
			TargetType:       aws.ResourceTypeAutoScalingLaunchConfiguration,
			SourceRecordID:   groupName + "#launch-configuration#" + launchConfiguration,
		})
	}

	// Subnets: key on the bare subnet ID, matching the EC2-owned subnet
	// resource_id form.
	for _, subnetID := range dedupeStrings(group.SubnetIDs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAutoScalingGroupUsesSubnet,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: subnetID,
			TargetType:       aws.ResourceTypeEC2Subnet,
			SourceRecordID:   groupName + "#subnet#" + subnetID,
		})
	}

	// Target groups: key on the target group ARN, matching the ELBv2-owned
	// target-group resource_id form.
	for _, targetGroupARN := range dedupeStrings(group.TargetGroupARNs) {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAutoScalingGroupAttachedToTargetGroup,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: targetGroupARN,
			TargetARN:        targetGroupARN,
			TargetType:       aws.ResourceTypeELBv2TargetGroup,
			SourceRecordID:   groupName + "#target-group#" + targetGroupARN,
		})
	}

	// Service-linked IAM role: key on the role ARN.
	if roleARN := strings.TrimSpace(group.ServiceLinkedRoleARN); roleARN != "" {
		observations = append(observations, aws.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: aws.RelationshipAutoScalingGroupUsesIAMRole,
			SourceResourceID: groupName,
			SourceARN:        groupARN,
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       aws.ResourceTypeIAMRole,
			SourceRecordID:   groupName + "#service-linked-role#" + roleARN,
		})
	}

	return observations
}

// scalingPolicyRelationship records the Auto Scaling group a scaling policy
// applies to. The edge keys on the group name, matching the Auto Scaling group
// resource_id form.
func scalingPolicyRelationship(
	boundary aws.Boundary,
	policy ScalingPolicy,
) (aws.RelationshipObservation, bool) {
	policyID := firstNonEmpty(policy.ARN, policy.Name)
	groupName := strings.TrimSpace(policy.AutoScalingGroupName)
	if policyID == "" || groupName == "" {
		return aws.RelationshipObservation{}, false
	}
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAutoScalingPolicyTargetsGroup,
		SourceResourceID: policyID,
		SourceARN:        strings.TrimSpace(policy.ARN),
		TargetResourceID: groupName,
		TargetType:       aws.ResourceTypeAutoScalingGroup,
		SourceRecordID:   policyID + "#group#" + groupName,
	}, true
}

// lifecycleHookRelationship records the Auto Scaling group a lifecycle hook is
// defined on. The edge keys on the group name.
func lifecycleHookRelationship(
	boundary aws.Boundary,
	hook LifecycleHook,
) (aws.RelationshipObservation, bool) {
	name := strings.TrimSpace(hook.Name)
	groupName := strings.TrimSpace(hook.AutoScalingGroupName)
	if name == "" || groupName == "" {
		return aws.RelationshipObservation{}, false
	}
	hookID := groupName + "/" + name
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAutoScalingLifecycleHookTargetsGroup,
		SourceResourceID: hookID,
		TargetResourceID: groupName,
		TargetType:       aws.ResourceTypeAutoScalingGroup,
		SourceRecordID:   hookID + "#group#" + groupName,
	}, true
}

// scheduledActionRelationship records the Auto Scaling group a scheduled action
// is defined on. The edge keys on the group name.
func scheduledActionRelationship(
	boundary aws.Boundary,
	action ScheduledAction,
) (aws.RelationshipObservation, bool) {
	name := strings.TrimSpace(action.Name)
	groupName := strings.TrimSpace(action.AutoScalingGroupName)
	if name == "" || groupName == "" {
		return aws.RelationshipObservation{}, false
	}
	actionID := firstNonEmpty(action.ARN, groupName+"/"+name)
	return aws.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: aws.RelationshipAutoScalingScheduledActionTargetsGroup,
		SourceResourceID: actionID,
		SourceARN:        strings.TrimSpace(action.ARN),
		TargetResourceID: groupName,
		TargetType:       aws.ResourceTypeAutoScalingGroup,
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
