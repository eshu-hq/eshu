// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsaastypes "github.com/aws/aws-sdk-go-v2/service/applicationautoscaling/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	aasservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/applicationautoscaling"
)

// mapScalableTarget maps one SDK scalable target into the scanner-owned model.
func mapScalableTarget(target awsaastypes.ScalableTarget) aasservice.ScalableTarget {
	mapped := aasservice.ScalableTarget{
		ARN:               strings.TrimSpace(aws.ToString(target.ScalableTargetARN)),
		ServiceNamespace:  strings.TrimSpace(string(target.ServiceNamespace)),
		ResourceID:        strings.TrimSpace(aws.ToString(target.ResourceId)),
		ScalableDimension: strings.TrimSpace(string(target.ScalableDimension)),
		RoleARN:           strings.TrimSpace(aws.ToString(target.RoleARN)),
		MinCapacity:       target.MinCapacity,
		MaxCapacity:       target.MaxCapacity,
		CreationTime:      aws.ToTime(target.CreationTime),
	}
	if state := target.SuspendedState; state != nil {
		mapped.SuspendedDynamicScalingInSuspended = state.DynamicScalingInSuspended
		mapped.SuspendedDynamicScalingOutSuspended = state.DynamicScalingOutSuspended
		mapped.SuspendedScheduledScalingSuspended = state.ScheduledScalingSuspended
	}
	return mapped
}

// mapScalingPolicy maps one SDK scaling policy into the scanner-owned model. The
// step-scaling and target-tracking configuration bodies are intentionally
// dropped; only the bound CloudWatch alarm ARNs are kept.
func mapScalingPolicy(policy awsaastypes.ScalingPolicy) aasservice.ScalingPolicy {
	return aasservice.ScalingPolicy{
		ARN:               strings.TrimSpace(aws.ToString(policy.PolicyARN)),
		Name:              strings.TrimSpace(aws.ToString(policy.PolicyName)),
		PolicyType:        strings.TrimSpace(string(policy.PolicyType)),
		ServiceNamespace:  strings.TrimSpace(string(policy.ServiceNamespace)),
		ResourceID:        strings.TrimSpace(aws.ToString(policy.ResourceId)),
		ScalableDimension: strings.TrimSpace(string(policy.ScalableDimension)),
		AlarmARNs:         alarmARNs(policy.Alarms),
		CreationTime:      aws.ToTime(policy.CreationTime),
	}
}

// mapScheduledAction maps one SDK scheduled action into the scanner-owned model.
func mapScheduledAction(action awsaastypes.ScheduledAction) aasservice.ScheduledAction {
	mapped := aasservice.ScheduledAction{
		ARN:               strings.TrimSpace(aws.ToString(action.ScheduledActionARN)),
		Name:              strings.TrimSpace(aws.ToString(action.ScheduledActionName)),
		ServiceNamespace:  strings.TrimSpace(string(action.ServiceNamespace)),
		ResourceID:        strings.TrimSpace(aws.ToString(action.ResourceId)),
		ScalableDimension: strings.TrimSpace(string(action.ScalableDimension)),
		Schedule:          strings.TrimSpace(aws.ToString(action.Schedule)),
		Timezone:          strings.TrimSpace(aws.ToString(action.Timezone)),
		StartTime:         aws.ToTime(action.StartTime),
		EndTime:           aws.ToTime(action.EndTime),
		CreationTime:      aws.ToTime(action.CreationTime),
	}
	if act := action.ScalableTargetAction; act != nil {
		mapped.MinCapacity = act.MinCapacity
		mapped.MaxCapacity = act.MaxCapacity
	}
	return mapped
}

// alarmARNs extracts the bound CloudWatch alarm ARNs from a scaling policy.
func alarmARNs(alarms []awsaastypes.Alarm) []string {
	if len(alarms) == 0 {
		return nil
	}
	arns := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		if arn := strings.TrimSpace(aws.ToString(alarm.AlarmARN)); arn != "" {
			arns = append(arns, arn)
		}
	}
	if len(arns) == 0 {
		return nil
	}
	return arns
}

// throttleWarning builds a non-fatal sustained-throttle warning for one
// namespace/operation so a partial scan records the omitted component instead of
// silently dropping it.
func (c *Client) throttleWarning(
	namespace awsaastypes.ServiceNamespace,
	operation, component string,
) *awscloud.WarningObservation {
	return &awscloud.WarningObservation{
		Boundary:    c.boundary,
		WarningKind: awscloud.WarningThrottleSustained,
		ErrorClass:  "throttled",
		Message: "Application Auto Scaling " + operation +
			" throttled after SDK retries; " + component + " metadata omitted for namespace " +
			string(namespace),
		Attributes: map[string]any{
			"operation":         operation,
			"service_namespace": string(namespace),
			"partial_component": component,
		},
		SourceRecordID: "applicationautoscaling_" + component + "_throttled_" + string(namespace),
	}
}

// appendThrottleWarning appends warning when it is non-nil. Each throttled
// namespace/component produces a distinct omission (its message and
// SourceRecordID embed the namespace), so every observation is recorded rather
// than deduplicated: suppressing a later namespace would hide that its metadata
// was dropped. A nil warning (the namespace succeeded) leaves the stream
// unchanged.
func appendThrottleWarning(
	warnings []awscloud.WarningObservation,
	warning *awscloud.WarningObservation,
) []awscloud.WarningObservation {
	if warning == nil {
		return warnings
	}
	return append(warnings, *warning)
}
