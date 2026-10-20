// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package applicationautoscaling

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Application Auto Scaling metadata-only facts for one claimed
// account and region. It reports scalable targets, scaling policies, and
// scheduled actions across every supported service namespace, plus the
// target-to-governed-resource (DynamoDB table, ECS service, Aurora cluster,
// Lambda function), policy-to-CloudWatch-alarm, and policy/scheduled-action to
// scalable-target relationships. It never registers, deregisters, or mutates a
// scalable target, never puts or deletes a scaling policy or scheduled action,
// and never invokes a scaling action.
type Scanner struct {
	// Client is the metadata-only Application Auto Scaling snapshot source.
	Client Client
}

// Scan observes Application Auto Scaling scalable targets, scaling policies, and
// scheduled actions plus their resolvable resource dependencies through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("applicationautoscaling scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceApplicationAutoScaling:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceApplicationAutoScaling
	default:
		return nil, fmt.Errorf("applicationautoscaling scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("snapshot Application Auto Scaling: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, boundary, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, target := range snapshot.ScalableTargets {
		next, err := scalableTargetEnvelopes(boundary, target)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, policy := range snapshot.ScalingPolicies {
		next, err := scalingPolicyEnvelopes(boundary, policy)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	for _, action := range snapshot.ScheduledActions {
		next, err := scheduledActionEnvelopes(boundary, action)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

// appendWarnings emits a warning envelope per observation, forcing each
// observation's service_kind to the canonical boundary value. Warning
// observations are produced by the SDK client from the caller-supplied boundary,
// which may carry whitespace padding; overwriting it here keeps warning fact IDs
// and payloads aligned with the resource and relationship facts Scan emits.
func appendWarnings(
	envelopes *[]facts.Envelope,
	boundary awscloud.Boundary,
	observations []awscloud.WarningObservation,
) error {
	for _, observation := range observations {
		observation.Boundary.ServiceKind = boundary.ServiceKind
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func scalableTargetEnvelopes(boundary awscloud.Boundary, target ScalableTarget) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(scalableTargetObservation(boundary, target))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := targetScalesResourceRelationship(boundary, target); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func scalingPolicyEnvelopes(boundary awscloud.Boundary, policy ScalingPolicy) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(scalingPolicyObservation(boundary, policy))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := policyForScalableTargetRelationship(boundary, policy); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	for _, relationship := range policyAlarmRelationships(boundary, policy) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func scheduledActionEnvelopes(boundary awscloud.Boundary, action ScheduledAction) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(scheduledActionObservation(boundary, action))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := scheduledActionForScalableTargetRelationship(boundary, action); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func scalableTargetObservation(boundary awscloud.Boundary, target ScalableTarget) awscloud.ResourceObservation {
	targetARN := strings.TrimSpace(target.ARN)
	resourceID := scalableTargetResourceID(target.ServiceNamespace, target.ScalableDimension, target.ResourceID)
	name := strings.TrimSpace(target.ResourceID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          targetARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeApplicationAutoScalingScalableTarget,
		Name:         name,
		Attributes: map[string]any{
			"service_namespace":             strings.TrimSpace(target.ServiceNamespace),
			"scalable_dimension":            strings.TrimSpace(target.ScalableDimension),
			"scaled_resource_id":            name,
			"role_arn":                      strings.TrimSpace(target.RoleARN),
			"min_capacity":                  int32OrNil(target.MinCapacity),
			"max_capacity":                  int32OrNil(target.MaxCapacity),
			"dynamic_scaling_in_suspended":  boolOrNil(target.SuspendedDynamicScalingInSuspended),
			"dynamic_scaling_out_suspended": boolOrNil(target.SuspendedDynamicScalingOutSuspended),
			"scheduled_scaling_suspended":   boolOrNil(target.SuspendedScheduledScalingSuspended),
			"creation_time":                 timeOrNil(target.CreationTime),
		},
		CorrelationAnchors: []string{targetARN, resourceID},
		SourceRecordID:     resourceID,
	}
}

func scalingPolicyObservation(boundary awscloud.Boundary, policy ScalingPolicy) awscloud.ResourceObservation {
	policyARN := strings.TrimSpace(policy.ARN)
	resourceID := policyResourceID(policy)
	name := strings.TrimSpace(policy.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          policyARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeApplicationAutoScalingScalingPolicy,
		Name:         name,
		Attributes: map[string]any{
			"policy_type":        strings.TrimSpace(policy.PolicyType),
			"service_namespace":  strings.TrimSpace(policy.ServiceNamespace),
			"scalable_dimension": strings.TrimSpace(policy.ScalableDimension),
			"scaled_resource_id": strings.TrimSpace(policy.ResourceID),
			"alarm_arns":         cloneStrings(policy.AlarmARNs),
			"creation_time":      timeOrNil(policy.CreationTime),
		},
		CorrelationAnchors: []string{policyARN, name},
		SourceRecordID:     resourceID,
	}
}

func scheduledActionObservation(boundary awscloud.Boundary, action ScheduledAction) awscloud.ResourceObservation {
	actionARN := strings.TrimSpace(action.ARN)
	resourceID := scheduledActionResourceID(action)
	name := strings.TrimSpace(action.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          actionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeApplicationAutoScalingScheduledAction,
		Name:         name,
		Attributes: map[string]any{
			"service_namespace":  strings.TrimSpace(action.ServiceNamespace),
			"scalable_dimension": strings.TrimSpace(action.ScalableDimension),
			"scaled_resource_id": strings.TrimSpace(action.ResourceID),
			"schedule":           strings.TrimSpace(action.Schedule),
			"timezone":           strings.TrimSpace(action.Timezone),
			"min_capacity":       int32OrNil(action.MinCapacity),
			"max_capacity":       int32OrNil(action.MaxCapacity),
			"start_time":         timeOrNil(action.StartTime),
			"end_time":           timeOrNil(action.EndTime),
			"creation_time":      timeOrNil(action.CreationTime),
		},
		CorrelationAnchors: []string{actionARN, name},
		SourceRecordID:     resourceID,
	}
}
