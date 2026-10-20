// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package autoscaling

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits EC2 Auto Scaling group, launch-configuration, scaling-policy,
// lifecycle-hook, scheduled-action, and relationship facts for one claimed
// account and region.
//
// The scanner is metadata-only. It never reads or persists launch
// configuration or launch template UserData, and it never mutates an Auto
// Scaling resource. The Auto Scaling group resource_id is the bare group name
// so the CodeDeploy and Batch dangling edges that target
// aws_autoscaling_group by name resolve to this resource.
type Scanner struct {
	Client Client
}

// Scan observes EC2 Auto Scaling resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("autoscaling scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceAutoScaling:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceAutoScaling
	default:
		return nil, fmt.Errorf("autoscaling scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	groups, err := s.Client.ListAutoScalingGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Auto Scaling groups: %w", err)
	}
	for _, group := range groups {
		groupEnvelopes, err := groupEnvelopes(boundary, group)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, groupEnvelopes...)

		hooks, err := s.Client.ListLifecycleHooks(ctx, group)
		if err != nil {
			return nil, fmt.Errorf("list lifecycle hooks for Auto Scaling group %q: %w", group.Name, err)
		}
		for _, hook := range hooks {
			hookEnvelopes, err := lifecycleHookEnvelopes(boundary, hook)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, hookEnvelopes...)
		}
	}

	launchConfigurations, err := s.Client.ListLaunchConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Auto Scaling launch configurations: %w", err)
	}
	for _, launchConfiguration := range launchConfigurations {
		resource, err := awscloud.NewResourceEnvelope(launchConfigurationObservation(boundary, launchConfiguration))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	policies, err := s.Client.ListScalingPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Auto Scaling scaling policies: %w", err)
	}
	for _, policy := range policies {
		policyEnvelopes, err := scalingPolicyEnvelopes(boundary, policy)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, policyEnvelopes...)
	}

	scheduledActions, err := s.Client.ListScheduledActions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Auto Scaling scheduled actions: %w", err)
	}
	for _, scheduledAction := range scheduledActions {
		actionEnvelopes, err := scheduledActionEnvelopes(boundary, scheduledAction)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, actionEnvelopes...)
	}

	return envelopes, nil
}

func groupEnvelopes(boundary awscloud.Boundary, group Group) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(groupObservation(boundary, group))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range groupRelationships(boundary, group) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func lifecycleHookEnvelopes(boundary awscloud.Boundary, hook LifecycleHook) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(lifecycleHookObservation(boundary, hook))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if observation, ok := lifecycleHookRelationship(boundary, hook); ok {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func scalingPolicyEnvelopes(boundary awscloud.Boundary, policy ScalingPolicy) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(scalingPolicyObservation(boundary, policy))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if observation, ok := scalingPolicyRelationship(boundary, policy); ok {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

func scheduledActionEnvelopes(boundary awscloud.Boundary, action ScheduledAction) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(scheduledActionObservation(boundary, action))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if observation, ok := scheduledActionRelationship(boundary, action); ok {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

// groupObservation builds the Auto Scaling group resource fact. ResourceID is
// the bare group name so CodeDeploy and Batch dangling edges that target an
// Auto Scaling group by name resolve to this resource.
func groupObservation(boundary awscloud.Boundary, group Group) awscloud.ResourceObservation {
	groupName := strings.TrimSpace(group.Name)
	groupARN := strings.TrimSpace(group.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   groupName,
		ResourceType: awscloud.ResourceTypeAutoScalingGroup,
		Name:         groupName,
		State:        strings.TrimSpace(group.Status),
		Tags:         group.Tags,
		Attributes: map[string]any{
			"availability_zones":        cloneStrings(group.AvailabilityZones),
			"capacity_rebalance":        group.CapacityRebalance,
			"created_time":              timeOrNil(group.CreatedTime),
			"default_cooldown":          group.DefaultCooldown,
			"desired_capacity":          group.DesiredCapacity,
			"health_check_grace_period": group.HealthCheckGracePeriod,
			"health_check_type":         strings.TrimSpace(group.HealthCheckType),
			"launch_configuration_name": strings.TrimSpace(group.LaunchConfigurationName),
			"launch_template_id":        strings.TrimSpace(group.LaunchTemplateID),
			"launch_template_name":      strings.TrimSpace(group.LaunchTemplateName),
			"launch_template_version":   strings.TrimSpace(group.LaunchTemplateVersion),
			"load_balancer_names":       cloneStrings(group.LoadBalancerNames),
			"max_instance_lifetime":     group.MaxInstanceLifetime,
			"max_size":                  group.MaxSize,
			"min_size":                  group.MinSize,
			"new_instances_protected":   group.NewInstancesProtected,
			"service_linked_role_arn":   strings.TrimSpace(group.ServiceLinkedRoleARN),
			"status":                    strings.TrimSpace(group.Status),
			"subnet_ids":                cloneStrings(group.SubnetIDs),
			"target_group_arns":         cloneStrings(group.TargetGroupARNs),
			"termination_policies":      cloneStrings(group.TerminationPolicies),
		},
		CorrelationAnchors: []string{groupARN, groupName},
		SourceRecordID:     firstNonEmpty(groupName, groupARN),
	}
}

func launchConfigurationObservation(
	boundary awscloud.Boundary,
	launchConfiguration LaunchConfiguration,
) awscloud.ResourceObservation {
	launchConfigurationARN := strings.TrimSpace(launchConfiguration.ARN)
	name := strings.TrimSpace(launchConfiguration.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          launchConfigurationARN,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeAutoScalingLaunchConfiguration,
		Name:         name,
		// No attributes: launch configuration UserData and other launch detail
		// are never persisted. Only identity is emitted.
		CorrelationAnchors: []string{launchConfigurationARN, name},
		SourceRecordID:     firstNonEmpty(name, launchConfigurationARN),
	}
}

func scalingPolicyObservation(boundary awscloud.Boundary, policy ScalingPolicy) awscloud.ResourceObservation {
	policyARN := strings.TrimSpace(policy.ARN)
	name := strings.TrimSpace(policy.Name)
	resourceID := firstNonEmpty(policyARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          policyARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAutoScalingPolicy,
		Name:         name,
		Attributes: map[string]any{
			"adjustment_type":         strings.TrimSpace(policy.AdjustmentType),
			"auto_scaling_group_name": strings.TrimSpace(policy.AutoScalingGroupName),
			"enabled":                 policy.Enabled,
			"policy_type":             strings.TrimSpace(policy.PolicyType),
		},
		CorrelationAnchors: []string{policyARN, name},
		SourceRecordID:     resourceID,
	}
}

func lifecycleHookObservation(boundary awscloud.Boundary, hook LifecycleHook) awscloud.ResourceObservation {
	name := strings.TrimSpace(hook.Name)
	groupName := strings.TrimSpace(hook.AutoScalingGroupName)
	resourceID := groupName + "/" + name
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAutoScalingLifecycleHook,
		Name:         name,
		Attributes: map[string]any{
			"auto_scaling_group_name": groupName,
			"default_result":          strings.TrimSpace(hook.DefaultResult),
			"global_timeout":          hook.GlobalTimeout,
			"heartbeat_timeout":       hook.HeartbeatTimeout,
			"lifecycle_transition":    strings.TrimSpace(hook.LifecycleTransition),
			"notification_target_arn": strings.TrimSpace(hook.NotificationTargetARN),
			"role_arn":                strings.TrimSpace(hook.RoleARN),
		},
		CorrelationAnchors: []string{resourceID, name},
		SourceRecordID:     resourceID,
	}
}

func scheduledActionObservation(boundary awscloud.Boundary, action ScheduledAction) awscloud.ResourceObservation {
	actionARN := strings.TrimSpace(action.ARN)
	name := strings.TrimSpace(action.Name)
	groupName := strings.TrimSpace(action.AutoScalingGroupName)
	resourceID := firstNonEmpty(actionARN, groupName+"/"+name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          actionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeAutoScalingScheduledAction,
		Name:         name,
		Attributes: map[string]any{
			"auto_scaling_group_name": groupName,
			"desired_capacity":        int32PtrOrNil(action.DesiredCapacity),
			"end_time":                timeOrNil(action.EndTime),
			"max_size":                int32PtrOrNil(action.MaxSize),
			"min_size":                int32PtrOrNil(action.MinSize),
			"recurrence":              strings.TrimSpace(action.Recurrence),
			"start_time":              timeOrNil(action.StartTime),
			"time_zone":               strings.TrimSpace(action.TimeZone),
		},
		CorrelationAnchors: []string{actionARN, name},
		SourceRecordID:     resourceID,
	}
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
