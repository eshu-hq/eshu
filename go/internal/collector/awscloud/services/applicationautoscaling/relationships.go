// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package applicationautoscaling

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// targetScalesResourceRelationship records that a scalable target governs the
// capacity of the underlying AWS resource it scales. The target is keyed by the
// partition-aware ARN the governed resource's scanner publishes as its
// resource_id, so the edge joins the real node. It returns nil for any
// namespace whose governed resource the repo does not scan to a stable
// ARN-keyed node, skipping the edge rather than dangling it.
func targetScalesResourceRelationship(
	boundary awscloud.Boundary,
	target ScalableTarget,
) *awscloud.RelationshipObservation {
	sourceID := scalableTargetResourceID(target.ServiceNamespace, target.ScalableDimension, target.ResourceID)
	if sourceID == "" {
		return nil
	}
	partition := awscloud.PartitionForBoundary(boundary)
	targetARN, targetType := targetResourceARN(
		partition,
		boundary.AccountID,
		boundary.Region,
		target.ServiceNamespace,
		target.ResourceID,
	)
	if targetARN == "" || targetType == "" {
		return nil
	}
	relationshipType := relationshipTypeForNamespace(strings.TrimSpace(target.ServiceNamespace))
	if relationshipType == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: relationshipType,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(target.ARN),
		TargetResourceID: targetARN,
		TargetARN:        targetARN,
		TargetType:       targetType,
		Attributes: map[string]any{
			"service_namespace":  strings.TrimSpace(target.ServiceNamespace),
			"resource_id":        strings.TrimSpace(target.ResourceID),
			"scalable_dimension": strings.TrimSpace(target.ScalableDimension),
		},
		SourceRecordID: sourceID + "->" + relationshipType + ":" + targetARN,
	}
}

// relationshipTypeForNamespace maps a scalable target's service namespace to the
// scoped scale-relationship type. It returns "" for namespaces with no mapped
// scanned target node.
func relationshipTypeForNamespace(namespace string) string {
	switch namespace {
	case "dynamodb":
		return awscloud.RelationshipApplicationAutoScalingTargetScalesDynamoDBTable
	case "ecs":
		return awscloud.RelationshipApplicationAutoScalingTargetScalesECSService
	case "rds":
		return awscloud.RelationshipApplicationAutoScalingTargetScalesRDSCluster
	case "lambda":
		return awscloud.RelationshipApplicationAutoScalingTargetScalesLambdaFunction
	default:
		return ""
	}
}

// policyForScalableTargetRelationship records that a scaling policy governs a
// scalable target, keyed by the composite scalable-target resource_id the
// scalable-target node publishes. It returns nil when either endpoint identity
// is missing.
func policyForScalableTargetRelationship(
	boundary awscloud.Boundary,
	policy ScalingPolicy,
) *awscloud.RelationshipObservation {
	sourceID := policyResourceID(policy)
	targetID := scalableTargetResourceID(policy.ServiceNamespace, policy.ScalableDimension, policy.ResourceID)
	if sourceID == "" || targetID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipApplicationAutoScalingPolicyForScalableTarget,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(policy.ARN),
		TargetResourceID: targetID,
		TargetType:       awscloud.ResourceTypeApplicationAutoScalingScalableTarget,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipApplicationAutoScalingPolicyForScalableTarget + ":" + targetID,
	}
}

// scheduledActionForScalableTargetRelationship records that a scheduled action
// governs a scalable target, keyed by the composite scalable-target resource_id
// the scalable-target node publishes. It returns nil when either endpoint
// identity is missing.
func scheduledActionForScalableTargetRelationship(
	boundary awscloud.Boundary,
	action ScheduledAction,
) *awscloud.RelationshipObservation {
	sourceID := scheduledActionResourceID(action)
	targetID := scalableTargetResourceID(action.ServiceNamespace, action.ScalableDimension, action.ResourceID)
	if sourceID == "" || targetID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipApplicationAutoScalingScheduledActionForScalableTarget,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(action.ARN),
		TargetResourceID: targetID,
		TargetType:       awscloud.ResourceTypeApplicationAutoScalingScalableTarget,
		SourceRecordID:   sourceID + "->" + awscloud.RelationshipApplicationAutoScalingScheduledActionForScalableTarget + ":" + targetID,
	}
}

// policyAlarmRelationships records each CloudWatch alarm a scaling policy is
// bound to. Application Auto Scaling reports the alarm ARN, which matches the
// CloudWatch scanner's published alarm resource_id, so the edges join real
// alarm nodes. Non-ARN or empty alarm identifiers are skipped.
func policyAlarmRelationships(
	boundary awscloud.Boundary,
	policy ScalingPolicy,
) []awscloud.RelationshipObservation {
	sourceID := policyResourceID(policy)
	if sourceID == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	for _, alarmARN := range policy.AlarmARNs {
		trimmed := strings.TrimSpace(alarmARN)
		if !strings.HasPrefix(trimmed, "arn:") {
			continue
		}
		relationships = append(relationships, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipApplicationAutoScalingPolicyTriggersCloudWatchAlarm,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(policy.ARN),
			TargetResourceID: trimmed,
			TargetARN:        trimmed,
			TargetType:       awscloud.ResourceTypeCloudWatchAlarm,
			SourceRecordID:   sourceID + "->" + awscloud.RelationshipApplicationAutoScalingPolicyTriggersCloudWatchAlarm + ":" + trimmed,
		})
	}
	return relationships
}
